// Package gitlab is a thin REST v4 client for gitlab.com. It speaks GitLab's
// own wire format and knows nothing about the platform's neutral types — the
// translation lives in internal/integrations/scm.
package gitlab

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DefaultBaseURL is gitlab.com's API root. Self-hosted instances are out of
// scope for now; the organization config carries a `gitlab_base_url` column so
// supporting them later needs no migration.
const DefaultBaseURL = "https://gitlab.com/api/v4"

var (
	ErrUnauthorized = errors.New("gitlab: unauthorized — check your token")
	ErrRateLimited  = errors.New("gitlab: API rate limit exceeded")
	ErrNotFound     = errors.New("gitlab: resource not found")
)

type Client struct {
	token      string
	httpClient *http.Client
	baseURL    string
}

func NewClient(token string) *Client {
	return NewClientWithBaseURL(token, DefaultBaseURL)
}

func NewClientWithBaseURL(token, baseURL string) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{
		token:      token,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		baseURL:    strings.TrimSuffix(baseURL, "/"),
	}
}

// projectPath URL-encodes a project path for use as the `:id` path segment.
// GitLab identifies a project either by numeric ID or by its full path with
// the slashes percent-encoded, which is what makes nested groups addressable.
func projectPath(path string) string {
	return url.PathEscape(strings.Trim(path, "/"))
}

// do issues a request and decodes the JSON body into v. It returns the
// response headers so paginated callers can read `x-next-page`.
func (c *Client) do(ctx context.Context, method, path string, body io.Reader, v interface{}) (http.Header, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	// Bearer works for personal, project and group access tokens as well as
	// OAuth tokens, so one header covers every token this platform stores.
	// https://docs.gitlab.com/api/rest/authentication/
	//
	// With no token the header is omitted rather than sent empty: GitLab
	// rejects `Bearer ` outright, while an anonymous request still reads
	// public projects.
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		// GitLab answers 403 for a token whose scopes are too narrow, which is
		// an authorization problem — unlike GitHub, it uses 429 for throttling.
		return resp.Header, ErrUnauthorized
	case http.StatusTooManyRequests:
		return resp.Header, ErrRateLimited
	case http.StatusNotFound:
		return resp.Header, ErrNotFound
	case http.StatusNoContent:
		return resp.Header, nil
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return resp.Header, fmt.Errorf("gitlab API error %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	if v != nil {
		if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
			return resp.Header, fmt.Errorf("decode response: %w", err)
		}
	}
	return resp.Header, nil
}

// Project is a GitLab project, the equivalent of a GitHub repository.
type Project struct {
	ID                int      `json:"id"`
	Name              string   `json:"name"`
	PathWithNamespace string   `json:"path_with_namespace"`
	Description       string   `json:"description"`
	DefaultBranch     string   `json:"default_branch"`
	Topics            []string `json:"topics"`
	StarCount         int      `json:"star_count"`
	ForksCount        int      `json:"forks_count"`
	// OpenIssuesCount is absent when the project has issues disabled, which is
	// why it is a pointer: 0 and "not reported" are different facts.
	OpenIssuesCount *int   `json:"open_issues_count"`
	Visibility      string `json:"visibility"`
	WebURL          string `json:"web_url"`
}

func (c *Client) GetProject(ctx context.Context, path string) (*Project, error) {
	var project Project
	if _, err := c.do(ctx, http.MethodGet, "/projects/"+projectPath(path), nil, &project); err != nil {
		return nil, err
	}
	return &project, nil
}

type Branch struct {
	Name   string `json:"name"`
	Commit struct {
		ID string `json:"id"`
	} `json:"commit"`
}

func (c *Client) ListBranches(ctx context.Context, path string) ([]Branch, error) {
	var branches []Branch
	endpoint := fmt.Sprintf("/projects/%s/repository/branches?per_page=100", projectPath(path))
	if _, err := c.do(ctx, http.MethodGet, endpoint, nil, &branches); err != nil {
		return nil, err
	}
	return branches, nil
}

type Commit struct {
	ID            string    `json:"id"`
	Message       string    `json:"message"`
	AuthorName    string    `json:"author_name"`
	CommittedDate time.Time `json:"committed_date"`
}

func (c *Client) ListCommits(ctx context.Context, path, ref string, limit int) ([]Commit, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	var commits []Commit
	endpoint := fmt.Sprintf("/projects/%s/repository/commits?ref_name=%s&per_page=%d",
		projectPath(path), url.QueryEscape(ref), limit)
	if _, err := c.do(ctx, http.MethodGet, endpoint, nil, &commits); err != nil {
		return nil, err
	}
	return commits, nil
}

// GetLanguages returns the language breakdown as percentages of the codebase.
//
// This differs from GitHub, which reports byte counts. The values are only
// meaningful relative to each other in both cases, so callers comparing
// magnitudes across providers would be wrong either way.
func (c *Client) GetLanguages(ctx context.Context, path string) (map[string]float64, error) {
	var languages map[string]float64
	endpoint := fmt.Sprintf("/projects/%s/languages", projectPath(path))
	if _, err := c.do(ctx, http.MethodGet, endpoint, nil, &languages); err != nil {
		return nil, err
	}
	return languages, nil
}

// TreeEntry is one node of a repository listing.
type TreeEntry struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"` // "blob" or "tree"
	Path string `json:"path"`
	Mode string `json:"mode"`
}

// Tree is a recursive listing assembled from GitLab's paginated response.
//
// GitLab has no `truncated` flag of its own: it just keeps paginating. The
// ceiling below is ours, so Truncated is what the client synthesizes in its
// place — and it must be honoured, because the recursive listing is
// depth-first, so a root-level file can legitimately land on a late page.
type Tree struct {
	Entries   []TreeEntry
	Truncated bool
}

// maxTreePages bounds one sync's tree walk. 30 pages of 100 entries covers
// repositories well past the size where this listing is still useful, while
// keeping a single sync from turning into hundreds of sequential requests.
const maxTreePages = 30

func (c *Client) GetTree(ctx context.Context, path, ref string) (*Tree, error) {
	tree := &Tree{Entries: make([]TreeEntry, 0, 100)}
	page := 1
	for {
		var entries []TreeEntry
		endpoint := fmt.Sprintf("/projects/%s/repository/tree?recursive=true&per_page=100&page=%d&ref=%s",
			projectPath(path), page, url.QueryEscape(ref))
		header, err := c.do(ctx, http.MethodGet, endpoint, nil, &entries)
		if err != nil {
			return nil, err
		}
		tree.Entries = append(tree.Entries, entries...)

		next := strings.TrimSpace(header.Get("x-next-page"))
		if next == "" {
			return tree, nil
		}
		if page >= maxTreePages {
			// More pages exist and we are not fetching them, so absence of a
			// path from here on proves nothing.
			tree.Truncated = true
			return tree, nil
		}
		nextPage, convErr := strconv.Atoi(next)
		if convErr != nil || nextPage <= page {
			return tree, nil
		}
		page = nextPage
	}
}

// MergeRequest is GitLab's change request. Note IID versus ID: IID is the
// per-project number users see and every API path uses; ID is global.
type MergeRequest struct {
	ID           int64  `json:"id"`
	IID          int64  `json:"iid"`
	Title        string `json:"title"`
	Description  string `json:"description"`
	State        string `json:"state"` // opened, closed, merged, locked
	Draft        bool   `json:"draft"`
	SourceBranch string `json:"source_branch"`
	TargetBranch string `json:"target_branch"`
	SHA          string `json:"sha"`
	WebURL       string `json:"web_url"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
	MergedAt     string `json:"merged_at"`
	Author       struct {
		Username string `json:"username"`
		Name     string `json:"name"`
	} `json:"author"`
	DiffRefs struct {
		BaseSHA  string `json:"base_sha"`
		HeadSHA  string `json:"head_sha"`
		StartSHA string `json:"start_sha"`
	} `json:"diff_refs"`
	// ChangesCount is documented as a string, not an integer, and can be
	// "1000+" when the diff is too large to count exactly.
	ChangesCount string `json:"changes_count"`
}

// ChangedFileCount parses ChangesCount, returning 0 when GitLab reported
// nothing or something non-numeric like "1000+".
// ChangedFileCount parses `changes_count`, which GitLab documents as a string
// and can send as "1000+" when the diff is too large to count exactly.
//
// nil means GitLab reported nothing usable — absent on the list endpoint, or a
// value that did not parse. That is not the same as a merge request touching no
// files, and callers must not render it as zero.
func (m *MergeRequest) ChangedFileCount() *int {
	digits := strings.TrimRight(strings.TrimSpace(m.ChangesCount), "+")
	n, err := strconv.Atoi(digits)
	if err != nil {
		return nil
	}
	return &n
}

func (c *Client) ListMergeRequests(ctx context.Context, path string) ([]MergeRequest, error) {
	var mrs []MergeRequest
	endpoint := fmt.Sprintf("/projects/%s/merge_requests?state=opened&per_page=100&order_by=updated_at",
		projectPath(path))
	if _, err := c.do(ctx, http.MethodGet, endpoint, nil, &mrs); err != nil {
		return nil, err
	}
	return mrs, nil
}

// Issue is GitLab's issue payload. Unlike GitHub, GitLab keeps issues and
// merge requests on separate endpoints, so nothing needs filtering here.
type Issue struct {
	ID     int64    `json:"id"`
	IID    int64    `json:"iid"`
	Title  string   `json:"title"`
	State  string   `json:"state"` // opened, closed
	Labels []string `json:"labels"`
	// UserNotesCount counts discussion notes, GitLab's equivalent of comments.
	UserNotesCount int    `json:"user_notes_count"`
	WebURL         string `json:"web_url"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
	Author         struct {
		Username string `json:"username"`
		Name     string `json:"name"`
	} `json:"author"`
}

// Contributor is GitLab's contributor payload. It identifies people by name
// and email — there is no username here, and no activity timestamp.
type Contributor struct {
	Name      string `json:"name"`
	Email     string `json:"email"`
	Commits   int    `json:"commits"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
}

// ListIssues returns the open issues for a project, most recently updated
// first.
func (c *Client) ListIssues(ctx context.Context, path string) ([]Issue, error) {
	var issues []Issue
	endpoint := fmt.Sprintf("/projects/%s/issues?state=opened&per_page=100&order_by=updated_at",
		projectPath(path))
	if _, err := c.do(ctx, http.MethodGet, endpoint, nil, &issues); err != nil {
		return nil, err
	}
	return issues, nil
}

// CloseIssue closes an issue. GitLab mutates state through `state_event`
// rather than by assigning the state directly.
func (c *Client) CloseIssue(ctx context.Context, path string, iid int64) error {
	body, err := json.Marshal(map[string]string{"state_event": "close"})
	if err != nil {
		return fmt.Errorf("marshal close issue: %w", err)
	}
	endpoint := fmt.Sprintf("/projects/%s/issues/%d", projectPath(path), iid)
	_, err = c.do(ctx, http.MethodPut, endpoint, bytes.NewReader(body), nil)
	return err
}

// ApproveMergeRequest records an approval.
//
// GitLab has no REST counterpart to GitHub's REQUEST_CHANGES review event that
// is stable across versions, so only the positive verdict is implemented here;
// the adapter reports the other as an unsupported capability.
func (c *Client) ApproveMergeRequest(ctx context.Context, path string, iid int64) error {
	endpoint := fmt.Sprintf("/projects/%s/merge_requests/%d/approve", projectPath(path), iid)
	_, err := c.do(ctx, http.MethodPost, endpoint, nil, nil)
	return err
}

// CreateMergeRequestNote posts a discussion note. It is how attribution
// survives an action taken with the organization's token — see the handler.
func (c *Client) CreateMergeRequestNote(ctx context.Context, path string, iid int64, note string) error {
	body, err := json.Marshal(map[string]string{"body": note})
	if err != nil {
		return fmt.Errorf("marshal note: %w", err)
	}
	endpoint := fmt.Sprintf("/projects/%s/merge_requests/%d/notes", projectPath(path), iid)
	_, err = c.do(ctx, http.MethodPost, endpoint, bytes.NewReader(body), nil)
	return err
}

// ListContributors returns contributors to the default branch, ordered by
// commit count so the shape matches GitHub's.
func (c *Client) ListContributors(ctx context.Context, path string) ([]Contributor, error) {
	var contributors []Contributor
	endpoint := fmt.Sprintf("/projects/%s/repository/contributors?per_page=100&order_by=commits&sort=desc",
		projectPath(path))
	if _, err := c.do(ctx, http.MethodGet, endpoint, nil, &contributors); err != nil {
		return nil, err
	}
	return contributors, nil
}

func (c *Client) GetMergeRequest(ctx context.Context, path string, iid int64) (*MergeRequest, error) {
	var mr MergeRequest
	endpoint := fmt.Sprintf("/projects/%s/merge_requests/%d", projectPath(path), iid)
	if _, err := c.do(ctx, http.MethodGet, endpoint, nil, &mr); err != nil {
		return nil, err
	}
	return &mr, nil
}

// Diff is one file's change within a merge request.
type Diff struct {
	OldPath     string `json:"old_path"`
	NewPath     string `json:"new_path"`
	NewFile     bool   `json:"new_file"`
	RenamedFile bool   `json:"renamed_file"`
	DeletedFile bool   `json:"deleted_file"`
	// Diff is the unified diff body, starting at the first @@ hunk header —
	// the same shape GitHub calls `patch`.
	Diff      string `json:"diff"`
	TooLarge  bool   `json:"too_large"`
	Collapsed bool   `json:"collapsed"`
}

// maxDiffPages bounds the per-merge-request diff walk at 1000 files.
const maxDiffPages = 10

// ListMergeRequestDiffs returns the files changed by a merge request.
func (c *Client) ListMergeRequestDiffs(ctx context.Context, path string, iid int64) ([]Diff, error) {
	out := make([]Diff, 0, 20)
	page := 1
	for {
		var diffs []Diff
		endpoint := fmt.Sprintf("/projects/%s/merge_requests/%d/diffs?per_page=100&page=%d",
			projectPath(path), iid, page)
		header, err := c.do(ctx, http.MethodGet, endpoint, nil, &diffs)
		if err != nil {
			return nil, err
		}
		out = append(out, diffs...)

		next := strings.TrimSpace(header.Get("x-next-page"))
		if next == "" || page >= maxDiffPages {
			return out, nil
		}
		nextPage, convErr := strconv.Atoi(next)
		if convErr != nil || nextPage <= page {
			return out, nil
		}
		page = nextPage
	}
}

func (c *Client) CreateBranch(ctx context.Context, path, baseBranch, newBranch string) error {
	endpoint := fmt.Sprintf("/projects/%s/repository/branches?branch=%s&ref=%s",
		projectPath(path), url.QueryEscape(newBranch), url.QueryEscape(baseBranch))
	_, err := c.do(ctx, http.MethodPost, endpoint, nil, nil)
	return err
}

type fileRequest struct {
	Branch        string `json:"branch"`
	Content       string `json:"content"`
	CommitMessage string `json:"commit_message"`
	Encoding      string `json:"encoding"`
}

// UpsertFile creates filePath on branch, or updates it when it already exists.
//
// GitLab splits creation and update across POST and PUT, so existence has to
// be probed first — POSTing over an existing file fails, and PUTting a missing
// one does too.
func (c *Client) UpsertFile(ctx context.Context, path, branch, filePath, message, content string) error {
	method := http.MethodPost
	if exists, err := c.fileExists(ctx, path, branch, filePath); err != nil {
		return err
	} else if exists {
		method = http.MethodPut
	}

	payload := fileRequest{Branch: branch, Content: content, CommitMessage: message, Encoding: "text"}
	body, err := jsonReader(payload)
	if err != nil {
		return err
	}
	endpoint := fmt.Sprintf("/projects/%s/repository/files/%s",
		projectPath(path), url.PathEscape(strings.TrimPrefix(filePath, "/")))
	_, err = c.do(ctx, method, endpoint, body, nil)
	return err
}

func (c *Client) fileExists(ctx context.Context, path, ref, filePath string) (bool, error) {
	endpoint := fmt.Sprintf("/projects/%s/repository/files/%s?ref=%s",
		projectPath(path), url.PathEscape(strings.TrimPrefix(filePath, "/")), url.QueryEscape(ref))
	if _, err := c.do(ctx, http.MethodGet, endpoint, nil, nil); err != nil {
		if errors.Is(err, ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

type createMergeRequestPayload struct {
	SourceBranch string `json:"source_branch"`
	TargetBranch string `json:"target_branch"`
	Title        string `json:"title"`
	Description  string `json:"description"`
}

func (c *Client) CreateMergeRequest(ctx context.Context, path, source, target, title, description string) (*MergeRequest, error) {
	body, err := jsonReader(createMergeRequestPayload{
		SourceBranch: source,
		TargetBranch: target,
		Title:        title,
		Description:  description,
	})
	if err != nil {
		return nil, err
	}
	var mr MergeRequest
	endpoint := fmt.Sprintf("/projects/%s/merge_requests", projectPath(path))
	if _, err := c.do(ctx, http.MethodPost, endpoint, body, &mr); err != nil {
		return nil, err
	}
	return &mr, nil
}

type createHookPayload struct {
	URL                   string `json:"url"`
	Token                 string `json:"token"`
	PushEvents            bool   `json:"push_events"`
	MergeRequestsEvents   bool   `json:"merge_requests_events"`
	IssuesEvents          bool   `json:"issues_events"`
	EnableSSLVerification bool   `json:"enable_ssl_verification"`
}

// HookEvents are the events CreateHook subscribes to. They are named the way
// GitLab names them, so the stored webhook config reflects what was actually
// registered rather than GitHub's vocabulary.
var HookEvents = []string{"push", "merge_request", "issues"}

type hookResponse struct {
	ID int64 `json:"id"`
}

// CreateHook registers a webhook with a secret token. GitLab sends that token
// back verbatim in the X-Gitlab-Token header on every delivery.
//
// GitLab 19.1 also supports a `signing_token` that yields real HMAC-SHA256
// signatures, which would be preferable — but it is not wired up here because
// the platform must keep working against the GitLab versions in the field, and
// the plain token path is the one available everywhere.
func (c *Client) CreateHook(ctx context.Context, path, webhookURL, secret string) (int64, error) {
	body, err := jsonReader(createHookPayload{
		URL:                   webhookURL,
		Token:                 secret,
		PushEvents:            true,
		MergeRequestsEvents:   true,
		IssuesEvents:          true,
		EnableSSLVerification: true,
	})
	if err != nil {
		return 0, err
	}
	var hook hookResponse
	endpoint := fmt.Sprintf("/projects/%s/hooks", projectPath(path))
	if _, err := c.do(ctx, http.MethodPost, endpoint, body, &hook); err != nil {
		return 0, err
	}
	return hook.ID, nil
}

func jsonReader(payload interface{}) (io.Reader, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	return strings.NewReader(string(data)), nil
}
