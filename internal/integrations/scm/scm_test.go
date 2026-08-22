package scm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/paulozy/idp-with-ai-backend/internal/integrations/github"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
)

func TestParseRepoRef(t *testing.T) {
	tests := []struct {
		name          string
		path          string
		wantNamespace string
		wantName      string
		wantErr       bool
	}{
		{name: "owner and repo", path: "owner/repo", wantNamespace: "owner", wantName: "repo"},
		{name: "surrounding slashes", path: "/owner/repo/", wantNamespace: "owner", wantName: "repo"},
		// GitLab groups nest, and a truncated path points at a different
		// project that may well exist — so every leading segment is kept.
		{name: "nested gitlab group", path: "group/subgroup/project", wantNamespace: "group/subgroup", wantName: "project"},
		{name: "single segment", path: "repo", wantErr: true},
		{name: "empty", path: "", wantErr: true},
		{name: "missing name", path: "owner/", wantErr: true},
		{name: "missing namespace", path: "/repo", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ref, err := ParseRepoRef(tt.path)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got ref %+v", ref)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ref.Namespace != tt.wantNamespace || ref.Name != tt.wantName {
				t.Errorf("ref = %+v, want %s/%s", ref, tt.wantNamespace, tt.wantName)
			}
			if ref.FullPath() != tt.wantNamespace+"/"+tt.wantName {
				t.Errorf("FullPath() = %q, want %q", ref.FullPath(), tt.wantNamespace+"/"+tt.wantName)
			}
		})
	}
}

func TestRepoTree_BlobPaths_DropsDirectories(t *testing.T) {
	tree := &RepoTree{Entries: []TreeEntry{
		{Path: "cmd", Type: TreeEntryTree},
		{Path: "cmd/main.go", Type: TreeEntryBlob},
		{Path: ".github/workflows/ci.yml", Type: TreeEntryBlob},
	}}

	got := tree.BlobPaths()
	if len(got) != 2 || got[0] != "cmd/main.go" || got[1] != ".github/workflows/ci.yml" {
		t.Fatalf("BlobPaths() = %v, want only the two files", got)
	}
}

// ── resolver ─────────────────────────────────────────────────────────────────

func TestFor_PicksProviderByRepositoryType(t *testing.T) {
	creds := Credentials{GitHubToken: "gh", GitLabToken: "gl"}

	if _, err := For(models.RepositoryTypeGitHub, creds); err != nil {
		t.Errorf("github: unexpected error: %v", err)
	}
	if _, err := For(models.RepositoryTypeGitea, creds); !errors.Is(err, ErrUnsupportedProvider) {
		t.Errorf("gitea: err = %v, want ErrUnsupportedProvider", err)
	}
	if _, err := For("", creds); !errors.Is(err, ErrUnsupportedProvider) {
		t.Errorf("empty type: err = %v, want ErrUnsupportedProvider", err)
	}
}

func TestFor_RequiresTheProvidersOwnToken(t *testing.T) {
	// A GitLab token is no use to GitHub: resolving must fail rather than
	// build a client that will authenticate against the wrong host.
	_, err := For(models.RepositoryTypeGitHub, Credentials{GitLabToken: "gl"})
	if !errors.Is(err, ErrMissingCredentials) {
		t.Fatalf("err = %v, want ErrMissingCredentials", err)
	}
}

func TestCredentialsFromConfig(t *testing.T) {
	fallback := Credentials{GitHubToken: "platform-gh"}

	if got := CredentialsFromConfig(nil, fallback); got != fallback {
		t.Errorf("nil config: got %+v, want the fallback %+v", got, fallback)
	}

	cfg := &models.OrganizationConfig{GithubToken: "org-gh"}
	if got := CredentialsFromConfig(cfg, fallback); got.GitHubToken != "org-gh" {
		t.Errorf("GitHubToken = %q, want the organization's own token", got.GitHubToken)
	}

	// An organization that configured nothing still inherits the platform token.
	if got := CredentialsFromConfig(&models.OrganizationConfig{}, fallback); got.GitHubToken != "platform-gh" {
		t.Errorf("GitHubToken = %q, want the platform fallback", got.GitHubToken)
	}
}

// ── github adapter ───────────────────────────────────────────────────────────

type fakeGitHubClient struct {
	github.ClientInterface
	err  error
	prs  []github.PullRequest
	tree *github.RepoTree
}

func (f *fakeGitHubClient) GetRepository(context.Context, string, string) (*github.RepoInfo, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &github.RepoInfo{ID: 7, FullName: "owner/repo", DefaultBranch: "main", StargazersCount: 3, OpenIssuesCount: 1}, nil
}

func (f *fakeGitHubClient) ListPullRequests(context.Context, string, string) ([]github.PullRequest, error) {
	return f.prs, f.err
}

func (f *fakeGitHubClient) GetRepositoryTree(context.Context, string, string, string) (*github.RepoTree, error) {
	return f.tree, f.err
}

func (f *fakeGitHubClient) GetCommits(context.Context, string, string, string, int) ([]github.Commit, error) {
	if f.err != nil {
		return nil, f.err
	}
	commit := github.Commit{SHA: "abc123"}
	commit.Commit.Message = "fix: thing"
	commit.Commit.Author.Name = "Dev"
	return []github.Commit{commit}, nil
}

func TestGitHubProvider_TranslatesSentinelErrors(t *testing.T) {
	tests := []struct {
		name string
		from error
		want error
	}{
		{name: "not found", from: github.ErrNotFound, want: ErrNotFound},
		{name: "unauthorized", from: github.ErrUnauthorized, want: ErrUnauthorized},
		{name: "rate limited", from: github.ErrRateLimited, want: ErrRateLimited},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewGitHubProviderWithClient(&fakeGitHubClient{err: tt.from}, "token")
			_, err := p.GetRepository(context.Background(), RepoRef{Namespace: "owner", Name: "repo"})
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestGitHubProvider_PreservesUnrecognizedErrors(t *testing.T) {
	sentinel := errors.New("github API error 500: boom")
	p := NewGitHubProviderWithClient(&fakeGitHubClient{err: sentinel}, "token")

	_, err := p.GetRepository(context.Background(), RepoRef{Namespace: "owner", Name: "repo"})
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want the original error to survive translation", err)
	}
}

func TestGitHubProvider_MapsCatalogFields(t *testing.T) {
	p := NewGitHubProviderWithClient(&fakeGitHubClient{}, "token")
	ref := RepoRef{Namespace: "owner", Name: "repo"}

	info, err := p.GetRepository(context.Background(), ref)
	if err != nil {
		t.Fatalf("GetRepository: %v", err)
	}
	if info.ID != 7 || info.StarCount != 3 || info.OpenIssueCount != 1 || info.DefaultBranch != "main" {
		t.Errorf("info = %+v, want the GitHub counts mapped onto neutral names", info)
	}

	commits, err := p.ListCommits(context.Background(), ref, "main", 10)
	if err != nil {
		t.Fatalf("ListCommits: %v", err)
	}
	// GitHub nests the message under commit.commit.message; the neutral type
	// is flat, and that flattening is the whole point of the adapter.
	if len(commits) != 1 || commits[0].Message != "fix: thing" || commits[0].AuthorName != "Dev" {
		t.Errorf("commits = %+v, want the nested commit info flattened", commits)
	}
}

func TestGitHubProvider_MapsChangeRequestRefs(t *testing.T) {
	pr := github.PullRequest{
		Number:  42,
		Title:   "fix",
		State:   "open",
		HTMLURL: "https://github.com/owner/repo/pull/42",
		User:    github.User{Login: "dev"},
		Head:    github.Branch{Ref: "feature", SHA: "head-sha"},
		Base:    github.Branch{Ref: "main", SHA: "base-sha"},
	}
	p := NewGitHubProviderWithClient(&fakeGitHubClient{prs: []github.PullRequest{pr}}, "token")

	got, err := p.ListChangeRequests(context.Background(), RepoRef{Namespace: "owner", Name: "repo"})
	if err != nil {
		t.Fatalf("ListChangeRequests: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	cr := got[0]
	if cr.HeadRef != "feature" || cr.HeadSHA != "head-sha" || cr.BaseRef != "main" || cr.AuthorLogin != "dev" {
		t.Errorf("change request = %+v, want head/base refs and author flattened", cr)
	}
	if cr.WebURL != pr.HTMLURL {
		t.Errorf("WebURL = %q, want %q", cr.WebURL, pr.HTMLURL)
	}
}

func TestGitHubProvider_PreservesTreeTruncation(t *testing.T) {
	p := NewGitHubProviderWithClient(&fakeGitHubClient{tree: &github.RepoTree{
		Truncated: true,
		Tree:      []github.TreeEntry{{Path: "main.go", Type: "blob"}},
	}}, "token")

	tree, err := p.GetTree(context.Background(), RepoRef{Namespace: "owner", Name: "repo"}, "main")
	if err != nil {
		t.Fatalf("GetTree: %v", err)
	}
	// Losing this flag would turn "we could not see the whole repository" into
	// a confident "this file does not exist" downstream in detection.
	if !tree.Truncated {
		t.Fatal("Truncated was dropped in translation")
	}
}

func TestGitHubProvider_CloneAuthUsesTokenAsPassword(t *testing.T) {
	user, password := NewGitHubProviderWithClient(&fakeGitHubClient{}, "ghp_token").CloneAuth()
	if user != "x-access-token" || password != "ghp_token" {
		t.Fatalf("CloneAuth() = %q/%q, want x-access-token/ghp_token", user, password)
	}
}

func TestFor_HonoursACustomGitLabBaseURL(t *testing.T) {
	// Self-hosted GitLab, and the hook the E2E harness uses to point the client
	// at a fake instance. Without this the client is pinned to gitlab.com and
	// every provider test needs the real network.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/projects/group%2Fproject" {
			t.Errorf("path = %q, want the project request to reach this host", r.URL.EscapedPath())
		}
		json.NewEncoder(w).Encode(map[string]any{"id": 1, "default_branch": "main"})
	}))
	defer srv.Close()

	provider, err := For(models.RepositoryTypeGitLab, Credentials{GitLabToken: "glpat", GitLabBaseURL: srv.URL})
	if err != nil {
		t.Fatalf("For: %v", err)
	}
	info, err := provider.GetRepository(context.Background(), RepoRef{Namespace: "group", Name: "project"})
	if err != nil {
		t.Fatalf("GetRepository: %v", err)
	}
	if info.ID != 1 {
		t.Fatalf("info = %+v, want the fake instance's project", info)
	}
}

func TestCredentialsFromConfig_PrefersTheOrganizationsGitLabHost(t *testing.T) {
	fallback := Credentials{GitLabToken: "platform", GitLabBaseURL: "https://gitlab.platform.example/api/v4"}

	cfg := &models.OrganizationConfig{GitlabToken: "org", GitlabBaseURL: "https://gitlab.acme.example/api/v4"}
	got := CredentialsFromConfig(cfg, fallback)
	if got.GitLabToken != "org" || got.GitLabBaseURL != "https://gitlab.acme.example/api/v4" {
		t.Fatalf("creds = %+v, want the organization's own host and token", got)
	}

	// An organization that set only a token still talks to the deployment's host.
	got = CredentialsFromConfig(&models.OrganizationConfig{GitlabToken: "org"}, fallback)
	if got.GitLabBaseURL != "https://gitlab.platform.example/api/v4" {
		t.Fatalf("GitLabBaseURL = %q, want the platform fallback", got.GitLabBaseURL)
	}
}
