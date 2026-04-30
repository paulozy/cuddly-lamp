package github

import (
	"context"
	"encoding/base64"
	"encoding/json"
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
