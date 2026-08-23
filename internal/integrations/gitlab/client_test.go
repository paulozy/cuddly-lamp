package gitlab

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
)

func newTestClient(srv *httptest.Server) *Client {
	return NewClientWithBaseURL("test-token", srv.URL)
}

// Payload shapes in these tests mirror real gitlab.com responses (captured
// from the public gitlab-org/gitlab-runner project), so a field that only
// exists in this file cannot pass for one the API actually sends.

func TestClient_GetProject(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Nested groups are addressed by percent-encoding the slashes; the
		// server decodes them, so the path arrives whole.
		if r.URL.Path != "/projects/group/subgroup/project" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("Authorization = %q, want a bearer token", got)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"id":                  250833,
			"name":                "project",
			"path_with_namespace": "group/subgroup/project",
			"default_branch":      "main",
			"topics":              []string{"golang"},
			"star_count":          2569,
			"forks_count":         2651,
			"open_issues_count":   nil, // real projects return null with issues disabled
			"visibility":          "public",
		})
	}))
	defer srv.Close()

	project, err := newTestClient(srv).GetProject(context.Background(), "group/subgroup/project")
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if project.ID != 250833 || project.PathWithNamespace != "group/subgroup/project" {
		t.Errorf("project = %+v, want the nested path preserved", project)
	}
	if project.OpenIssuesCount != nil {
		t.Errorf("OpenIssuesCount = %v, want nil for a null field", *project.OpenIssuesCount)
	}
}

func TestClient_EscapesProjectPathInRequestURL(t *testing.T) {
	var rawPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawPath = r.URL.EscapedPath()
		json.NewEncoder(w).Encode(map[string]any{"id": 1})
	}))
	defer srv.Close()

	if _, err := newTestClient(srv).GetProject(context.Background(), "group/sub/project"); err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	// Sending the slashes unescaped would address `/projects/group` and its
	// sub-resources instead of the project.
	if rawPath != "/projects/group%2Fsub%2Fproject" {
		t.Fatalf("escaped path = %q, want the slashes percent-encoded", rawPath)
	}
}

func TestClient_ErrorMapping(t *testing.T) {
	tests := []struct {
		status int
		want   error
	}{
		{status: http.StatusNotFound, want: ErrNotFound},
		{status: http.StatusUnauthorized, want: ErrUnauthorized},
		// GitLab answers 403 for insufficient token scope, and throttles with
		// 429 — the opposite of GitHub's use of 403 for rate limiting.
		{status: http.StatusForbidden, want: ErrUnauthorized},
		{status: http.StatusTooManyRequests, want: ErrRateLimited},
	}

	for _, tt := range tests {
		t.Run(strconv.Itoa(tt.status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
			}))
			defer srv.Close()

			_, err := newTestClient(srv).GetProject(context.Background(), "owner/repo")
			if err != tt.want {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestClient_GetLanguagesReturnsPercentages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"Go":98.31,"Shell":0.91,"Makefile":0.48}`))
	}))
	defer srv.Close()

	languages, err := newTestClient(srv).GetLanguages(context.Background(), "owner/repo")
	if err != nil {
		t.Fatalf("GetLanguages: %v", err)
	}
	if languages["Go"] != 98.31 || languages["Makefile"] != 0.48 {
		t.Fatalf("languages = %v, want the raw percentages", languages)
	}
}

func TestClient_GetTree_FollowsPagination(t *testing.T) {
	var requestedPages []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		requestedPages = append(requestedPages, page)
		if page == "1" {
			w.Header().Set("x-next-page", "2")
			json.NewEncoder(w).Encode([]TreeEntry{{Path: "cmd", Type: "tree"}, {Path: "cmd/main.go", Type: "blob"}})
			return
		}
		// Last page: GitLab leaves x-next-page empty.
		w.Header().Set("x-next-page", "")
		json.NewEncoder(w).Encode([]TreeEntry{{Path: ".gitlab-ci.yml", Type: "blob"}})
	}))
	defer srv.Close()

	tree, err := newTestClient(srv).GetTree(context.Background(), "owner/repo", "main")
	if err != nil {
		t.Fatalf("GetTree: %v", err)
	}
	if len(requestedPages) != 2 || requestedPages[1] != "2" {
		t.Fatalf("requested pages = %v, want both pages fetched", requestedPages)
	}
	if len(tree.Entries) != 3 {
		t.Fatalf("entries = %d, want all 3 across both pages", len(tree.Entries))
	}
	// Every page was read, so nothing is missing and the tree is not truncated.
	if tree.Truncated {
		t.Error("Truncated = true, but pagination completed")
	}
}

func TestClient_GetTree_MarksTruncatedAtThePageCeiling(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		// An endless repository: there is always another page.
		w.Header().Set("x-next-page", strconv.Itoa(page+1))
		json.NewEncoder(w).Encode([]TreeEntry{{Path: fmt.Sprintf("file-%d.go", page), Type: "blob"}})
	}))
	defer srv.Close()

	tree, err := newTestClient(srv).GetTree(context.Background(), "owner/repo", "main")
	if err != nil {
		t.Fatalf("GetTree: %v", err)
	}
	if calls != maxTreePages {
		t.Fatalf("calls = %d, want the walk bounded at %d pages", calls, maxTreePages)
	}
	// GitLab sends no truncation flag of its own. Without synthesizing one
	// here, a file we simply never fetched would read downstream as a file
	// that does not exist — which is how a repository with CI gets reported as
	// having none.
	if !tree.Truncated {
		t.Fatal("Truncated = false after stopping short of the last page")
	}
}

func TestClient_ListMergeRequestDiffs_FollowsPagination(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/projects/owner%2Frepo/merge_requests/7222/diffs" &&
			r.URL.Path != "/projects/owner/repo/merge_requests/7222/diffs" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		if r.URL.Query().Get("page") == "1" {
			w.Header().Set("x-next-page", "2")
			json.NewEncoder(w).Encode([]Diff{{NewPath: "a.go", Diff: "@@\n+x"}})
			return
		}
		json.NewEncoder(w).Encode([]Diff{{NewPath: "b.go", Diff: "@@\n-y"}})
	}))
	defer srv.Close()

	diffs, err := newTestClient(srv).ListMergeRequestDiffs(context.Background(), "owner/repo", 7222)
	if err != nil {
		t.Fatalf("ListMergeRequestDiffs: %v", err)
	}
	if len(diffs) != 2 {
		t.Fatalf("diffs = %d, want 2 across both pages", len(diffs))
	}
}

func TestMergeRequest_ChangedFileCount(t *testing.T) {
	tests := []struct {
		raw string
		// nil means "GitLab said nothing usable", which is a different fact from
		// a merge request that touches no files.
		want *int
	}{
		{raw: "2", want: intPtr(2)},
		// GitLab documents changes_count as a string, and it reports "1000+"
		// for diffs too large to count. Neither may crash or invent a number.
		{raw: "1000+", want: intPtr(1000)},
		{raw: "", want: nil},
		{raw: "unknown", want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			mr := MergeRequest{ChangesCount: tt.raw}
			got := mr.ChangedFileCount()
			switch {
			case tt.want == nil && got != nil:
				t.Fatalf("ChangedFileCount() = %d, want nil (unreported)", *got)
			case tt.want != nil && got == nil:
				t.Fatalf("ChangedFileCount() = nil, want %d", *tt.want)
			case tt.want != nil && *got != *tt.want:
				t.Fatalf("ChangedFileCount() = %d, want %d", *got, *tt.want)
			}
		})
	}
}

func TestClient_UpsertFile_CreatesThenUpdates(t *testing.T) {
	tests := []struct {
		name       string
		fileExists bool
		wantMethod string
	}{
		// GitLab splits creation and update: POST fails on an existing file and
		// PUT fails on a missing one, so the method has to be chosen.
		{name: "new file", fileExists: false, wantMethod: http.MethodPost},
		{name: "existing file", fileExists: true, wantMethod: http.MethodPut},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var writeMethod string
			var body map[string]string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					if !tt.fileExists {
						w.WriteHeader(http.StatusNotFound)
						return
					}
					json.NewEncoder(w).Encode(map[string]string{"file_path": "docs/ARCHITECTURE.md"})
					return
				}
				writeMethod = r.Method
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatalf("decode body: %v", err)
				}
				w.WriteHeader(http.StatusCreated)
			}))
			defer srv.Close()

			err := newTestClient(srv).UpsertFile(context.Background(), "owner/repo", "docs/auto", "docs/ARCHITECTURE.md", "docs: update", "# Architecture")
			if err != nil {
				t.Fatalf("UpsertFile: %v", err)
			}
			if writeMethod != tt.wantMethod {
				t.Errorf("method = %s, want %s", writeMethod, tt.wantMethod)
			}
			if body["content"] != "# Architecture" || body["branch"] != "docs/auto" || body["commit_message"] != "docs: update" {
				t.Errorf("body = %v, want the content committed verbatim", body)
			}
			// GitLab defaults to text encoding, but being explicit keeps a
			// markdown file from ever being read as base64.
			if body["encoding"] != "text" {
				t.Errorf("encoding = %q, want text", body["encoding"])
			}
		})
	}
}

func TestClient_CreateBranch(t *testing.T) {
	var query url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		query = r.URL.Query()
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{"name": "docs/auto"})
	}))
	defer srv.Close()

	if err := newTestClient(srv).CreateBranch(context.Background(), "owner/repo", "main", "docs/auto"); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}
	if query.Get("branch") != "docs/auto" || query.Get("ref") != "main" {
		t.Fatalf("query = %v, want branch=docs/auto ref=main", query)
	}
}

func TestClient_CreateHook_SendsSecretAsToken(t *testing.T) {
	var payload map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		json.NewEncoder(w).Encode(map[string]any{"id": 99})
	}))
	defer srv.Close()

	id, err := newTestClient(srv).CreateHook(context.Background(), "owner/repo", "https://idp.example.com/api/v1/webhooks/gitlab/repo-1", "s3cret")
	if err != nil {
		t.Fatalf("CreateHook: %v", err)
	}
	if id != 99 {
		t.Errorf("id = %d, want 99", id)
	}
	if payload["token"] != "s3cret" {
		t.Errorf("token = %v, want the secret to be registered", payload["token"])
	}
	if payload["merge_requests_events"] != true || payload["push_events"] != true {
		t.Errorf("payload = %v, want push and merge request events subscribed", payload)
	}
	if payload["enable_ssl_verification"] != true {
		t.Error("SSL verification must stay on")
	}
}

func TestValidateWebhookToken(t *testing.T) {
	tests := []struct {
		name     string
		secret   string
		received string
		want     bool
	}{
		{name: "match", secret: "s3cret", received: "s3cret", want: true},
		{name: "mismatch", secret: "s3cret", received: "wrong", want: false},
		// An empty header must never pass, and neither must an unconfigured
		// secret — otherwise a repository with no secret accepts anything.
		{name: "empty header", secret: "s3cret", received: "", want: false},
		{name: "empty secret", secret: "", received: "anything", want: false},
		{name: "both empty", secret: "", received: "", want: false},
		{name: "prefix of secret", secret: "s3cret", received: "s3c", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ValidateWebhookToken(tt.secret, tt.received); got != tt.want {
				t.Fatalf("ValidateWebhookToken(%q, %q) = %v, want %v", tt.secret, tt.received, got, tt.want)
			}
		})
	}
}

func TestClient_ListIssues(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/projects/group/project/issues" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		// GitLab says `opened`, not `open`; the adapter is what normalizes it.
		if got := r.URL.Query().Get("state"); got != "opened" {
			t.Errorf("state = %q, want opened", got)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[
			{"id": 9001, "iid": 88, "title": "Sidebar does not collapse", "state": "opened",
			 "labels": ["bug", "ui"], "user_notes_count": 3,
			 "web_url": "https://gitlab.com/group/project/-/issues/88",
			 "author": {"username": "julia.r", "name": "Julia R"}}
		]`)
	}))
	defer srv.Close()

	got, err := newTestClient(srv).ListIssues(context.Background(), "group/project")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d issues, want 1", len(got))
	}
	// IID is the number people cite; ID is the global one and must not leak.
	if got[0].IID != 88 {
		t.Errorf("iid = %d, want 88", got[0].IID)
	}
	if got[0].UserNotesCount != 3 {
		t.Errorf("user_notes_count = %d, want 3", got[0].UserNotesCount)
	}
	if got[0].Author.Username != "julia.r" {
		t.Errorf("author = %q, want julia.r", got[0].Author.Username)
	}
}

func TestClient_ListContributors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/projects/group/project/repository/contributors" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		// Note the shape: a name and an email, and no username anywhere.
		fmt.Fprint(w, `[
			{"name": "Paulo Abreu", "email": "paulo@example.com", "commits": 240},
			{"name": "Diego Souza", "email": "diego@example.com", "commits": 58}
		]`)
	}))
	defer srv.Close()

	got, err := newTestClient(srv).ListContributors(context.Background(), "group/project")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d contributors, want 2", len(got))
	}
	if got[0].Name != "Paulo Abreu" || got[0].Commits != 240 {
		t.Errorf("first contributor = %+v, want Paulo Abreu with 240 commits", got[0])
	}
}

func intPtr(v int) *int { return &v }
