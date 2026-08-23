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

	"github.com/paulozy/idp-with-ai-backend/internal/detect"
	"github.com/paulozy/idp-with-ai-backend/internal/integrations/scm"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
	rediscache "github.com/paulozy/idp-with-ai-backend/internal/storage/redis"
	"github.com/paulozy/idp-with-ai-backend/internal/utils"
)

var (
	ErrSyncInProgress = errors.New("repository sync already in progress")
	// ErrUnsupportedProvider guards the sync path against hosts we have no
	// client for. The catalog accepts any supported forge URL, but a
	// repository can only be synced through a client that speaks its host:
	// `owner/repo` is not unique across forges, so querying one host's path
	// against another silently imports an unrelated project's data.
	//
	// It aliases the resolver's error so callers can match either.
	ErrUnsupportedProvider = scm.ErrUnsupportedProvider
)

type SyncService struct {
	repo storage.Repository
	// architecture extracts derivation facts from the tree this sync already
	// read. Optional: a deployment without it syncs exactly as before.
	architecture *ArchitectureService
	// resolve builds the provider client for a repository's host. Injectable
	// so tests can supply a stub without reaching the network.
	resolve        scm.ResolverFunc
	credentials    scm.Credentials
	cache          rediscache.Cache
	webhookBaseURL string
}

// NewSyncService wires the sync service with the platform-level provider
// tokens, which act as the fallback for organizations that have not configured
// their own.
func NewSyncService(repo storage.Repository, creds scm.Credentials, cache rediscache.Cache, webhookBaseURL string) *SyncService {
	return &SyncService{
		repo:           repo,
		resolve:        scm.For,
		credentials:    creds,
		cache:          cache,
		webhookBaseURL: webhookBaseURL,
	}
}

// WithArchitecture attaches fact extraction to this sync service.
//
// It is a separate call rather than a constructor parameter because extraction
// is additive: every existing caller of NewSyncService keeps working unchanged,
// and a deployment that cannot reconcile still syncs.
func (s *SyncService) WithArchitecture(svc *ArchitectureService) *SyncService {
	s.architecture = svc
	return s
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

	// ParseRepositoryURL returns the project path plus the provider the host
	// resolves to. The provider must be honoured: the path is not unique
	// across forges, so syncing a GitLab URL through the GitHub client can
	// import an unrelated project that happens to share it.
	projectPath, provider, err := utils.ParseRepositoryURL(repo.URL)
	if err != nil {
		return fmt.Errorf("parse repository URL: %w", err)
	}
	ref, err := scm.ParseRepoRef(projectPath)
	if err != nil {
		return fmt.Errorf("parse repository URL: %w", err)
	}

	// Resolving before any status write keeps an unsyncable repository's
	// catalog row untouched apart from the recorded error.
	client, err := s.providerForRepository(ctx, repo, provider)
	if err != nil {
		errMsg := err.Error()
		repo.UpdateSyncStatus("error", &errMsg)
		if updateErr := s.persist(ctx, repo); updateErr != nil {
			utils.Warn("sync: failed to record provider resolution failure", "repo_id", repoID, "error", updateErr)
		}
		utils.Warn("sync: provider unavailable", "repo_id", repoID, "provider", provider, "error", err)
		return err
	}

	repo.SyncStatus = "syncing"
	if updateErr := s.persist(ctx, repo); updateErr != nil {
		utils.Warn("sync: failed to set syncing status", "repo_id", repoID, "error", updateErr)
	}

	if syncErr := s.doSync(ctx, repo, client, provider, ref); syncErr != nil {
		utils.Error("sync: failed", "repo_id", repoID, "error", syncErr)
		errMsg := syncErr.Error()
		repo.UpdateSyncStatus("error", &errMsg)
		_ = s.persist(ctx, repo)
		return syncErr
	}

	utils.Info("sync: completed", "repo_id", repoID)
	return nil
}

func (s *SyncService) doSync(ctx context.Context, repo *models.Repository, client scm.Provider, provider models.RepositoryType, ref scm.RepoRef) error {
	info, err := client.GetRepository(ctx, ref)
	if err != nil {
		return fmt.Errorf("get repository info: %w", err)
	}

	branches, err := client.ListBranches(ctx, ref)
	if err != nil {
		return fmt.Errorf("get branches: %w", err)
	}

	defaultBranch := info.DefaultBranch
	if defaultBranch == "" {
		defaultBranch = "main"
	}

	commits, err := client.ListCommits(ctx, ref, defaultBranch, 100)
	if err != nil && !errors.Is(err, scm.ErrNotFound) {
		return fmt.Errorf("get commits: %w", err)
	}

	changeRequests, err := client.ListChangeRequests(ctx, ref)
	if err != nil {
		return fmt.Errorf("list pull requests: %w", err)
	}

	// The real language breakdown. `info.Language` is only the dominant one, and
	// test detection gates on the full set. Soft-fail: an unavailable breakdown
	// degrades detection to "unknown", it does not fail the sync.
	languages, err := client.ListLanguages(ctx, ref)
	if err != nil || len(languages) == 0 {
		if err != nil {
			utils.Warn("sync: language breakdown unavailable", "repo_id", repo.ID, "error", err)
		}
		languages = make(map[string]int)
		if info.Language != "" {
			languages[info.Language] = 100
		}
	}

	// Detection is best-effort by design. A repository we cannot inspect must
	// report "unknown", never a confident "no CI, no tests" — and it must never
	// turn a healthy sync into sync_status=error, which would fail the
	// sync.healthy check as collateral damage.
	repo.Metadata.HasCI, repo.Metadata.CIEvidence = nil, ""
	repo.Metadata.HasTests, repo.Metadata.TestEvidence = nil, ""
	// The tree is read once. Detection and architecture extraction both consume
	// it, so adding the second costs no extra listing.
	var syncedTree *scm.RepoTree
	if tree, treeErr := client.GetTree(ctx, ref, defaultBranch); treeErr != nil {
		utils.Warn("sync: repository tree unavailable, CI/test signals left undetermined",
			"repo_id", repo.ID, "error", treeErr)
	} else if tree != nil {
		syncedTree = tree
		paths := tree.BlobPaths()
		applyDetection(repo, detect.DetectCI(paths, tree.Truncated), detect.DetectTests(paths, languages, tree.Truncated))
	}

	repo.Metadata.ProviderID = strconv.Itoa(info.ID)
	repo.Metadata.OwnerName = ref.Namespace
	repo.Metadata.DefaultBranch = defaultBranch
	repo.Metadata.Languages = languages
	repo.Metadata.Topics = info.Topics
	repo.Metadata.StarCount = info.StarCount
	repo.Metadata.ForkCount = info.ForkCount
	repo.Metadata.IssueCount = info.OpenIssueCount
	repo.Metadata.BranchCount = len(branches)
	repo.Metadata.CommitCount = len(commits)
	repo.Metadata.PRCount = len(changeRequests)

	repo.SyncStatus = "synced"
	repo.SyncError = ""
	repo.LastSyncedAt = time.Now().UTC()

	if err := s.persist(ctx, repo); err != nil {
		return fmt.Errorf("update repository: %w", err)
	}

	// Architecture extraction runs after the repository row is safely persisted,
	// so nothing it does can affect the sync's recorded status. It reports
	// failure by leaving the fact incomplete, never by returning an error — a
	// repository we cannot fully inspect must not fail sync.healthy as
	// collateral damage.
	if s.architecture != nil && syncedTree != nil {
		if s.architecture.ExtractFacts(ctx, repo, client, ref, defaultBranch, syncedTree) {
			s.architecture.EnqueueDerivation(ctx, repo.OrganizationID)
		}
	}

	if s.webhookBaseURL != "" && !isLocalURL(s.webhookBaseURL) {
		if regErr := s.ensureWebhookRegistered(ctx, client, provider, repo, ref); regErr != nil {
			utils.Warn("sync: webhook registration failed", "repo_id", repo.ID, "error", regErr)
		}
	} else if s.webhookBaseURL != "" {
		utils.Info("sync: skipping webhook registration (local URL not reachable by the provider)", "repo_id", repo.ID)
	}

	return nil
}

// persist writes the repository and drops its cached detail response.
//
// Every status the sync writes has to invalidate that cache, not just the
// successful one. The read path caches the response for an hour, so a
// repository fetched while it was still `idle` would keep reporting `idle`
// long after the sync failed — the UI would say "never synced" for what is
// really "sync failed", and the scorecard's sync.healthy check would read
// "not applicable" instead of failing. Only the successful path used to
// invalidate, which is exactly the path where staleness is harmless.
func (s *SyncService) persist(ctx context.Context, repo *models.Repository) error {
	if err := s.repo.UpdateRepository(ctx, repo); err != nil {
		// The write failed, so the cached copy still matches the database.
		return err
	}
	if s.cache != nil {
		_ = s.cache.Del(ctx, rediscache.RepoKey(repo.ID))
	}
	return nil
}

func (s *SyncService) ensureWebhookRegistered(ctx context.Context, client scm.WebhookRegistrar, provider models.RepositoryType, repo *models.Repository, ref scm.RepoRef) error {
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

	// Each provider posts to its own receiver, which is what lets the handler
	// know how to authenticate the delivery — HMAC for GitHub, a shared token
	// for GitLab.
	webhookURL := fmt.Sprintf("%s/api/v1/webhooks/%s/%s", s.webhookBaseURL, provider, repo.ID)
	hook, err := client.RegisterWebhook(ctx, ref, webhookURL, secret)
	if err != nil {
		return fmt.Errorf("create %s webhook: %w", provider, err)
	}

	cfg := &models.WebhookConfig{
		RepositoryID:      repo.ID,
		WebhookURL:        webhookURL,
		Secret:            secret,
		Events:            models.StringArray(hook.Events),
		IsActive:          true,
		ProviderWebhookID: hook.ID,
		ProviderType:      string(provider),
	}

	if existing == nil {
		return s.repo.CreateWebhookConfig(ctx, cfg)
	}

	existing.Secret = secret
	existing.ProviderWebhookID = cfg.ProviderWebhookID
	existing.IsActive = true
	return s.repo.UpdateWebhookConfig(ctx, existing)
}

// providerForRepository picks the client for the repository's host, preferring
// the organization's own token and falling back to the platform-level one.
func (s *SyncService) providerForRepository(ctx context.Context, repo *models.Repository, provider models.RepositoryType) (scm.Provider, error) {
	creds := s.credentials
	if repo.OrganizationID != "" {
		cfg, err := s.repo.GetOrganizationConfig(ctx, repo.OrganizationID)
		if err != nil {
			return nil, err
		}
		creds = scm.CredentialsFromConfig(cfg, s.credentials)
	}
	return s.resolve(provider, creds)
}

func isLocalURL(u string) bool {
	return WebhookRegistrationUnavailable(u)
}

// WebhookRegistrationUnavailable reports whether this deployment can register
// webhooks with the provider at all. An empty or localhost base URL is not
// reachable from the provider, so registration is skipped — and the scorecard
// reports the webhook check as not applicable rather than failing every
// repository for a platform-level configuration.
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

// applyDetection writes the two signals onto the repository's metadata. Only a
// definitive yes/no is recorded — `unknown` leaves the field nil, which is what
// makes the scorecard report "not verified" instead of inventing a "no".
func applyDetection(repo *models.Repository, ci, tests detect.Detection) {
	if ci.Result != detect.ResultUnknown {
		has := ci.Result == detect.ResultYes
		repo.Metadata.HasCI = &has
		repo.Metadata.CIEvidence = firstEvidence(ci)
	}
	if tests.Result != detect.ResultUnknown {
		has := tests.Result == detect.ResultYes
		repo.Metadata.HasTests = &has
		repo.Metadata.TestEvidence = firstEvidence(tests)
	}
}

func firstEvidence(d detect.Detection) string {
	if len(d.Evidence) == 0 {
		return ""
	}
	return d.Evidence[0]
}
