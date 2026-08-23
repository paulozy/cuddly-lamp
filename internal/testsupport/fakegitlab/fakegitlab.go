// Package fakegitlab is a stand-in for gitlab.com's REST v4 API, serving the
// subset of endpoints the platform calls.
//
// It exists so the end-to-end suite can exercise the real server, worker and
// database against a provider whose answers are known — no personal access
// token, no network, no dependency on the state of somebody else's project.
//
// Every payload shape here was captured from real gitlab.com responses (the
// public `gitlab-org/gitlab-runner` project). That is what keeps the fake
// honest: the quirks the client has to cope with are reproduced deliberately,
// including `changes_count` being a string, `open_issues_count` arriving null,
// a tree paginated with `x-next-page` and no truncation flag, and merge
// requests addressed by `iid` rather than `id`.
package fakegitlab

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Project is a fixture project served by the fake.
type Project struct {
	ID            int
	Path          string // full path with namespace, e.g. group/subgroup/project
	Description   string
	DefaultBranch string
	Visibility    string
	Topics        []string
	StarCount     int
	ForksCount    int
	// OpenIssuesCount is a pointer because gitlab.com sends null for projects
	// with issues disabled, and the client distinguishes that from zero.
	OpenIssuesCount *int
	Languages       map[string]float64
	Branches        []string
	Commits         []Commit
	Issues          []Issue
	Contributors    []Contributor
	// TreePages is served one page per element, so pagination is a real
	// multi-request walk rather than a single response.
	TreePages [][]TreeEntry
	// EndlessTree keeps advertising a next page forever, which is how the
	// client's page ceiling — and the Truncated flag it synthesizes — get
	// exercised.
	EndlessTree   bool
	MergeRequests []MergeRequest
	// Diffs are keyed by merge request iid.
	Diffs map[int64][]Diff
}

type Commit struct {
	ID         string
	Message    string
	AuthorName string
}

type TreeEntry struct {
	Path string
	Type string // "blob" or "tree"
}

type MergeRequest struct {
	ID           int64
	IID          int64
	Title        string
	Description  string
	State        string // opened, closed, merged, locked
	Draft        bool
	Author       string
	SourceBranch string
	TargetBranch string
	SHA          string
	BaseSHA      string
	MergedAt     string
	ChangesCount string
}

type Diff struct {
	OldPath     string
	NewPath     string
	NewFile     bool
	RenamedFile bool
	DeletedFile bool
	Diff        string
}

// Hook is a webhook registration the platform performed.
type Hook struct {
	ID                  int64  `json:"id"`
	ProjectPath         string `json:"project_path"`
	URL                 string `json:"url"`
	Token               string `json:"token"`
	PushEvents          bool   `json:"push_events"`
	MergeRequestsEvents bool   `json:"merge_requests_events"`
	SSLVerification     bool   `json:"enable_ssl_verification"`
}

// CommittedFile is a file the platform created or updated.
type CommittedFile struct {
	ProjectPath string `json:"project_path"`
	Branch      string `json:"branch"`
	Path        string `json:"path"`
	Message     string `json:"message"`
	Content     string `json:"content"`
	Method      string `json:"method"` // POST for create, PUT for update
}

// CreatedBranch is a branch the platform created.
type CreatedBranch struct {
	ProjectPath string `json:"project_path"`
	Name        string `json:"name"`
	Ref         string `json:"ref"`
}

// CreatedMergeRequest is a merge request the platform opened.
type CreatedMergeRequest struct {
	ProjectPath  string `json:"project_path"`
	IID          int64  `json:"iid"`
	Title        string `json:"title"`
	SourceBranch string `json:"source_branch"`
	TargetBranch string `json:"target_branch"`
	Description  string `json:"description"`
}

// Server is the fake instance. Mount Handler() on an httptest.Server (Go
// suite) or an http.Server (the standalone command the browser suite uses).
type Server struct {
	mu        sync.Mutex
	token     string
	projects  map[string]*Project
	hooks     []Hook
	files     []CommittedFile
	branches  []CreatedBranch
	createdMR []CreatedMergeRequest
	// State a test can assert on after driving a write through the platform.
	closedIssues map[int64]bool
	approvedMRs  map[int64]bool
	nextIID      int64
	client       *http.Client
}

// New returns a fake requiring the given bearer token. An empty token accepts
// any request, which is only useful for exploring by hand.
func New(token string, projects ...*Project) *Server {
	s := &Server{
		token:    token,
		projects: make(map[string]*Project, len(projects)),
		nextIID:  9000,
		client:   &http.Client{Timeout: 10 * time.Second},
	}
	for _, p := range projects {
		s.projects[p.Path] = p
	}
	return s
}

// Hooks returns the webhook registrations recorded so far.
func (s *Server) Hooks() []Hook {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Hook(nil), s.hooks...)
}

// Files returns the file writes recorded so far.
func (s *Server) Files() []CommittedFile {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]CommittedFile(nil), s.files...)
}

// Branches returns the branch creations recorded so far.
func (s *Server) Branches() []CreatedBranch {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]CreatedBranch(nil), s.branches...)
}

// MergeRequestsOpened returns the merge requests the platform opened.
func (s *Server) MergeRequestsOpened() []CreatedMergeRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]CreatedMergeRequest(nil), s.createdMR...)
}

// FireWebhook delivers an event to the webhook the platform registered,
// authenticating with the secret the platform chose — exactly as gitlab.com
// would. eventUUID may be empty to exercise the derived-idempotency path.
//
// It returns the receiver's status code, so a test can assert 202 for a fresh
// delivery and 200 for a replay.
func (s *Server) FireWebhook(event, eventUUID string, payload any) (int, error) {
	s.mu.Lock()
	if len(s.hooks) == 0 {
		s.mu.Unlock()
		return 0, fmt.Errorf("fakegitlab: no webhook registered — did the sync run with a non-local WEBHOOK_BASE_URL?")
	}
	hook := s.hooks[len(s.hooks)-1]
	s.mu.Unlock()

	return s.deliver(hook, event, eventUUID, payload)
}

// HookForRepo returns the webhook registered for one repository.
//
// Registration happens inside an asynchronous sync, so "the most recently
// registered hook" is not reliably the one a given test caused — any other
// test that created a repository can land its registration in between. Tests
// that care about a specific repository must address it by id.
func (s *Server) HookForRepo(repoID string) (Hook, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.hooks) - 1; i >= 0; i-- {
		if strings.HasSuffix(s.hooks[i].URL, "/"+repoID) {
			return s.hooks[i], true
		}
	}
	return Hook{}, false
}

// FireWebhookTo delivers an event to one repository's webhook. Prefer it over
// FireWebhook in tests — see HookForRepo for why.
func (s *Server) FireWebhookTo(repoID, event, eventUUID string, payload any) (int, error) {
	hook, ok := s.HookForRepo(repoID)
	if !ok {
		return 0, fmt.Errorf("fakegitlab: no webhook registered for repository %s", repoID)
	}
	return s.deliver(hook, event, eventUUID, payload)
}

func (s *Server) deliver(hook Hook, event, eventUUID string, payload any) (int, error) {

	body, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequest(http.MethodPost, hook.URL, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Gitlab-Token", hook.Token)
	req.Header.Set("X-Gitlab-Event", event)
	if eventUUID != "" {
		req.Header.Set("X-Gitlab-Event-UUID", eventUUID)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
}

// Handler routes the API surface plus a small `/_fake/` control plane the
// browser suite drives over HTTP.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/_fake/", s.handleControl)
	mux.HandleFunc("/", s.handleAPI)
	return mux
}

func (s *Server) handleControl(w http.ResponseWriter, r *http.Request) {
	switch strings.TrimPrefix(r.URL.Path, "/_fake/") {
	case "state":
		writeJSON(w, http.StatusOK, map[string]any{
			"hooks":          s.Hooks(),
			"files":          s.Files(),
			"branches":       s.Branches(),
			"merge_requests": s.MergeRequestsOpened(),
		})
	case "fire-webhook":
		var body struct {
			Event     string          `json:"event"`
			EventUUID string          `json:"event_uuid"`
			Payload   json.RawMessage `json:"payload"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		status, err := s.FireWebhook(body.Event, body.EventUUID, json.RawMessage(body.Payload))
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]int{"receiver_status": status})
	default:
		http.NotFound(w, r)
	}
}

// handleAPI dispatches on the escaped path, because a project is addressed by
// its path with the slashes percent-encoded — decoding first would make
// `group/subgroup/project` indistinguishable from a sub-resource.
func (s *Server) handleAPI(w http.ResponseWriter, r *http.Request) {
	if s.token != "" && r.Header.Get("Authorization") != "Bearer "+s.token {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "401 Unauthorized"})
		return
	}

	escaped := strings.TrimPrefix(r.URL.EscapedPath(), "/projects/")
	if escaped == r.URL.EscapedPath() {
		http.NotFound(w, r)
		return
	}
	segments := strings.SplitN(escaped, "/", 2)
	projectPath, err := url.PathUnescape(segments[0])
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "bad project path"})
		return
	}
	rest := ""
	if len(segments) == 2 {
		rest = segments[1]
	}

	s.mu.Lock()
	project := s.projects[projectPath]
	s.mu.Unlock()
	if project == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "404 Project Not Found"})
		return
	}

	switch {
	case rest == "":
		s.serveProject(w, project)
	case rest == "languages":
		writeJSON(w, http.StatusOK, project.Languages)
	case rest == "repository/branches" && r.Method == http.MethodGet:
		s.serveBranches(w, project)
	case rest == "repository/branches" && r.Method == http.MethodPost:
		s.createBranch(w, r, project)
	case rest == "repository/commits":
		s.serveCommits(w, project)
	case rest == "repository/tree":
		s.serveTree(w, r, project)
	case strings.HasPrefix(rest, "repository/files/"):
		s.serveFile(w, r, project, strings.TrimPrefix(rest, "repository/files/"))
	case rest == "merge_requests" && r.Method == http.MethodGet:
		s.serveMergeRequests(w, project)
	case rest == "merge_requests" && r.Method == http.MethodPost:
		s.createMergeRequest(w, r, project)
	case rest == "issues" && r.Method == http.MethodGet:
		s.serveIssues(w, project)
	case strings.HasPrefix(rest, "issues/") && r.Method == http.MethodPut:
		s.updateIssue(w, r, project, strings.TrimPrefix(rest, "issues/"))
	case rest == "repository/contributors":
		s.serveContributors(w, project)
	case rest == "hooks" && r.Method == http.MethodPost:
		s.createHook(w, r, project)
	case strings.HasSuffix(rest, "/approve") && strings.HasPrefix(rest, "merge_requests/") && r.Method == http.MethodPost:
		s.approveMergeRequest(w, project, strings.TrimSuffix(strings.TrimPrefix(rest, "merge_requests/"), "/approve"))
	case strings.HasPrefix(rest, "merge_requests/"):
		s.serveMergeRequestDetail(w, project, strings.TrimPrefix(rest, "merge_requests/"))
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "404 Not Found: " + rest})
	}
}

func (s *Server) serveProject(w http.ResponseWriter, p *Project) {
	writeJSON(w, http.StatusOK, map[string]any{
		"id":                  p.ID,
		"name":                p.Path[strings.LastIndex(p.Path, "/")+1:],
		"path_with_namespace": p.Path,
		"description":         p.Description,
		"default_branch":      p.DefaultBranch,
		"topics":              p.Topics,
		"star_count":          p.StarCount,
		"forks_count":         p.ForksCount,
		"open_issues_count":   p.OpenIssuesCount,
		"visibility":          p.Visibility,
		"web_url":             "https://gitlab.example/" + p.Path,
	})
}

func (s *Server) serveBranches(w http.ResponseWriter, p *Project) {
	out := make([]map[string]any, 0, len(p.Branches))
	for i, name := range p.Branches {
		out = append(out, map[string]any{
			"name":   name,
			"commit": map[string]any{"id": fmt.Sprintf("branch-sha-%d", i)},
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) serveCommits(w http.ResponseWriter, p *Project) {
	out := make([]map[string]any, 0, len(p.Commits))
	for _, c := range p.Commits {
		out = append(out, map[string]any{
			"id":             c.ID,
			"message":        c.Message,
			"author_name":    c.AuthorName,
			"committed_date": "2026-08-21T14:50:35.000+05:30",
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// serveTree paginates like gitlab.com: one page per request, the next page
// advertised in `x-next-page`, and an empty header on the last page. There is
// no `truncated` field, because GitLab does not send one.
func (s *Server) serveTree(w http.ResponseWriter, r *http.Request, p *Project) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}

	if p.EndlessTree {
		w.Header().Set("x-next-page", strconv.Itoa(page+1))
		writeJSON(w, http.StatusOK, []map[string]any{
			{"path": fmt.Sprintf("generated/file-%d.go", page), "type": "blob"},
		})
		return
	}

	if page > len(p.TreePages) {
		writeJSON(w, http.StatusOK, []map[string]any{})
		return
	}
	if page < len(p.TreePages) {
		w.Header().Set("x-next-page", strconv.Itoa(page+1))
	}
	entries := p.TreePages[page-1]
	out := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		out = append(out, map[string]any{"path": e.Path, "type": e.Type, "name": e.Path, "mode": "100644"})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) serveMergeRequests(w http.ResponseWriter, p *Project) {
	out := make([]map[string]any, 0, len(p.MergeRequests))
	for _, mr := range p.MergeRequests {
		if mr.State != "opened" {
			continue
		}
		out = append(out, mergeRequestJSON(mr, false))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) serveMergeRequestDetail(w http.ResponseWriter, p *Project, rest string) {
	iidPart, tail, _ := strings.Cut(rest, "/")
	iid, err := strconv.ParseInt(iidPart, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "404 Not Found"})
		return
	}

	if tail == "diffs" {
		diffs := p.Diffs[iid]
		out := make([]map[string]any, 0, len(diffs))
		for _, d := range diffs {
			out = append(out, map[string]any{
				"old_path":     d.OldPath,
				"new_path":     d.NewPath,
				"new_file":     d.NewFile,
				"renamed_file": d.RenamedFile,
				"deleted_file": d.DeletedFile,
				"diff":         d.Diff,
				"too_large":    false,
				"collapsed":    false,
			})
		}
		writeJSON(w, http.StatusOK, out)
		return
	}

	for _, mr := range p.MergeRequests {
		if mr.IID == iid {
			// Only the detail payload carries diff_refs, which is where the
			// base SHA comes from.
			writeJSON(w, http.StatusOK, mergeRequestJSON(mr, true))
			return
		}
	}
	writeJSON(w, http.StatusNotFound, map[string]string{"message": "404 Not found"})
}

func mergeRequestJSON(mr MergeRequest, withDiffRefs bool) map[string]any {
	out := map[string]any{
		"id":            mr.ID,
		"iid":           mr.IID,
		"title":         mr.Title,
		"description":   mr.Description,
		"state":         mr.State,
		"draft":         mr.Draft,
		"source_branch": mr.SourceBranch,
		"target_branch": mr.TargetBranch,
		"sha":           mr.SHA,
		"web_url":       fmt.Sprintf("https://gitlab.example/-/merge_requests/%d", mr.IID),
		"created_at":    "2026-08-21T12:45:26.673Z",
		"updated_at":    "2026-08-21T17:28:23.903Z",
		"author":        map[string]any{"username": mr.Author, "name": mr.Author},
	}
	if mr.MergedAt != "" {
		out["merged_at"] = mr.MergedAt
	}
	// gitlab.com omits changes_count on the list endpoint and sends it as a
	// string (sometimes "1000+") on the detail endpoint.
	if withDiffRefs {
		out["changes_count"] = mr.ChangesCount
		out["diff_refs"] = map[string]any{
			"base_sha":  mr.BaseSHA,
			"head_sha":  mr.SHA,
			"start_sha": mr.BaseSHA,
		}
	}
	return out
}

func (s *Server) createHook(w http.ResponseWriter, r *http.Request, p *Project) {
	var payload struct {
		URL                   string `json:"url"`
		Token                 string `json:"token"`
		PushEvents            bool   `json:"push_events"`
		MergeRequestsEvents   bool   `json:"merge_requests_events"`
		EnableSSLVerification bool   `json:"enable_ssl_verification"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
		return
	}

	s.mu.Lock()
	hook := Hook{
		ID:                  int64(4700 + len(s.hooks)),
		ProjectPath:         p.Path,
		URL:                 payload.URL,
		Token:               payload.Token,
		PushEvents:          payload.PushEvents,
		MergeRequestsEvents: payload.MergeRequestsEvents,
		SSLVerification:     payload.EnableSSLVerification,
	}
	s.hooks = append(s.hooks, hook)
	s.mu.Unlock()

	// The real API never echoes the token back.
	writeJSON(w, http.StatusCreated, map[string]any{"id": hook.ID, "url": hook.URL})
}

func (s *Server) createBranch(w http.ResponseWriter, r *http.Request, p *Project) {
	name := r.URL.Query().Get("branch")
	ref := r.URL.Query().Get("ref")
	if name == "" || ref == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "branch and ref are required"})
		return
	}

	s.mu.Lock()
	s.branches = append(s.branches, CreatedBranch{ProjectPath: p.Path, Name: name, Ref: ref})
	s.mu.Unlock()

	writeJSON(w, http.StatusCreated, map[string]any{"name": name})
}

// serveFile answers the existence probe with 404 until the file has been
// written, which is what makes the client choose POST for a create and PUT for
// an update.
func (s *Server) serveFile(w http.ResponseWriter, r *http.Request, p *Project, escapedPath string) {
	filePath, err := url.PathUnescape(escapedPath)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "bad file path"})
		return
	}

	if r.Method == http.MethodGet {
		s.mu.Lock()
		defer s.mu.Unlock()
		for _, f := range s.files {
			if f.ProjectPath == p.Path && f.Path == filePath {
				writeJSON(w, http.StatusOK, map[string]any{"file_path": filePath, "blob_id": "blob-sha"})
				return
			}
		}
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "404 File Not Found"})
		return
	}

	var payload struct {
		Branch        string `json:"branch"`
		Content       string `json:"content"`
		CommitMessage string `json:"commit_message"`
		Encoding      string `json:"encoding"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
		return
	}

	s.mu.Lock()
	s.files = append(s.files, CommittedFile{
		ProjectPath: p.Path,
		Branch:      payload.Branch,
		Path:        filePath,
		Message:     payload.CommitMessage,
		Content:     payload.Content,
		Method:      r.Method,
	})
	s.mu.Unlock()

	status := http.StatusCreated
	if r.Method == http.MethodPut {
		status = http.StatusOK
	}
	writeJSON(w, status, map[string]any{"file_path": filePath, "branch": payload.Branch})
}

func (s *Server) createMergeRequest(w http.ResponseWriter, r *http.Request, p *Project) {
	var payload struct {
		SourceBranch string `json:"source_branch"`
		TargetBranch string `json:"target_branch"`
		Title        string `json:"title"`
		Description  string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
		return
	}

	s.mu.Lock()
	s.nextIID++
	iid := s.nextIID
	s.createdMR = append(s.createdMR, CreatedMergeRequest{
		ProjectPath:  p.Path,
		IID:          iid,
		Title:        payload.Title,
		SourceBranch: payload.SourceBranch,
		TargetBranch: payload.TargetBranch,
		Description:  payload.Description,
	})
	s.mu.Unlock()

	writeJSON(w, http.StatusCreated, mergeRequestJSON(MergeRequest{
		ID:           iid * 1000,
		IID:          iid,
		Title:        payload.Title,
		State:        "opened",
		Author:       "idp-bot",
		SourceBranch: payload.SourceBranch,
		TargetBranch: payload.TargetBranch,
		SHA:          "created-sha",
	}, true))
}

// ── issues, contributors and the write actions ───────────────────────────────

// Issue is a project issue as gitlab.com reports it. Note `iid` and the
// `opened` state — both are places the adapter has to translate, and a fake
// that used GitHub's vocabulary would let a bug through.
type Issue struct {
	ID             int
	IID            int64
	Title          string
	State          string
	Labels         []string
	UserNotesCount int
	AuthorUsername string
}

// Contributor is what /repository/contributors returns: a name and an email,
// and deliberately no username — the asymmetry the platform has to cope with.
type Contributor struct {
	Name    string
	Email   string
	Commits int
}

func (s *Server) serveIssues(w http.ResponseWriter, p *Project) {
	s.mu.Lock()
	closed := make(map[int64]bool, len(s.closedIssues))
	for iid := range s.closedIssues {
		closed[iid] = true
	}
	s.mu.Unlock()

	out := make([]map[string]any, 0, len(p.Issues))
	for _, issue := range p.Issues {
		if closed[issue.IID] {
			continue
		}
		out = append(out, map[string]any{
			"id":               issue.ID,
			"iid":              issue.IID,
			"title":            issue.Title,
			"state":            issue.State,
			"labels":           issue.Labels,
			"user_notes_count": issue.UserNotesCount,
			"web_url":          fmt.Sprintf("https://gitlab.example/-/issues/%d", issue.IID),
			"created_at":       "2026-08-20T10:00:00Z",
			"updated_at":       "2026-08-20T10:00:00Z",
			"author":           map[string]any{"username": issue.AuthorUsername, "name": issue.AuthorUsername},
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// updateIssue implements the `state_event=close` mutation and nothing else —
// that is the only issue write the platform performs.
func (s *Server) updateIssue(w http.ResponseWriter, r *http.Request, p *Project, rest string) {
	iid, err := strconv.ParseInt(strings.Trim(rest, "/"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "bad iid"})
		return
	}
	var payload struct {
		StateEvent string `json:"state_event"`
	}
	_ = json.NewDecoder(r.Body).Decode(&payload)
	if payload.StateEvent != "close" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "unsupported state_event"})
		return
	}

	s.mu.Lock()
	if s.closedIssues == nil {
		s.closedIssues = map[int64]bool{}
	}
	s.closedIssues[iid] = true
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{"iid": iid, "state": "closed"})
}

func (s *Server) serveContributors(w http.ResponseWriter, p *Project) {
	out := make([]map[string]any, 0, len(p.Contributors))
	for _, c := range p.Contributors {
		out = append(out, map[string]any{
			"name":    c.Name,
			"email":   c.Email,
			"commits": c.Commits,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) approveMergeRequest(w http.ResponseWriter, p *Project, rest string) {
	iid, err := strconv.ParseInt(strings.Trim(rest, "/"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "bad iid"})
		return
	}
	s.mu.Lock()
	if s.approvedMRs == nil {
		s.approvedMRs = map[int64]bool{}
	}
	s.approvedMRs[iid] = true
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{"iid": iid, "state": "opened"})
}

// ClosedIssues reports which issues a test caused to be closed.
func (s *Server) ClosedIssues() []int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]int64, 0, len(s.closedIssues))
	for iid := range s.closedIssues {
		out = append(out, iid)
	}
	return out
}

// ApprovedMergeRequests reports which merge requests a test caused to be approved.
func (s *Server) ApprovedMergeRequests() []int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]int64, 0, len(s.approvedMRs))
	for iid := range s.approvedMRs {
		out = append(out, iid)
	}
	return out
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
