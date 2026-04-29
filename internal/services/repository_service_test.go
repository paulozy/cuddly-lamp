package services

import (
	"context"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
	rediscache "github.com/paulozy/idp-with-ai-backend/internal/storage/redis"
)

// ── mocks ──────────────────────────────────────────────────────────────────

type mockRepoStore struct {
	storage.Repository // embed for unimplemented methods

	repos     map[string]*models.Repository
	createErr error
	updateErr error
	deleteErr error
}

func newMockRepoStore() *mockRepoStore {
	return &mockRepoStore{repos: make(map[string]*models.Repository)}
}

func (m *mockRepoStore) GetRepository(_ context.Context, id string) (*models.Repository, error) {
	r, ok := m.repos[id]
	if !ok {
		return nil, nil
	}
	return r, nil
}

func (m *mockRepoStore) GetRepositoryByURL(_ context.Context, organizationID, url string) (*models.Repository, error) {
	for _, r := range m.repos {
		if r.URL == url && (organizationID == "" || r.OrganizationID == organizationID) {
			return r, nil
		}
	}
	return nil, nil
}

func (m *mockRepoStore) CreateRepository(_ context.Context, repo *models.Repository) error {
	if m.createErr != nil {
		return m.createErr
	}
	repo.ID = "repo-1"
	m.repos[repo.ID] = repo
	return nil
}

func (m *mockRepoStore) UpdateRepository(_ context.Context, repo *models.Repository) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	m.repos[repo.ID] = repo
	return nil
}

func (m *mockRepoStore) DeleteRepository(_ context.Context, id string) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	delete(m.repos, id)
	return nil
}

func (m *mockRepoStore) ListRepositories(_ context.Context, filter *storage.RepositoryFilter) ([]models.Repository, int64, error) {
	var result []models.Repository
	for _, r := range m.repos {
		if filter.OrganizationID == "" || r.OrganizationID == filter.OrganizationID {
			result = append(result, *r)
		}
	}
	return result, int64(len(result)), nil
}

type mockCache struct {
	store map[string]string
}

func newMockCache() *mockCache {
	return &mockCache{store: make(map[string]string)}
}

func (c *mockCache) Get(_ context.Context, key string) (string, error) {
	v, ok := c.store[key]
	if !ok {
		return "", rediscache.ErrCacheMiss
	}
	return v, nil
}

func (c *mockCache) Set(_ context.Context, key, value string, _ time.Duration) error {
	c.store[key] = value
	return nil
}

func (c *mockCache) Del(_ context.Context, keys ...string) error {
	for _, k := range keys {
		delete(c.store, k)
	}
	return nil
}

func (c *mockCache) Exists(_ context.Context, key string) (bool, error) {
	_, ok := c.store[key]
	return ok, nil
}

// ── mock enqueuer ──────────────────────────────────────────────────────────

type mockEnqueuer struct{}

func (m *mockEnqueuer) Enqueue(_ context.Context, _ string, _ any, _ ...asynq.Option) error {
	return nil
}

func (m *mockEnqueuer) EnqueueIn(_ context.Context, _ string, _ any, _ time.Duration, _ ...asynq.Option) error {
	return nil
}

// ── helpers ────────────────────────────────────────────────────────────────

func newRepoService(store *mockRepoStore, cache rediscache.Cache) *RepositoryService {
	return NewRepositoryService(store, cache, &mockEnqueuer{})
}

const (
	orgID      = "org-1"
	otherOrgID = "org-2"
	ownerID    = "user-owner"
	otherID    = "user-other"
	ghURL      = "https://github.com/owner/repo"
	glURL      = "https://gitlab.com/owner/repo"
)

// ── CreateRepository ───────────────────────────────────────────────────────

func TestRepositoryService_Create_Success(t *testing.T) {
	svc := newRepoService(newMockRepoStore(), newMockCache())

	resp, err := svc.CreateRepository(context.Background(), orgID, ownerID, models.CreateRepositoryRequest{URL: ghURL})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.URL != ghURL {
		t.Errorf("URL = %q, want %q", resp.URL, ghURL)
	}
	if resp.Name != "owner/repo" {
		t.Errorf("Name = %q, want %q", resp.Name, "owner/repo")
	}
	if resp.Type != models.RepositoryTypeGitHub {
		t.Errorf("Type = %q, want GitHub", resp.Type)
	}
}

func TestRepositoryService_Create_DuplicateURL(t *testing.T) {
	store := newMockRepoStore()
	svc := newRepoService(store, newMockCache())

	_, _ = svc.CreateRepository(context.Background(), orgID, ownerID, models.CreateRepositoryRequest{URL: ghURL})
	_, err := svc.CreateRepository(context.Background(), orgID, ownerID, models.CreateRepositoryRequest{URL: ghURL})
	if err == nil {
		t.Fatal("expected ErrRepositoryAlreadyExists, got nil")
	}
	if err != ErrRepositoryAlreadyExists {
		t.Errorf("error = %v, want ErrRepositoryAlreadyExists", err)
	}
}

func TestRepositoryService_Create_InvalidURL(t *testing.T) {
	svc := newRepoService(newMockRepoStore(), newMockCache())

	_, err := svc.CreateRepository(context.Background(), orgID, ownerID, models.CreateRepositoryRequest{URL: "https://bitbucket.org/owner/repo"})
	if err == nil {
		t.Fatal("expected error for unsupported host, got nil")
	}
}

// ── GetRepository ──────────────────────────────────────────────────────────

func TestRepositoryService_Get_Success(t *testing.T) {
	store := newMockRepoStore()
	svc := newRepoService(store, newMockCache())

	created, _ := svc.CreateRepository(context.Background(), orgID, ownerID, models.CreateRepositoryRequest{URL: ghURL})

	got, err := svc.GetRepository(context.Background(), created.ID, orgID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("ID = %q, want %q", got.ID, created.ID)
	}
}

func TestRepositoryService_Get_NotFound(t *testing.T) {
	svc := newRepoService(newMockRepoStore(), newMockCache())

	_, err := svc.GetRepository(context.Background(), "nonexistent", orgID)
	if err != ErrRepositoryNotFound {
		t.Errorf("error = %v, want ErrRepositoryNotFound", err)
	}
}

func TestRepositoryService_Get_Forbidden(t *testing.T) {
	store := newMockRepoStore()
	svc := newRepoService(store, newMockCache())

	created, _ := svc.CreateRepository(context.Background(), orgID, ownerID, models.CreateRepositoryRequest{URL: ghURL})

	_, err := svc.GetRepository(context.Background(), created.ID, otherOrgID)
	if err != ErrForbidden {
		t.Errorf("error = %v, want ErrForbidden", err)
	}
}

// ── ListRepositories ───────────────────────────────────────────────────────

func TestRepositoryService_List_OnlyOwnerRepos(t *testing.T) {
	store := newMockRepoStore()
	svc := newRepoService(store, newMockCache())

	_, _ = svc.CreateRepository(context.Background(), orgID, ownerID, models.CreateRepositoryRequest{URL: ghURL})

	// Seed a repo for another user directly so the URL doesn't conflict
	store.repos["other-1"] = &models.Repository{ID: "other-1", URL: glURL, OrganizationID: otherOrgID, OwnerUserID: otherID, Name: "owner/repo2", Type: models.RepositoryTypeGitLab}

	result, err := svc.ListRepositories(context.Background(), orgID, 10, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Total != 1 {
		t.Errorf("Total = %d, want 1", result.Total)
	}
	if result.Items[0].OrganizationID != orgID {
		t.Errorf("wrong organization in result")
	}
}

// ── UpdateRepository ───────────────────────────────────────────────────────

func TestRepositoryService_Update_Success(t *testing.T) {
	store := newMockRepoStore()
	svc := newRepoService(store, newMockCache())

	created, _ := svc.CreateRepository(context.Background(), orgID, ownerID, models.CreateRepositoryRequest{URL: ghURL})

	desc := "new description"
	updated, err := svc.UpdateRepository(context.Background(), created.ID, orgID, models.UpdateRepositoryRequest{Description: &desc})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Description != desc {
		t.Errorf("Description = %q, want %q", updated.Description, desc)
	}
}

func TestRepositoryService_Update_Forbidden(t *testing.T) {
	store := newMockRepoStore()
	svc := newRepoService(store, newMockCache())

	created, _ := svc.CreateRepository(context.Background(), orgID, ownerID, models.CreateRepositoryRequest{URL: ghURL})

	desc := "desc"
	_, err := svc.UpdateRepository(context.Background(), created.ID, otherOrgID, models.UpdateRepositoryRequest{Description: &desc})
	if err != ErrForbidden {
		t.Errorf("error = %v, want ErrForbidden", err)
	}
}

// ── DeleteRepository ───────────────────────────────────────────────────────

func TestRepositoryService_Delete_Success(t *testing.T) {
	store := newMockRepoStore()
	svc := newRepoService(store, newMockCache())

	created, _ := svc.CreateRepository(context.Background(), orgID, ownerID, models.CreateRepositoryRequest{URL: ghURL})

	if err := svc.DeleteRepository(context.Background(), created.ID, orgID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err := svc.GetRepository(context.Background(), created.ID, orgID)
	if err != ErrRepositoryNotFound {
		t.Errorf("expected ErrRepositoryNotFound after delete, got %v", err)
	}
}

func TestRepositoryService_Delete_Forbidden(t *testing.T) {
	store := newMockRepoStore()
	svc := newRepoService(store, newMockCache())

	created, _ := svc.CreateRepository(context.Background(), orgID, ownerID, models.CreateRepositoryRequest{URL: ghURL})

	if err := svc.DeleteRepository(context.Background(), created.ID, otherOrgID); err != ErrForbidden {
		t.Errorf("error = %v, want ErrForbidden", err)
	}
}
