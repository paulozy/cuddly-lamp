package services

import (
	"context"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/paulozy/idp-with-ai-backend/internal/jobs/tasks"
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

	// orgConfig lets individual tests opt-in to a Voyage-configured org so
	// `maybeEnqueueInitialEmbeddings` can fire. nil → returns nil (no
	// provider configured → no embeddings enqueue).
	orgConfig *models.OrganizationConfig

	// lastFilter records what the service asked storage for, so tests can
	// assert on the filter rather than on the rows a mock chose to return.
	lastFilter *storage.RepositoryFilter
}

func newMockRepoStore() *mockRepoStore {
	return &mockRepoStore{repos: make(map[string]*models.Repository)}
}

func (m *mockRepoStore) GetOrganizationConfig(_ context.Context, _ string) (*models.OrganizationConfig, error) {
	return m.orgConfig, nil
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
	m.lastFilter = filter
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

type enqueuedTask struct {
	taskType string
	payload  any
}

type mockEnqueuer struct {
	tasks []enqueuedTask
	// unavailable models the no-op enqueuer, which is the case that used to be
	// invisible to callers: it returns nil so a deployment without Redis still
	// serves HTTP, and that made a dropped job look exactly like an accepted one.
	unavailable bool
}

func (m *mockEnqueuer) Available() bool { return !m.unavailable }

func (m *mockEnqueuer) Enqueue(_ context.Context, taskType string, payload any, _ ...asynq.Option) error {
	m.tasks = append(m.tasks, enqueuedTask{taskType: taskType, payload: payload})
	return nil
}

func (m *mockEnqueuer) EnqueueIn(_ context.Context, taskType string, payload any, _ time.Duration, _ ...asynq.Option) error {
	m.tasks = append(m.tasks, enqueuedTask{taskType: taskType, payload: payload})
	return nil
}

// ── helpers ────────────────────────────────────────────────────────────────

// maintainerActor is the identity most existing tests assume: a role that may
// write anything in the organization, so ownership never enters the picture.
func maintainerActor() Actor {
	return Actor{UserID: "user-1", Role: models.RoleMaintainer}
}

func newRepoService(store *mockRepoStore, cache rediscache.Cache) *RepositoryService {
	return NewRepositoryService(store, cache, &mockEnqueuer{})
}

func newRepoServiceWithEnqueuer(store *mockRepoStore, cache rediscache.Cache, eq *mockEnqueuer) *RepositoryService {
	return NewRepositoryService(store, cache, eq)
}

func syncTaskCount(eq *mockEnqueuer) int {
	n := 0
	for _, t := range eq.tasks {
		if t.taskType == tasks.TypeSyncRepo {
			n++
		}
	}
	return n
}

func TestRepositoryService_TriggerSync(t *testing.T) {
	const orgID = "org-1"

	t.Run("enqueues a sync when the repo is stale", func(t *testing.T) {
		store := newMockRepoStore()
		store.repos["r1"] = &models.Repository{ID: "r1", OrganizationID: orgID, SyncStatus: "synced", LastSyncedAt: time.Now().Add(-2 * time.Hour)}
		eq := &mockEnqueuer{}
		svc := newRepoServiceWithEnqueuer(store, &mockCache{}, eq)

		outcome, retryAfter, err := svc.TriggerSync(context.Background(), "r1", orgID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if outcome != SyncTriggerQueued || syncTaskCount(eq) != 1 {
			t.Fatalf("outcome = %q count = %d, want queued and one task", outcome, syncTaskCount(eq))
		}
		if retryAfter != 0 {
			t.Errorf("retryAfter = %v, want 0 for a queued sync", retryAfter)
		}
	})

	// Each declined case used to answer the same "skipped", so a person clicking
	// the button got identical silence whether they were 5 seconds early or the
	// worker did not exist. The outcome is what makes them distinguishable.
	t.Run("reports the throttle window with a retry hint", func(t *testing.T) {
		store := newMockRepoStore()
		store.repos["r1"] = &models.Repository{ID: "r1", OrganizationID: orgID, SyncStatus: "synced", LastSyncedAt: time.Now()}
		eq := &mockEnqueuer{}
		svc := newRepoServiceWithEnqueuer(store, &mockCache{}, eq)

		outcome, retryAfter, err := svc.TriggerSync(context.Background(), "r1", orgID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if outcome != SyncTriggerThrottled || syncTaskCount(eq) != 0 {
			t.Fatalf("outcome = %q count = %d, want throttled and no task", outcome, syncTaskCount(eq))
		}
		// The hint is the whole point: "again in 55s" beats a button that looks broken.
		if retryAfter <= 0 || retryAfter > resyncThrottle {
			t.Errorf("retryAfter = %v, want a positive value within the throttle window", retryAfter)
		}
	})

	t.Run("reports a sync that is already running", func(t *testing.T) {
		store := newMockRepoStore()
		store.repos["r1"] = &models.Repository{ID: "r1", OrganizationID: orgID, SyncStatus: "syncing", LastSyncedAt: time.Now().Add(-2 * time.Hour)}
		eq := &mockEnqueuer{}
		svc := newRepoServiceWithEnqueuer(store, &mockCache{}, eq)

		outcome, _, _ := svc.TriggerSync(context.Background(), "r1", orgID)
		if outcome != SyncTriggerAlreadySyncing || syncTaskCount(eq) != 0 {
			t.Fatalf("outcome = %q, want already_syncing with no new task", outcome)
		}
	})

	// The bug this exists for: with no Redis the no-op enqueuer returns nil, so the
	// old code reported "queued" for a job nobody would ever run and sent the
	// caller off to wait forever.
	t.Run("reports an unavailable queue instead of claiming it queued", func(t *testing.T) {
		store := newMockRepoStore()
		store.repos["r1"] = &models.Repository{ID: "r1", OrganizationID: orgID, SyncStatus: "synced", LastSyncedAt: time.Now().Add(-2 * time.Hour)}
		eq := &mockEnqueuer{unavailable: true}
		svc := newRepoServiceWithEnqueuer(store, &mockCache{}, eq)

		outcome, _, err := svc.TriggerSync(context.Background(), "r1", orgID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if outcome != SyncTriggerQueueUnavailable {
			t.Errorf("outcome = %q, want queue_unavailable", outcome)
		}
		if syncTaskCount(eq) != 0 {
			t.Errorf("tasks = %d, want none enqueued against a dead queue", syncTaskCount(eq))
		}
	})

	t.Run("forbids a repo from another organization", func(t *testing.T) {
		store := newMockRepoStore()
		store.repos["r1"] = &models.Repository{ID: "r1", OrganizationID: "other-org", SyncStatus: "synced", LastSyncedAt: time.Now().Add(-2 * time.Hour)}
		eq := &mockEnqueuer{}
		svc := newRepoServiceWithEnqueuer(store, &mockCache{}, eq)

		if _, _, err := svc.TriggerSync(context.Background(), "r1", orgID); err != ErrForbidden {
			t.Fatalf("expected ErrForbidden, got %v", err)
		}
	})
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

// Creating a repository must enqueue a sync and nothing else. Every AI-driven
// follow-up (initial code analysis, embedding indexing) has been removed, so a
// second task showing up here means one of them crept back in.
func TestRepositoryService_Create_EnqueuesOnlySync(t *testing.T) {
	eq := &mockEnqueuer{}
	svc := newRepoServiceWithEnqueuer(newMockRepoStore(), newMockCache(), eq)

	resp, err := svc.CreateRepository(context.Background(), orgID, ownerID, models.CreateRepositoryRequest{URL: ghURL})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(eq.tasks) != 1 {
		t.Fatalf("enqueued tasks = %d, want 1 (sync only); got %+v", len(eq.tasks), eq.tasks)
	}
	if eq.tasks[0].taskType != tasks.TypeSyncRepo {
		t.Errorf("task = %q, want %q", eq.tasks[0].taskType, tasks.TypeSyncRepo)
	}
	syncPayload, ok := eq.tasks[0].payload.(tasks.SyncRepoPayload)
	if !ok {
		t.Fatalf("payload type = %T, want SyncRepoPayload", eq.tasks[0].payload)
	}
	if syncPayload.RepositoryID != resp.ID {
		t.Errorf("sync payload RepositoryID = %q, want %q", syncPayload.RepositoryID, resp.ID)
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
	store.repos["other-1"] = &models.Repository{ID: "other-1", URL: glURL, OrganizationID: otherOrgID, Name: "owner/repo2", Type: models.RepositoryTypeGitLab}

	result, err := svc.ListRepositories(context.Background(), orgID, RepositoryListOptions{Limit: 10})
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
	updated, err := svc.UpdateRepository(context.Background(), created.ID, orgID, maintainerActor(), models.UpdateRepositoryRequest{Description: &desc})
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
	_, err := svc.UpdateRepository(context.Background(), created.ID, otherOrgID, maintainerActor(), models.UpdateRepositoryRequest{Description: &desc})
	if err != ErrForbidden {
		t.Errorf("error = %v, want ErrForbidden", err)
	}
}

// ── DeleteRepository ───────────────────────────────────────────────────────

func TestRepositoryService_Delete_Success(t *testing.T) {
	store := newMockRepoStore()
	svc := newRepoService(store, newMockCache())

	created, _ := svc.CreateRepository(context.Background(), orgID, ownerID, models.CreateRepositoryRequest{URL: ghURL})

	if err := svc.DeleteRepository(context.Background(), created.ID, orgID, maintainerActor()); err != nil {
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

	if err := svc.DeleteRepository(context.Background(), created.ID, otherOrgID, maintainerActor()); err != ErrForbidden {
		t.Errorf("error = %v, want ErrForbidden", err)
	}
}

// The team step of an onboarding flow asks "what does this team answer for",
// and every team page needs the same answer. The storage layer could already
// filter on owner team; nothing exposed it.
func TestRepositoryService_ListRepositories_FiltersByOwnerTeam(t *testing.T) {
	store := newMockRepoStore()
	svc := newRepoService(store, nil)

	if _, err := svc.ListRepositories(context.Background(), "org-1", RepositoryListOptions{
		Limit:       10,
		OwnerTeamID: "team-1",
	}); err != nil {
		t.Fatalf("ListRepositories: %v", err)
	}

	if store.lastFilter == nil {
		t.Fatal("no filter reached the storage layer")
	}
	if len(store.lastFilter.OwnerTeamIDs) != 1 || store.lastFilter.OwnerTeamIDs[0] != "team-1" {
		t.Fatalf("OwnerTeamIDs = %v, want [team-1]", store.lastFilter.OwnerTeamIDs)
	}
}

func TestRepositoryService_ListRepositories_NoTeamMeansNoFilter(t *testing.T) {
	store := newMockRepoStore()
	svc := newRepoService(store, nil)

	if _, err := svc.ListRepositories(context.Background(), "org-1", RepositoryListOptions{Limit: 10}); err != nil {
		t.Fatalf("ListRepositories: %v", err)
	}

	// An empty option must not become a filter for the empty string, which
	// would list nothing at all.
	if store.lastFilter == nil {
		t.Fatal("no filter reached the storage layer")
	}
	if len(store.lastFilter.OwnerTeamIDs) != 0 {
		t.Fatalf("OwnerTeamIDs = %v, want empty", store.lastFilter.OwnerTeamIDs)
	}
}
