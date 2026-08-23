package services

import (
	"context"
	"errors"
	"testing"

	"github.com/paulozy/idp-with-ai-backend/internal/derive"
	githubclient "github.com/paulozy/idp-with-ai-backend/internal/integrations/github"
	"github.com/paulozy/idp-with-ai-backend/internal/integrations/scm"
	"github.com/paulozy/idp-with-ai-backend/internal/jobs/tasks"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
	rediscache "github.com/paulozy/idp-with-ai-backend/internal/storage/redis"
)

// ── mock GitHub client ────────────────────────────────────────────────────────

type mockGitHubClient struct {
	repoInfo     *githubclient.RepoInfo
	repoErr      error
	branches     []githubclient.Branch
	commits      []githubclient.Commit
	prs          []githubclient.PullRequest
	tree         *githubclient.RepoTree
	treeErr      error
	languages    map[string]int
	languagesErr error
	// files is the content GetFileContent serves, keyed by path; an absent path
	// is ErrNotFound, which is what a real host answers.
	files map[string]string
	// fileRequests records every path asked for, in order.
	fileRequests []string
}

func (m *mockGitHubClient) GetRepository(_ context.Context, _, _ string) (*githubclient.RepoInfo, error) {
	return m.repoInfo, m.repoErr
}

func (m *mockGitHubClient) GetBranches(_ context.Context, _, _ string) ([]githubclient.Branch, error) {
	return m.branches, nil
}

func (m *mockGitHubClient) GetCommits(_ context.Context, _, _, _ string, _ int) ([]githubclient.Commit, error) {
	return m.commits, nil
}

func (m *mockGitHubClient) ListPullRequests(_ context.Context, _, _ string) ([]githubclient.PullRequest, error) {
	return m.prs, nil
}

// Issues and contributors are not part of sync — the catalog stores counts,
// not lists — so these satisfy the interface and nothing more.
func (m *mockGitHubClient) ListIssues(_ context.Context, _, _ string) ([]githubclient.Issue, error) {
	return nil, nil
}

func (m *mockGitHubClient) ListContributors(_ context.Context, _, _ string) ([]githubclient.Contributor, error) {
	return nil, nil
}

func (m *mockGitHubClient) CloseIssue(_ context.Context, _, _ string, _ int64) error { return nil }

func (m *mockGitHubClient) SubmitReview(_ context.Context, _, _ string, _ int64, _, _ string) error {
	return nil
}

func (m *mockGitHubClient) GetRepositoryTree(_ context.Context, _, _, _ string) (*githubclient.RepoTree, error) {
	return m.tree, m.treeErr
}

// Sync itself reads no file contents; the architecture extractor does, and it
// records what it was asked for so a test can prove the shortlist is short.
func (m *mockGitHubClient) GetFileContent(_ context.Context, _, _, _, path string, _ int64) ([]byte, error) {
	m.fileRequests = append(m.fileRequests, path)
	if m.files == nil {
		return nil, githubclient.ErrNotFound
	}
	content, ok := m.files[path]
	if !ok {
		return nil, githubclient.ErrNotFound
	}
	return []byte(content), nil
}

func (m *mockGitHubClient) GetLanguages(_ context.Context, _, _ string) (map[string]int, error) {
	return m.languages, m.languagesErr
}

func (m *mockGitHubClient) CreateWebhook(_ context.Context, _, _, _, _ string) (int64, error) {
	return 0, nil
}

func (m *mockGitHubClient) DeleteWebhook(_ context.Context, _, _ string, _ int64) error {
	return nil
}

func (m *mockGitHubClient) GetPullRequest(_ context.Context, _, _ string, _ int64) (*githubclient.PullRequest, error) {
	return nil, nil
}

func (m *mockGitHubClient) GetPullRequestFiles(_ context.Context, _, _ string, _ int64) ([]githubclient.PRFile, error) {
	return nil, nil
}

func (m *mockGitHubClient) CreateBranch(_ context.Context, _, _, _, _ string) error {
	return nil
}

func (m *mockGitHubClient) CreateOrUpdateFile(_ context.Context, _, _, _, _, _, _ string) error {
	return nil
}

func (m *mockGitHubClient) CreatePullRequest(_ context.Context, _, _, _, _, _, _ string) (*githubclient.PullRequest, error) {
	return nil, nil
}

// Sync reads neither the token's identity nor review state; both methods are
// here only to satisfy the interface.
func (m *mockGitHubClient) GetAuthenticatedUser(_ context.Context) (*githubclient.AuthenticatedUser, error) {
	return nil, nil
}

func (m *mockGitHubClient) ListReviews(_ context.Context, _, _ string, _ int64) ([]githubclient.Review, error) {
	return nil, nil
}

// stubProvider is an scm.Provider that records the ref it was asked about.
// Used for hosts whose real client would otherwise need the network.
type stubProvider struct {
	info     *scm.RepoInfo
	branches []scm.Branch
	ref      scm.RepoRef
}

// Sync never needs the token's identity or review state; these satisfy the
// interface.
func (s *stubProvider) CurrentUser(_ context.Context) (*scm.Identity, error) {
	return nil, nil
}

func (s *stubProvider) GetChangeRequestReviews(_ context.Context, _ scm.RepoRef, _ int64) (*scm.ReviewState, error) {
	return nil, nil
}

func (s *stubProvider) GetFileContent(_ context.Context, _ scm.RepoRef, _, _ string) ([]byte, error) {
	return nil, scm.ErrNotFound
}

func (s *stubProvider) GetRepository(_ context.Context, ref scm.RepoRef) (*scm.RepoInfo, error) {
	s.ref = ref
	return s.info, nil
}

func (s *stubProvider) ListBranches(_ context.Context, _ scm.RepoRef) ([]scm.Branch, error) {
	return s.branches, nil
}

func (s *stubProvider) ListCommits(_ context.Context, _ scm.RepoRef, _ string, _ int) ([]scm.Commit, error) {
	return nil, nil
}

func (s *stubProvider) ListLanguages(_ context.Context, _ scm.RepoRef) (map[string]int, error) {
	return nil, nil
}

func (s *stubProvider) GetTree(_ context.Context, _ scm.RepoRef, _ string) (*scm.RepoTree, error) {
	return nil, nil
}

func (s *stubProvider) ListChangeRequests(_ context.Context, _ scm.RepoRef) ([]scm.ChangeRequest, error) {
	return nil, nil
}

func (s *stubProvider) ListIssues(_ context.Context, _ scm.RepoRef) ([]scm.Issue, error) {
	return nil, nil
}

func (s *stubProvider) ListContributors(_ context.Context, _ scm.RepoRef) ([]scm.Contributor, error) {
	return nil, nil
}

func (s *stubProvider) CloseIssue(_ context.Context, _ scm.RepoRef, _ int64) error { return nil }

func (s *stubProvider) ApproveChangeRequest(_ context.Context, _ scm.RepoRef, _ int64, _ string) error {
	return nil
}

func (s *stubProvider) RequestChanges(_ context.Context, _ scm.RepoRef, _ int64, _ string) error {
	return nil
}

func (s *stubProvider) GetChangeRequest(_ context.Context, _ scm.RepoRef, _ int64) (*scm.ChangeRequest, error) {
	return nil, nil
}

func (s *stubProvider) GetChangeRequestFiles(_ context.Context, _ scm.RepoRef, _ int64) ([]scm.ChangeRequestFile, error) {
	return nil, nil
}

func (s *stubProvider) CreateBranch(_ context.Context, _ scm.RepoRef, _, _ string) error { return nil }

func (s *stubProvider) UpsertFile(_ context.Context, _ scm.RepoRef, _, _, _, _ string) error {
	return nil
}

func (s *stubProvider) OpenChangeRequest(_ context.Context, _ scm.RepoRef, _, _, _, _ string) (*scm.ChangeRequest, error) {
	return nil, nil
}

func (s *stubProvider) RegisterWebhook(_ context.Context, _ scm.RepoRef, _, _ string) (*scm.Webhook, error) {
	return nil, nil
}

func (s *stubProvider) CloneAuth() (string, string) { return "stub", "stub" }

// ── extended mock repo store for SyncService ─────────────────────────────────

type mockSyncRepoStore struct {
	storage.Repository
	repos     map[string]*models.Repository
	updateErr error
}

func newMockSyncRepoStore() *mockSyncRepoStore {
	return &mockSyncRepoStore{repos: make(map[string]*models.Repository)}
}

func (m *mockSyncRepoStore) GetRepository(_ context.Context, id string) (*models.Repository, error) {
	r, ok := m.repos[id]
	if !ok {
		return nil, nil
	}
	return r, nil
}

func (m *mockSyncRepoStore) UpdateRepository(_ context.Context, repo *models.Repository) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	m.repos[repo.ID] = repo
	return nil
}

// An organization with no config row falls back to the platform token, which is
// what a fresh tenant looks like. Sync only asks when the repository has an
// organization, so tests that set one need this.
func (m *mockSyncRepoStore) GetOrganizationConfig(_ context.Context, _ string) (*models.OrganizationConfig, error) {
	return nil, nil
}

func (m *mockSyncRepoStore) GetWebhookConfigByRepoID(_ context.Context, _ string) (*models.WebhookConfig, error) {
	return nil, nil
}

func (m *mockSyncRepoStore) CreateWebhookConfig(_ context.Context, _ *models.WebhookConfig) error {
	return nil
}

func (m *mockSyncRepoStore) UpdateWebhookConfig(_ context.Context, _ *models.WebhookConfig) error {
	return nil
}

// spyGitHubClient records whether the GitHub API was reached at all. Used to
// prove a non-GitHub repository never gets queried against api.github.com.
type spyGitHubClient struct {
	mockGitHubClient
	called bool
}

func (m *spyGitHubClient) GetRepository(ctx context.Context, owner, name string) (*githubclient.RepoInfo, error) {
	m.called = true
	return m.mockGitHubClient.GetRepository(ctx, owner, name)
}

// ── helpers ───────────────────────────────────────────────────────────────────

func newSyncService(store *mockSyncRepoStore, gh githubclient.ClientInterface) *SyncService {
	svc := NewSyncService(store, scm.Credentials{GitHubToken: "test-token"}, newMockCache(), "")
	// Resolve through the real GitHub adapter wrapped around a mock HTTP
	// client. The provider dispatch stays real — a non-GitHub URL is still
	// refused by scm.For — so only the network is replaced here.
	svc.resolve = func(kind models.RepositoryType, creds scm.Credentials) (scm.Provider, error) {
		if kind != models.RepositoryTypeGitHub {
			return scm.For(kind, creds)
		}
		return scm.NewGitHubProviderWithClient(gh, creds.GitHubToken), nil
	}
	return svc
}

func seedRepo(store *mockSyncRepoStore, id, url string) {
	store.repos[id] = &models.Repository{
		ID:         id,
		URL:        url,
		SyncStatus: "idle",
	}
}

// ── tests ─────────────────────────────────────────────────────────────────────

func TestSyncService_SyncRepository_HappyPath(t *testing.T) {
	store := newMockSyncRepoStore()
	seedRepo(store, "repo-1", "https://github.com/owner/repo")

	gh := &mockGitHubClient{
		repoInfo: &githubclient.RepoInfo{
			ID:              42,
			Language:        "Go",
			DefaultBranch:   "main",
			StargazersCount: 5,
			ForksCount:      2,
			OpenIssuesCount: 3,
		},
		branches: []githubclient.Branch{{Name: "main"}, {Name: "dev"}},
		commits:  []githubclient.Commit{{SHA: "abc"}},
		prs:      []githubclient.PullRequest{{Number: 1}},
	}

	svc := newSyncService(store, gh)
	if err := svc.SyncRepository(context.Background(), "repo-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated := store.repos["repo-1"]
	if updated.SyncStatus != "synced" {
		t.Errorf("SyncStatus = %q, want %q", updated.SyncStatus, "synced")
	}
	if updated.Metadata.BranchCount != 2 {
		t.Errorf("BranchCount = %d, want 2", updated.Metadata.BranchCount)
	}
	if updated.Metadata.CommitCount != 1 {
		t.Errorf("CommitCount = %d, want 1", updated.Metadata.CommitCount)
	}
	if updated.Metadata.PRCount != 1 {
		t.Errorf("PRCount = %d, want 1", updated.Metadata.PRCount)
	}
	if updated.Metadata.Languages["Go"] != 100 {
		t.Errorf("Languages[Go] = %d, want 100", updated.Metadata.Languages["Go"])
	}
}

func TestSyncService_SyncRepository_NotFound(t *testing.T) {
	svc := newSyncService(newMockSyncRepoStore(), &mockGitHubClient{})
	err := svc.SyncRepository(context.Background(), "missing")
	if !errors.Is(err, ErrRepositoryNotFound) {
		t.Errorf("err = %v, want ErrRepositoryNotFound", err)
	}
}

func TestSyncService_SyncRepository_GitHubError(t *testing.T) {
	store := newMockSyncRepoStore()
	seedRepo(store, "repo-1", "https://github.com/owner/repo")

	gh := &mockGitHubClient{repoErr: githubclient.ErrNotFound}
	svc := newSyncService(store, gh)

	err := svc.SyncRepository(context.Background(), "repo-1")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if store.repos["repo-1"].SyncStatus != "error" {
		t.Errorf("SyncStatus = %q, want %q", store.repos["repo-1"].SyncStatus, "error")
	}
}

func TestSyncService_SyncRepository_SyncInProgress(t *testing.T) {
	store := newMockSyncRepoStore()
	store.repos["repo-1"] = &models.Repository{
		ID:         "repo-1",
		URL:        "https://github.com/owner/repo",
		SyncStatus: "syncing",
	}

	svc := newSyncService(store, &mockGitHubClient{})
	err := svc.SyncRepository(context.Background(), "repo-1")
	if !errors.Is(err, ErrSyncInProgress) {
		t.Errorf("err = %v, want ErrSyncInProgress", err)
	}
}

func TestSyncService_SyncRepository_DBUpdateError(t *testing.T) {
	store := newMockSyncRepoStore()
	seedRepo(store, "repo-1", "https://github.com/owner/repo")
	store.updateErr = errors.New("db connection lost")

	gh := &mockGitHubClient{
		repoInfo: &githubclient.RepoInfo{DefaultBranch: "main"},
		branches: []githubclient.Branch{{Name: "main"}},
	}

	svc := newSyncService(store, gh)
	err := svc.SyncRepository(context.Background(), "repo-1")
	// First UpdateRepository (set "syncing") fails, but we warn and continue.
	// doSync's UpdateRepository also fails — that's the returned error.
	if err == nil {
		t.Fatal("expected error due to DB update failure, got nil")
	}
}

// Regression: SyncRepository used to discard the provider returned by
// ParseRepositoryURL and always build a GitHub client. Because the project
// path is not unique across forges, a gitlab.com URL would be queried against
// api.github.com — silently importing an unrelated GitHub project's languages,
// branches and commits into the catalog, and trying to register a webhook on
// someone else's repository.
//
// GitLab now has a client of its own, so the guard's job narrowed to hosts we
// still cannot talk to; the "never queried through the wrong client" half of
// the regression is covered by
// TestSyncService_SyncRepository_UsesTheProviderMatchingTheHost.
func TestSyncService_SyncRepository_RefusesHostsWithoutAClient(t *testing.T) {
	store := newMockSyncRepoStore()
	seedRepo(store, "repo-1", "https://gitea.example.com/owner/repo")

	gh := &spyGitHubClient{mockGitHubClient: mockGitHubClient{
		repoInfo: &githubclient.RepoInfo{ID: 42, Language: "Go", DefaultBranch: "main"},
	}}

	svc := newSyncService(store, gh)
	err := svc.SyncRepository(context.Background(), "repo-1")

	if !errors.Is(err, ErrUnsupportedProvider) {
		t.Fatalf("error = %v, want ErrUnsupportedProvider", err)
	}
	if gh.called {
		t.Error("the GitHub API was queried for a non-GitHub repository")
	}

	updated := store.repos["repo-1"]
	if updated.SyncStatus != "error" {
		t.Errorf("SyncStatus = %q, want %q", updated.SyncStatus, "error")
	}
	if updated.SyncError == "" {
		t.Error("SyncError should explain why the sync was refused")
	}
	// The catalog row must keep its original metadata — importing a
	// same-named GitHub project is exactly the failure being prevented.
	if updated.Metadata.DefaultBranch != "" {
		t.Errorf("metadata was populated from GitHub: %+v", updated.Metadata)
	}
}

// A GitLab URL must be synced through the GitLab client and never through
// GitHub's, whatever tokens happen to be configured.
func TestSyncService_SyncRepository_UsesTheProviderMatchingTheHost(t *testing.T) {
	store := newMockSyncRepoStore()
	seedRepo(store, "repo-1", "https://gitlab.com/group/subgroup/project")

	gh := &spyGitHubClient{mockGitHubClient: mockGitHubClient{
		repoInfo: &githubclient.RepoInfo{ID: 42, Language: "Go", DefaultBranch: "main"},
	}}
	stub := &stubProvider{
		info:     &scm.RepoInfo{ID: 7, DefaultBranch: "trunk", StarCount: 11},
		branches: []scm.Branch{{Name: "trunk"}},
	}

	svc := newSyncService(store, gh)
	var resolvedKind models.RepositoryType
	svc.resolve = func(kind models.RepositoryType, creds scm.Credentials) (scm.Provider, error) {
		resolvedKind = kind
		if kind != models.RepositoryTypeGitLab {
			t.Fatalf("resolved %q for a gitlab.com URL", kind)
		}
		return stub, nil
	}

	if err := svc.SyncRepository(context.Background(), "repo-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolvedKind != models.RepositoryTypeGitLab {
		t.Fatalf("resolved kind = %q, want gitlab", resolvedKind)
	}
	if gh.called {
		t.Error("the GitHub API was queried for a GitLab repository")
	}

	updated := store.repos["repo-1"]
	if updated.SyncStatus != "synced" {
		t.Fatalf("SyncStatus = %q, want synced", updated.SyncStatus)
	}
	if updated.Metadata.DefaultBranch != "trunk" || updated.Metadata.StarCount != 11 {
		t.Errorf("metadata = %+v, want the GitLab values", updated.Metadata)
	}
	// Nested groups must survive: `group/subgroup` is the namespace, and
	// truncating it to `group` would point at a different project.
	if updated.Metadata.OwnerName != "group/subgroup" {
		t.Errorf("OwnerName = %q, want the full nested namespace", updated.Metadata.OwnerName)
	}
	if stub.ref.FullPath() != "group/subgroup/project" {
		t.Errorf("queried %q, want group/subgroup/project", stub.ref.FullPath())
	}
}

// A repository whose host has a client but no token must fail loudly instead of
// falling back to another provider's token.
func TestSyncService_SyncRepository_RequiresTheHostsOwnToken(t *testing.T) {
	store := newMockSyncRepoStore()
	seedRepo(store, "repo-1", "https://gitlab.com/owner/repo")

	svc := NewSyncService(store, scm.Credentials{GitHubToken: "gh-only"}, newMockCache(), "")
	err := svc.SyncRepository(context.Background(), "repo-1")

	if !errors.Is(err, scm.ErrMissingCredentials) {
		t.Fatalf("error = %v, want ErrMissingCredentials", err)
	}
	if updated := store.repos["repo-1"]; updated.SyncStatus != "error" || updated.SyncError == "" {
		t.Errorf("repository = %+v, want the failure recorded", updated)
	}
}

// ── CI / test detection during sync ──────────────────────────────────────────

func syncWithTree(t *testing.T, tree *githubclient.RepoTree, treeErr error, langs map[string]int) *models.Repository {
	t.Helper()
	store := newMockSyncRepoStore()
	seedRepo(store, "repo-1", "https://github.com/owner/repo")
	gh := &mockGitHubClient{
		repoInfo:  &githubclient.RepoInfo{ID: 1, Language: "Go", DefaultBranch: "main"},
		tree:      tree,
		treeErr:   treeErr,
		languages: langs,
	}
	if err := newSyncService(store, gh).SyncRepository(context.Background(), "repo-1"); err != nil {
		t.Fatalf("SyncRepository: %v", err)
	}
	return store.repos["repo-1"]
}

func blobTree(truncated bool, paths ...string) *githubclient.RepoTree {
	tree := &githubclient.RepoTree{Truncated: truncated}
	for _, p := range paths {
		tree.Tree = append(tree.Tree, githubclient.TreeEntry{Path: p, Type: "blob"})
	}
	return tree
}

func TestSync_RecordsDetectedSignalsWithEvidence(t *testing.T) {
	repo := syncWithTree(t,
		blobTree(false, "main.go", ".github/workflows/ci.yml", "internal/svc_test.go"),
		nil, map[string]int{"Go": 1000})

	if repo.Metadata.HasCI == nil || !*repo.Metadata.HasCI {
		t.Error("HasCI should be true")
	}
	if repo.Metadata.CIEvidence != ".github/workflows/ci.yml" {
		t.Errorf("CIEvidence = %q", repo.Metadata.CIEvidence)
	}
	if repo.Metadata.HasTests == nil || !*repo.Metadata.HasTests {
		t.Error("HasTests should be true")
	}
	if repo.Metadata.TestEvidence != "internal/svc_test.go" {
		t.Errorf("TestEvidence = %q", repo.Metadata.TestEvidence)
	}
}

func TestSync_RecordsADefinitiveNo(t *testing.T) {
	repo := syncWithTree(t, blobTree(false, "main.go", "README.md"), nil, map[string]int{"Go": 1000})

	if repo.Metadata.HasCI == nil || *repo.Metadata.HasCI {
		t.Error("HasCI should be a definitive false")
	}
	if repo.Metadata.HasTests == nil || *repo.Metadata.HasTests {
		t.Error("HasTests should be a definitive false")
	}
}

// Detection is best-effort. A repository whose tree we cannot read must end up
// with undetermined signals — and, critically, a *successful* sync: turning the
// sync red would also fail the sync.healthy check as collateral damage.
func TestSync_TreeFailureLeavesSignalsUndeterminedAndSyncHealthy(t *testing.T) {
	repo := syncWithTree(t, nil, errors.New("403 from github"), map[string]int{"Go": 1000})

	if repo.Metadata.HasCI != nil || repo.Metadata.HasTests != nil {
		t.Error("signals should be nil when the tree could not be read")
	}
	if repo.SyncStatus != "synced" {
		t.Errorf("SyncStatus = %q, want synced — detection must not break the sync", repo.SyncStatus)
	}
}

// A truncated tree cannot prove absence, so the signals stay nil rather than
// recording a confident "no".
func TestSync_TruncatedTreeLeavesSignalsUndetermined(t *testing.T) {
	repo := syncWithTree(t, blobTree(true, "src/a.go"), nil, map[string]int{"Go": 1000})

	if repo.Metadata.HasCI != nil || repo.Metadata.HasTests != nil {
		t.Errorf("signals should stay nil on a truncated tree, got ci=%v tests=%v",
			repo.Metadata.HasCI, repo.Metadata.HasTests)
	}
	if repo.SyncStatus != "synced" {
		t.Errorf("SyncStatus = %q, want synced", repo.SyncStatus)
	}
}

// Regression: only the successful sync invalidated the cached repository
// response, which is the one outcome where staleness is harmless. A repository
// read while it was still `idle` kept reporting `idle` for the cache TTL — an
// hour — after its sync failed, so the UI showed "never synced" for what was
// really "sync failed", and the scorecard reported sync.healthy as not
// applicable instead of failing.
func TestSyncService_SyncRepository_DropsTheCachedResponseOnEveryOutcome(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		gh      *mockGitHubClient
		wantErr bool
	}{
		{
			name:    "host with no client",
			url:     "https://gitea.example.com/owner/repo",
			gh:      &mockGitHubClient{},
			wantErr: true,
		},
		{
			name:    "provider call fails",
			url:     "https://github.com/owner/repo",
			gh:      &mockGitHubClient{repoErr: errors.New("github is down")},
			wantErr: true,
		},
		{
			name: "sync succeeds",
			url:  "https://github.com/owner/repo",
			gh: &mockGitHubClient{
				repoInfo: &githubclient.RepoInfo{ID: 42, DefaultBranch: "main"},
				branches: []githubclient.Branch{{Name: "main"}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newMockSyncRepoStore()
			seedRepo(store, "repo-1", tt.url)

			cache := newMockCache()
			key := rediscache.RepoKey("repo-1")
			cache.store[key] = `{"id":"repo-1","sync_status":"idle"}`

			svc := NewSyncService(store, scm.Credentials{GitHubToken: "test-token"}, cache, "")
			svc.resolve = func(kind models.RepositoryType, creds scm.Credentials) (scm.Provider, error) {
				if kind != models.RepositoryTypeGitHub {
					return scm.For(kind, creds)
				}
				return scm.NewGitHubProviderWithClient(tt.gh, creds.GitHubToken), nil
			}

			err := svc.SyncRepository(context.Background(), "repo-1")
			if tt.wantErr && err == nil {
				t.Fatal("expected an error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if _, stillCached := cache.store[key]; stillCached {
				t.Errorf("the stale cached response survived a sync that ended in %q",
					store.repos["repo-1"].SyncStatus)
			}
		})
	}
}

// ── architecture extraction rides along, and never fails the sync ────────────

// failingFactStore is a sync store whose fact writes always fail. It exists to
// prove the one thing that matters about attaching extraction to sync: a
// repository we cannot inspect must still report `synced`. Reporting `error`
// would fail the scorecard's sync.healthy check as collateral damage — the exact
// mistake the CI/test detection path was written to avoid.
type failingFactStore struct {
	*mockSyncRepoStore
	factErr error
}

func (s *failingFactStore) GetRepositoryFact(_ context.Context, _ string, _ models.RepositoryFactKind) (*models.RepositoryFact, error) {
	return nil, s.factErr
}

func (s *failingFactStore) UpsertRepositoryFact(_ context.Context, _ *models.RepositoryFact) error {
	return s.factErr
}

func TestSyncService_ArchitectureExtractionFailureDoesNotFailTheSync(t *testing.T) {
	base := newMockSyncRepoStore()
	seedRepo(base, "repo-1", "https://github.com/owner/repo")
	base.repos["repo-1"].OrganizationID = "org-1"
	store := &failingFactStore{mockSyncRepoStore: base, factErr: errors.New("db down")}

	gh := &mockGitHubClient{
		repoInfo: &githubclient.RepoInfo{ID: 42, DefaultBranch: "main"},
		tree:     blobTree(false, "go.mod"),
	}

	svc := newSyncService(base, gh)
	svc.repo = store
	architecture := NewArchitectureService(store, newFakeEnqueuer())
	architecture.extractors = []Extractor{&stubExtractor{
		kind:    models.FactKindPackages,
		outcome: derive.CompleteOutcome(),
	}}
	svc.WithArchitecture(architecture)

	if err := svc.SyncRepository(context.Background(), "repo-1"); err != nil {
		t.Fatalf("SyncRepository() error = %v, want nil", err)
	}
	if got := base.repos["repo-1"].SyncStatus; got != "synced" {
		t.Errorf("SyncStatus = %q, want %q", got, "synced")
	}
}

// A sync that changed facts queues exactly one reconciliation for the
// organization, so the org-wide pass runs once per batch and not once per repo.
func TestSyncService_ChangedFactsEnqueueOneDerivation(t *testing.T) {
	store := newMockSyncRepoStore()
	seedRepo(store, "repo-1", "https://github.com/owner/repo")
	store.repos["repo-1"].OrganizationID = "org-1"

	gh := &mockGitHubClient{
		repoInfo: &githubclient.RepoInfo{ID: 42, DefaultBranch: "main"},
		tree:     blobTree(false, "go.mod"),
	}

	factStore := newArchitectureStore()
	svc := newSyncService(store, gh)
	architecture := NewArchitectureService(factStore, newFakeEnqueuer())
	architecture.extractors = []Extractor{&stubExtractor{
		kind:    models.FactKindPackages,
		outcome: derive.CompleteOutcome(),
	}}
	enqueuer := newFakeEnqueuer()
	architecture.enqueuer = enqueuer
	svc.WithArchitecture(architecture)

	if err := svc.SyncRepository(context.Background(), "repo-1"); err != nil {
		t.Fatalf("SyncRepository() error = %v, want nil", err)
	}
	if len(enqueuer.enqueued) != 1 || enqueuer.enqueued[0] != tasks.TypeDeriveArchitecture {
		t.Errorf("enqueued = %v, want one %q", enqueuer.enqueued, tasks.TypeDeriveArchitecture)
	}
}
