package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/paulozy/idp-with-ai-backend/internal/integrations/github"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
	rediscache "github.com/paulozy/idp-with-ai-backend/internal/storage/redis"
	"github.com/paulozy/idp-with-ai-backend/internal/utils"
)

var (
	ErrSyncInProgress = errors.New("repository sync already in progress")
	// ErrUnsupportedProvider guards the GitHub-only sync path. The catalog
	// accepts GitLab and Gitea URLs, but only GitHub can be synced today —
	// without this check the URL's owner/repo pair would be queried against
	// api.github.com, silently importing a same-named GitHub project's data.
	ErrUnsupportedProvider = errors.New("repository sync is only supported for github repositories")
)

type SyncService struct {
	repo           storage.Repository
	github         github.ClientInterface
	cache          rediscache.Cache
	webhookBaseURL string
}

func NewSyncService(repo storage.Repository, gh github.ClientInterface, cache rediscache.Cache, webhookBaseURL string) *SyncService {
	return &SyncService{
		repo:           repo,
		github:         gh,
		cache:          cache,
		webhookBaseURL: webhookBaseURL,
	}
}

func (s *SyncService) SyncRepository(ctx context.Context, repoID string) error {
	repo, err := s.repo.GetRepository(ctx, repoID)
	if err != nil {
		return fmt.Errorf("fetch repository: %w", err)
	}
	if repo == nil {
		return ErrRepositoryNotFound
	}
	if !repo.CanSync() {
		return ErrSyncInProgress
	}

	// ParseRepositoryURL returns "owner/repo" as name, plus the provider the
	// host resolves to. The provider must be honoured: owner/repo is not unique
	// across forges, so syncing a gitlab.com URL through the GitHub client can
	// import an unrelated project that happens to share the path.
	ownerRepo, provider, err := utils.ParseRepositoryURL(repo.URL)
	if err != nil {
		return fmt.Errorf("parse repository URL: %w", err)
	}
	if provider != models.RepositoryTypeGitHub {
		errMsg := fmt.Sprintf("%s: %s", ErrUnsupportedProvider.Error(), provider)
		repo.UpdateSyncStatus("error", &errMsg)
		if updateErr := s.repo.UpdateRepository(ctx, repo); updateErr != nil {
			utils.Warn("sync: failed to record unsupported provider", "repo_id", repoID, "error", updateErr)
		}
		utils.Warn("sync: unsupported provider", "repo_id", repoID, "provider", provider)
		return fmt.Errorf("%w: %s", ErrUnsupportedProvider, provider)
	}
	parts := strings.SplitN(ownerRepo, "/", 2)
	owner, name := parts[0], parts[1]

	repo.SyncStatus = "syncing"
	if updateErr := s.repo.UpdateRepository(ctx, repo); updateErr != nil {
		utils.Warn("sync: failed to set syncing status", "repo_id", repoID, "error", updateErr)
	}

	if syncErr := s.doSync(ctx, repo, owner, name); syncErr != nil {
		utils.Error("sync: failed", "repo_id", repoID, "error", syncErr)
		errMsg := syncErr.Error()
		repo.UpdateSyncStatus("error", &errMsg)
		_ = s.repo.UpdateRepository(ctx, repo)
		return syncErr
	}

	utils.Info("sync: completed", "repo_id", repoID)
	return nil
}

func (s *SyncService) doSync(ctx context.Context, repo *models.Repository, owner, name string) error {
	gh, webhookBaseURL, err := s.githubClientForRepository(ctx, repo)
	if err != nil {
		return err
	}

	info, err := gh.GetRepository(ctx, owner, name)
	if err != nil {
		return fmt.Errorf("get repository info: %w", err)
	}

	branches, err := gh.GetBranches(ctx, owner, name)
	if err != nil {
		return fmt.Errorf("get branches: %w", err)
	}

	defaultBranch := info.DefaultBranch
	if defaultBranch == "" {
		defaultBranch = "main"
	}

	commits, err := gh.GetCommits(ctx, owner, name, defaultBranch, 100)
	if err != nil && !errors.Is(err, github.ErrNotFound) {
		return fmt.Errorf("get commits: %w", err)
	}

	prs, err := gh.ListPullRequests(ctx, owner, name)
	if err != nil {
		return fmt.Errorf("list pull requests: %w", err)
	}

	languages := make(map[string]int)
	if info.Language != "" {
		languages[info.Language] = 100
	}

	repo.Metadata.ProviderID = strconv.Itoa(info.ID)
	repo.Metadata.OwnerName = owner
	repo.Metadata.DefaultBranch = defaultBranch
	repo.Metadata.Languages = languages
	repo.Metadata.Topics = info.Topics
	repo.Metadata.StarCount = info.StargazersCount
	repo.Metadata.ForkCount = info.ForksCount
	repo.Metadata.IssueCount = info.OpenIssuesCount
	repo.Metadata.BranchCount = len(branches)
	repo.Metadata.CommitCount = len(commits)
	repo.Metadata.PRCount = len(prs)

	repo.SyncStatus = "synced"
	repo.SyncError = ""
	repo.LastSyncedAt = time.Now().UTC()

	if err := s.repo.UpdateRepository(ctx, repo); err != nil {
		return fmt.Errorf("update repository: %w", err)
	}

	if s.cache != nil {
		_ = s.cache.Del(ctx, rediscache.RepoKey(repo.ID))
	}

	if webhookBaseURL != "" && !isLocalURL(webhookBaseURL) {
		if regErr := s.ensureWebhookRegistered(ctx, gh, webhookBaseURL, repo, owner, name); regErr != nil {
			utils.Warn("sync: webhook registration failed", "repo_id", repo.ID, "error", regErr)
		}
	} else if webhookBaseURL != "" {
		utils.Info("sync: skipping webhook registration (local URL not reachable by GitHub)", "repo_id", repo.ID)
	}

	return nil
}

func (s *SyncService) ensureWebhookRegistered(ctx context.Context, gh github.ClientInterface, webhookBaseURL string, repo *models.Repository, owner, name string) error {
	existing, err := s.repo.GetWebhookConfigByRepoID(ctx, repo.ID)
	if err != nil {
		return fmt.Errorf("check existing webhook config: %w", err)
	}
	if existing != nil && existing.IsActive {
		return nil
	}

	secret, err := generateSecret()
	if err != nil {
		return fmt.Errorf("generate webhook secret: %w", err)
	}

	webhookURL := fmt.Sprintf("%s/api/v1/webhooks/github/%s", webhookBaseURL, repo.ID)
	webhookID, err := gh.CreateWebhook(ctx, owner, name, webhookURL, secret)
	if err != nil {
		return fmt.Errorf("create github webhook: %w", err)
	}

	cfg := &models.WebhookConfig{
		RepositoryID:      repo.ID,
		WebhookURL:        webhookURL,
		Secret:            secret,
		Events:            models.StringArray{"push", "pull_request", "issues"},
		IsActive:          true,
		ProviderWebhookID: strconv.FormatInt(webhookID, 10),
		ProviderType:      "github",
	}

	if existing == nil {
		return s.repo.CreateWebhookConfig(ctx, cfg)
	}

	existing.Secret = secret
	existing.ProviderWebhookID = cfg.ProviderWebhookID
	existing.IsActive = true
	return s.repo.UpdateWebhookConfig(ctx, existing)
}

func (s *SyncService) githubClientForRepository(ctx context.Context, repo *models.Repository) (github.ClientInterface, string, error) {
	if repo.OrganizationID != "" {
		cfg, err := s.repo.GetOrganizationConfig(ctx, repo.OrganizationID)
		if err != nil {
			return nil, "", err
		}
		if cfg != nil {
			// Webhook base URL is a deployment-level fact (the public URL GitHub
			// must reach), so it comes solely from the server env, not per-org config.
			return github.NewClient(cfg.GithubToken), s.webhookBaseURL, nil
		}
	}
	if s.github != nil {
		return s.github, s.webhookBaseURL, nil
	}
	return nil, "", fmt.Errorf("github is not configured for organization")
}

func isLocalURL(u string) bool {
	return WebhookRegistrationUnavailable(u)
}

// WebhookRegistrationUnavailable reports whether this deployment can register
// webhooks with the provider at all. An empty or localhost base URL is not
// reachable from GitHub, so registration is skipped — and the scorecard reports
// the webhook check as not applicable rather than failing every repository for
// a platform-level configuration.
func WebhookRegistrationUnavailable(baseURL string) bool {
	u := strings.TrimSpace(baseURL)
	if u == "" {
		return true
	}
	return strings.Contains(u, "localhost") || strings.Contains(u, "127.0.0.1")
}

func generateSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
