package github

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTestClient(srv *httptest.Server) *Client {
	c := NewClient("test-token")
	c.baseURL = srv.URL
	return c
}

func TestClient_GetRepository(t *testing.T) {
	want := RepoInfo{
		ID:              12345,
		Name:            "repo",
		FullName:        "owner/repo",
		DefaultBranch:   "main",
		Language:        "Go",
		Topics:          []string{"go", "api"},
		StargazersCount: 10,
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/repo" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(want)
	}))
	defer srv.Close()

	got, err := newTestClient(srv).GetRepository(context.Background(), "owner", "repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != want.ID || got.FullName != want.FullName {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestClient_GetRepository_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := newTestClient(srv).GetRepository(context.Background(), "owner", "missing")
	if err != ErrNotFound {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestClient_GetRepository_RateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	_, err := newTestClient(srv).GetRepository(context.Background(), "owner", "repo")
	if err != ErrRateLimited {
		t.Errorf("err = %v, want ErrRateLimited", err)
	}
}

func TestClient_GetBranches(t *testing.T) {
	want := []Branch{{Name: "main"}, {Name: "dev"}}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(want)
	}))
	defer srv.Close()

	got, err := newTestClient(srv).GetBranches(context.Background(), "owner", "repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != len(want) {
		t.Errorf("len(branches) = %d, want %d", len(got), len(want))
	}
}

func TestClient_GetCommits(t *testing.T) {
	want := []Commit{{SHA: "abc123"}, {SHA: "def456"}}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(want)
	}))
	defer srv.Close()

	got, err := newTestClient(srv).GetCommits(context.Background(), "owner", "repo", "main", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != len(want) {
		t.Errorf("len(commits) = %d, want %d", len(got), len(want))
	}
}

func TestClient_ListPullRequests(t *testing.T) {
	want := []PullRequest{{Number: 1, Title: "fix bug", State: "open"}}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(want)
	}))
	defer srv.Close()

	got, err := newTestClient(srv).ListPullRequests(context.Background(), "owner", "repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].Number != 1 {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestClient_GetPullRequest_ParsesHeadAndBaseRefs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/repos/owner/repo/pulls/42" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"number": 42,
			"title":  "fix",
			"head": map[string]string{
				"ref": "feature",
				"sha": "head-sha",
			},
			"base": map[string]string{
				"ref": "main",
				"sha": "base-sha",
			},
		})
	}))
	defer srv.Close()

	pr, err := newTestClient(srv).GetPullRequest(context.Background(), "owner", "repo", 42)
	if err != nil {
		t.Fatalf("GetPullRequest returned error: %v", err)
	}
	if pr.Head.DisplayName() != "feature" || pr.Head.SHA != "head-sha" {
		t.Fatalf("head = %+v, want feature/head-sha", pr.Head)
	}
	if pr.Base.DisplayName() != "main" || pr.Base.SHA != "base-sha" {
		t.Fatalf("base = %+v, want main/base-sha", pr.Base)
	}
}

func TestCreateBranch(t *testing.T) {
	var createdRef string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo/git/ref/heads/main":
			json.NewEncoder(w).Encode(map[string]any{"object": map[string]string{"sha": "base-sha"}})
		case r.Method == http.MethodPost && r.URL.Path == "/repos/owner/repo/git/refs":
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			createdRef = body["ref"]
			if body["sha"] != "base-sha" {
				t.Fatalf("sha = %q, want base-sha", body["sha"])
			}
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]string{"ref": createdRef})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	if err := newTestClient(srv).CreateBranch(context.Background(), "owner", "repo", "main", "docs/auto"); err != nil {
		t.Fatalf("CreateBranch returned error: %v", err)
	}
	if createdRef != "refs/heads/docs/auto" {
		t.Fatalf("createdRef = %q, want refs/heads/docs/auto", createdRef)
	}
}

func TestCreateOrUpdateFile(t *testing.T) {
	var gotContent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo/contents/CONTRIBUTING.md":
			json.NewEncoder(w).Encode(map[string]string{"sha": "file-sha"})
		case r.Method == http.MethodPut && r.URL.Path == "/repos/owner/repo/contents/CONTRIBUTING.md":
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if body["sha"] != "file-sha" {
				t.Fatalf("sha = %q, want file-sha", body["sha"])
			}
			raw, err := base64.StdEncoding.DecodeString(body["content"])
			if err != nil {
				t.Fatalf("decode content: %v", err)
			}
			gotContent = string(raw)
			json.NewEncoder(w).Encode(map[string]any{})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	if err := newTestClient(srv).CreateOrUpdateFile(context.Background(), "owner", "repo", "docs/auto", "CONTRIBUTING.md", "docs", "# Guidelines"); err != nil {
		t.Fatalf("CreateOrUpdateFile returned error: %v", err)
	}
	if gotContent != "# Guidelines" {
		t.Fatalf("content = %q, want markdown", gotContent)
	}
}

func TestCreatePullRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/repos/owner/repo/pulls" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["head"] != "docs/auto" || body["base"] != "main" {
			t.Fatalf("body = %+v, want head/base", body)
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(PullRequest{Number: 12, HTMLURL: "https://github.com/owner/repo/pull/12"})
	}))
	defer srv.Close()

	pr, err := newTestClient(srv).CreatePullRequest(context.Background(), "owner", "repo", "docs", "docs/auto", "main", "body")
	if err != nil {
		t.Fatalf("CreatePullRequest returned error: %v", err)
	}
	if pr.Number != 12 || pr.HTMLURL == "" {
		t.Fatalf("pr = %+v, want number and url", pr)
	}
}

// GitHub's issues endpoint returns pull requests alongside real issues. This
// is the whole reason ListIssues exists rather than a bare `do` call: without
// the filter, every open PR would be listed twice in the UI — once under Pull
// requests and once under Issues.
func TestClient_ListIssues_ExcludesPullRequests(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/repo/issues" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if got := r.URL.Query().Get("state"); got != "open" {
			t.Errorf("state = %q, want open", got)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `[
			{"number": 88, "title": "Sidebar does not collapse", "state": "open",
			 "user": {"login": "julia.r"}, "comments": 3,
			 "labels": [{"name": "bug"}, {"name": "ui"}]},
			{"number": 412, "title": "Extract AppShell", "state": "open",
			 "user": {"login": "ana.m"}, "pull_request": {"url": "https://api.github.com/pulls/412"}}
		]`)
	}))
	defer srv.Close()

	got, err := newTestClient(srv).ListIssues(context.Background(), "owner", "repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d issues, want 1 (the pull request must be filtered out)", len(got))
	}
	if got[0].Number != 88 {
		t.Errorf("number = %d, want 88", got[0].Number)
	}
	if got[0].Comments != 3 {
		t.Errorf("comments = %d, want 3", got[0].Comments)
	}
	if labels := got[0].LabelNames(); len(labels) != 2 || labels[0] != "bug" || labels[1] != "ui" {
		t.Errorf("labels = %v, want [bug ui]", labels)
	}
}

func TestClient_ListIssues_EmptyIsNotNil(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `[]`)
	}))
	defer srv.Close()

	got, err := newTestClient(srv).ListIssues(context.Background(), "owner", "repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Error("got nil, want an empty slice — a nil marshals to JSON null, not []")
	}
}

func TestClient_ListContributors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/repo/contributors" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `[
			{"login": "paulozy", "contributions": 210, "avatar_url": "https://x/1"},
			{"login": "ana-m", "contributions": 132, "avatar_url": "https://x/2"}
		]`)
	}))
	defer srv.Close()

	got, err := newTestClient(srv).ListContributors(context.Background(), "owner", "repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d contributors, want 2", len(got))
	}
	if got[0].Login != "paulozy" || got[0].Contributions != 210 {
		t.Errorf("first contributor = %+v, want paulozy with 210", got[0])
	}
}

func TestClient_ListContributors_Unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	if _, err := newTestClient(srv).ListContributors(context.Background(), "owner", "repo"); err != ErrUnauthorized {
		t.Errorf("got %v, want ErrUnauthorized", err)
	}
}
