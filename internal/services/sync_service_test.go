package services

import (
	"context"
	"errors"
	"testing"

	githubclient "github.com/paulozy/idp-with-ai-backend/internal/integrations/github"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
)

// ── mock GitHub client ────────────────────────────────────────────────────────

type mockGitHubClient struct {
	repoInfo *githubclient.RepoInfo
	repoErr  error
	branches []githubclient.Branch
	commits  []githubclient.Commit
	prs      []githubclient.PullRequest
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

func (m *mockGitHubClient) CreatePullRequestReview(_ context.Context, _, _ string, _ int64, _, _ string, _ []githubclient.ReviewCommentInput) (int64, error) {
	return 0, nil
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

func (m *mockSyncRepoStore) GetWebhookConfigByRepoID(_ context.Context, _ string) (*models.WebhookConfig, error) {
	return nil, nil
}

func (m *mockSyncRepoStore) CreateWebhookConfig(_ context.Context, _ *models.WebhookConfig) error {
	return nil
}

func (m *mockSyncRepoStore) UpdateWebhookConfig(_ context.Context, _ *models.WebhookConfig) error {
	return nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

func newSyncService(store *mockSyncRepoStore, gh githubclient.ClientInterface) *SyncService {
	return NewSyncService(store, gh, newMockCache(), "")
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
