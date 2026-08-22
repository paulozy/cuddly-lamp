package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/paulozy/idp-with-ai-backend/internal/jobs"
	"github.com/paulozy/idp-with-ai-backend/internal/jobs/tasks"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
	"github.com/paulozy/idp-with-ai-backend/internal/storage/redis"
	"github.com/paulozy/idp-with-ai-backend/internal/utils"
)

var (
	ErrRepositoryNotFound      = errors.New("repository not found")
	ErrRepositoryAlreadyExists = errors.New("repository already exists")
	ErrForbidden               = errors.New("forbidden")
	// ErrNotRepositoryOwner separates "your role is too low, full stop" from
	// "your role would allow this on a repository your team owns". The message
	// the user sees is actionable only if the two stay distinct.
	ErrNotRepositoryOwner = errors.New("only the owning team or a maintainer can change this repository")
)

// Actor carries who is making the request. Update and Delete need the role and
// user id to evaluate ownership, which organization id alone cannot answer.
type Actor struct {
	UserID string
	Role   models.UserRole
}

const repoCacheTTL = time.Hour

type RepositoryService struct {
	repo     storage.Repository
	cache    redis.Cache
	enqueuer jobs.Enqueuer
}

func NewRepositoryService(repo storage.Repository, cache redis.Cache, enqueuer jobs.Enqueuer) *RepositoryService {
	return &RepositoryService{repo: repo, cache: cache, enqueuer: enqueuer}
}

func (s *RepositoryService) CreateRepository(ctx context.Context, organizationID, userID string, req models.CreateRepositoryRequest) (*models.RepositoryResponse, error) {
	name, repoType, err := utils.ParseRepositoryURL(req.URL)
	if err != nil {
		return nil, fmt.Errorf("invalid repository URL: %w", err)
	}

	existing, err := s.repo.GetRepositoryByURL(ctx, organizationID, req.URL)
	if err != nil {
		return nil, fmt.Errorf("check duplicate URL: %w", err)
	}
	if existing != nil {
		return nil, ErrRepositoryAlreadyExists
	}

	repo := &models.Repository{
		Name:            name,
		Description:     req.Description,
		URL:             req.URL,
		Type:            repoType,
		OrganizationID:  organizationID,
		CreatedByUserID: userID,
		IsPublic:        req.IsPublic,
	}

	if err := s.repo.CreateRepository(ctx, repo); err != nil {
		return nil, fmt.Errorf("create repository: %w", err)
	}

	if s.enqueuer != nil {
		syncPayload := tasks.SyncRepoPayload{RepositoryID: repo.ID}
		if err := s.enqueuer.Enqueue(ctx, tasks.TypeSyncRepo, syncPayload); err != nil {
			utils.Warn("repository: failed to enqueue sync job", "repo_id", repo.ID, "error", err)
		}
	}

	return models.RepositoryToResponse(repo), nil
}

func (s *RepositoryService) GetRepository(ctx context.Context, id, organizationID string) (*models.RepositoryResponse, error) {
	if s.cache != nil {
		if cached, err := s.cache.Get(ctx, redis.RepoKey(id)); err == nil {
			var resp models.RepositoryResponse
			if json.Unmarshal([]byte(cached), &resp) == nil {
				return &resp, nil
			}
		}
	}

	repo, err := s.repo.GetRepository(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get repository: %w", err)
	}
	if repo == nil {
		return nil, ErrRepositoryNotFound
	}
	if repo.OrganizationID != organizationID {
		return nil, ErrForbidden
	}

	resp := models.RepositoryToResponse(repo)

	if s.cache != nil {
		if data, err := json.Marshal(resp); err == nil {
			_ = s.cache.Set(ctx, redis.RepoKey(id), string(data), repoCacheTTL)
		}
	}

	return resp, nil
}

// resyncThrottle keeps "auto re-sync on open" from hammering the provider when
// the repo view is opened/refreshed repeatedly.
const resyncThrottle = 60 * time.Second

// TriggerSync enqueues a metadata re-sync for a repository (refreshing PR/issue
// counts, stars, branches, etc.). It is throttled: skipped when a sync is
// already running or one ran within resyncThrottle. Returns true when a sync
// was actually enqueued.
func (s *RepositoryService) TriggerSync(ctx context.Context, id, organizationID string) (bool, error) {
	repo, err := s.repo.GetRepository(ctx, id)
	if err != nil {
		return false, fmt.Errorf("get repository: %w", err)
	}
	if repo == nil {
		return false, ErrRepositoryNotFound
	}
	if repo.OrganizationID != organizationID {
		return false, ErrForbidden
	}
	if s.enqueuer == nil {
		return false, nil
	}
	if repo.SyncStatus == "syncing" {
		return false, nil
	}
	if !repo.LastSyncedAt.IsZero() && time.Since(repo.LastSyncedAt) < resyncThrottle {
		return false, nil
	}
	if err := s.enqueuer.Enqueue(ctx, tasks.TypeSyncRepo, tasks.SyncRepoPayload{RepositoryID: repo.ID}); err != nil {
		return false, fmt.Errorf("enqueue sync: %w", err)
	}
	return true, nil
}

// RepositoryListOptions are the knobs the list endpoint exposes.
type RepositoryListOptions struct {
	Limit  int
	Offset int
	// OwnerTeamID narrows the list to the repositories one team answers for.
	// The storage layer could already filter on it; nothing exposed it, and
	// "what does this team own" is the question the onboarding's team step —
	// and any team page — has to answer.
	OwnerTeamID string
}

func (s *RepositoryService) ListRepositories(ctx context.Context, organizationID string, opts RepositoryListOptions) (*models.RepositoryListResponse, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 20
	}
	offset := opts.Offset

	filter := &storage.RepositoryFilter{
		OrganizationID: organizationID,
		Limit:          limit,
		Offset:         offset,
	}
	if opts.OwnerTeamID != "" {
		filter.OwnerTeamIDs = []string{opts.OwnerTeamID}
	}

	repos, total, err := s.repo.ListRepositories(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("list repositories: %w", err)
	}

	items := make([]models.RepositoryResponse, len(repos))
	for i := range repos {
		items[i] = *models.RepositoryToResponse(&repos[i])
	}

	return &models.RepositoryListResponse{
		Items:  items,
		Total:  total,
		Limit:  limit,
		Offset: offset,
	}, nil
}

func (s *RepositoryService) UpdateRepository(ctx context.Context, id, organizationID string, actor Actor, req models.UpdateRepositoryRequest) (*models.RepositoryResponse, error) {
	repo, err := s.repo.GetRepository(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get repository: %w", err)
	}
	if repo == nil {
		return nil, ErrRepositoryNotFound
	}
	if repo.OrganizationID != organizationID {
		return nil, ErrForbidden
	}
	if !s.CanWriteRepository(ctx, actor.Role, actor.UserID, repo) {
		return nil, ErrNotRepositoryOwner
	}

	if req.Description != nil {
		repo.Description = *req.Description
	}
	if req.IsPublic != nil {
		repo.IsPublic = *req.IsPublic
	}

	if err := s.repo.UpdateRepository(ctx, repo); err != nil {
		return nil, fmt.Errorf("update repository: %w", err)
	}

	if s.cache != nil {
		_ = s.cache.Del(ctx, redis.RepoKey(id))
	}

	return models.RepositoryToResponse(repo), nil
}

func (s *RepositoryService) DeleteRepository(ctx context.Context, id, organizationID string, actor Actor) error {
	repo, err := s.repo.GetRepository(ctx, id)
	if err != nil {
		return fmt.Errorf("get repository: %w", err)
	}
	if repo == nil {
		return ErrRepositoryNotFound
	}
	if repo.OrganizationID != organizationID {
		return ErrForbidden
	}
	if !s.CanWriteRepository(ctx, actor.Role, actor.UserID, repo) {
		return ErrNotRepositoryOwner
	}

	if err := s.repo.DeleteRepository(ctx, id); err != nil {
		return fmt.Errorf("delete repository: %w", err)
	}

	if s.cache != nil {
		_ = s.cache.Del(ctx, redis.RepoKey(id))
	}

	return nil
}
