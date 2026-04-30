package workers

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/hibiken/asynq"
	"github.com/paulozy/idp-with-ai-backend/internal/dependencies"
	"github.com/paulozy/idp-with-ai-backend/internal/integrations/github"
	"github.com/paulozy/idp-with-ai-backend/internal/jobs"
	"github.com/paulozy/idp-with-ai-backend/internal/jobs/tasks"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/services"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
	"github.com/paulozy/idp-with-ai-backend/internal/utils"
)

type WebhookProcessor struct {
	repo          storage.Repository
	syncService   *services.SyncService
	enqueuer      jobs.Enqueuer
	githubFactory func(token string) github.ClientInterface
}

func NewWebhookProcessor(repo storage.Repository, svc *services.SyncService, enqueuer jobs.Enqueuer) *WebhookProcessor {
	return &WebhookProcessor{
		repo:        repo,
		syncService: svc,
		enqueuer:    enqueuer,
		githubFactory: func(token string) github.ClientInterface {
			return github.NewClient(token)
		},
	}
}

func (w *WebhookProcessor) Handle(ctx context.Context, task *asynq.Task) error {
	var payload tasks.WebhookProcessPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return fmt.Errorf("webhook processor: unmarshal payload: %w", err)
	}
	if payload.WebhookID == "" {
		return fmt.Errorf("webhook processor: empty webhook_id")
	}

	webhook, err := w.repo.GetWebhook(ctx, payload.WebhookID)
	if err != nil {
		return fmt.Errorf("webhook processor: fetch webhook: %w", err)
	}
	if webhook == nil {
		return fmt.Errorf("webhook processor: webhook %q not found", payload.WebhookID)
	}
	if !webhook.CanProcess() {
		utils.Info("webhook processor: skipping non-processable webhook", "webhook_id", payload.WebhookID, "status", webhook.Status)
		return nil
	}

	start := time.Now()
	webhook.MarkAsProcessing()
	if err := w.repo.UpdateWebhook(ctx, webhook); err != nil {
		utils.Warn("webhook processor: failed to mark as processing", "webhook_id", payload.WebhookID, "error", err)
	}

	processErr := w.processEvent(ctx, webhook)

	elapsed := time.Since(start).Milliseconds()
	if processErr != nil {
		utils.Error("webhook processor: processing failed", "webhook_id", payload.WebhookID, "error", processErr)
		webhook.MarkAsFailed(processErr.Error())
		_ = w.repo.UpdateWebhook(ctx, webhook)
		return processErr
	}

	webhook.MarkAsCompleted(models.WebhookProcessingResult{
		Success:          true,
		ProcessedAt:      time.Now().UTC(),
		ProcessingTimeMs: elapsed,
	})
	_ = w.repo.UpdateWebhook(ctx, webhook)
	return nil
}

func (w *WebhookProcessor) processEvent(ctx context.Context, webhook *models.Webhook) error {
	repoID := webhook.RepositoryID
	if repoID == "" {
		return fmt.Errorf("webhook has no repository_id")
	}

	switch webhook.EventType {
	case models.WebhookEventPush:
		utils.Info("webhook processor: triggering sync", "event", webhook.EventType, "repo_id", repoID)
		syncPayload := tasks.SyncRepoPayload{RepositoryID: repoID}
		if err := w.enqueuer.Enqueue(ctx, tasks.TypeSyncRepo, syncPayload); err != nil {
			return fmt.Errorf("enqueue sync job: %w", err)
		}
		if webhookPayloadHasManifestChanges(webhook.EventPayload.RawData) {
			scanPayload := tasks.ScanDependenciesPayload{
				RepositoryID: repoID,
				Branch:       webhook.EventPayload.Branch,
				CommitSHA:    webhook.EventPayload.CommitSHA,
				TriggeredBy:  "webhook",
			}
			if err := w.enqueuer.Enqueue(ctx, tasks.TypeScanDependencies, scanPayload); err != nil {
				utils.Warn("webhook processor: failed to enqueue dependency scan", "repo_id", repoID, "error", err)
			}
		}

		// Trigger analysis for push events if repository needs analysis
		repo, err := w.repo.GetRepository(ctx, repoID)
		if err == nil && repo != nil && repo.AnalysisStatus != "in_progress" {
			// Check token budget
			limit := w.tokenLimitForRepository(ctx, repo)
			used, err := w.repo.SumTokensUsedSince(ctx, repo.OrganizationID, time.Now().UTC().Add(-time.Hour))
			if err != nil || used < limit {
				analyzePayload := tasks.AnalyzeRepoPayload{
					RepositoryID: repoID,
					Branch:       webhook.EventPayload.Branch,
					CommitSHA:    webhook.EventPayload.CommitSHA,
					Type:         "code_review",
					TriggeredBy:  "webhook",
				}
				if err := w.enqueuer.Enqueue(ctx, tasks.TypeAnalyzeRepo, analyzePayload); err != nil {
					utils.Warn("webhook processor: failed to enqueue analysis", "repo_id", repoID, "error", err)
					// Don't fail the whole webhook if analysis fails to enqueue
				}
			} else {
				utils.Warn("webhook processor: skipping analysis due to token budget", "repo_id", repoID, "tokens_used", used, "limit", limit)
			}
		}

	case models.WebhookEventPullRequest:
		utils.Info("webhook processor: triggering analysis for PR", "event", webhook.EventType, "repo_id", repoID)

		// Check token budget
		repo, repoErr := w.repo.GetRepository(ctx, repoID)
		if repoErr != nil || repo == nil {
			return fmt.Errorf("fetch repository: %w", repoErr)
		}
		limit := w.tokenLimitForRepository(ctx, repo)
		used, err := w.repo.SumTokensUsedSince(ctx, repo.OrganizationID, time.Now().UTC().Add(-time.Hour))
		if err != nil || used < limit {
			// For PR events, trigger analysis directly
			var prID int64
			if webhook.EventPayload.PullRequestID != nil {
				prID = int64(*webhook.EventPayload.PullRequestID)
			}
			analyzePayload := tasks.AnalyzeRepoPayload{
				RepositoryID:  repoID,
				Branch:        webhook.EventPayload.Branch,
				CommitSHA:     webhook.EventPayload.CommitSHA,
				PullRequestID: prID,
				Type:          "code_review",
				TriggeredBy:   "webhook",
			}
			if err := w.enqueuer.Enqueue(ctx, tasks.TypeAnalyzeRepo, analyzePayload); err != nil {
				return fmt.Errorf("enqueue analysis job: %w", err)
			}
		} else {
			utils.Warn("webhook processor: skipping PR analysis due to token budget", "repo_id", repoID, "tokens_used", used, "limit", limit)
		}
		if w.pullRequestHasManifestChanges(ctx, repo, webhook) {
			prID := 0
			if webhook.EventPayload.PullRequestID != nil {
				prID = *webhook.EventPayload.PullRequestID
			}
			scanPayload := tasks.ScanDependenciesPayload{
				RepositoryID:  repoID,
				Branch:        webhook.EventPayload.Branch,
				CommitSHA:     webhook.EventPayload.CommitSHA,
				PullRequestID: prID,
				TriggeredBy:   "webhook",
			}
			if err := w.enqueuer.Enqueue(ctx, tasks.TypeScanDependencies, scanPayload); err != nil {
				utils.Warn("webhook processor: failed to enqueue PR dependency scan", "repo_id", repoID, "error", err)
			}
		}

	default:
		utils.Info("webhook processor: ignoring event type", "event", webhook.EventType)
	}
	return nil
}

func webhookPayloadHasManifestChanges(raw map[string]interface{}) bool {
	body, _ := raw["body"].(string)
	if body == "" {
		return false
	}
	var payload struct {
		Commits []struct {
			Added    []string `json:"added"`
			Modified []string `json:"modified"`
		} `json:"commits"`
		Ref   string `json:"ref"`
		After string `json:"after"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return false
	}
	for _, commit := range payload.Commits {
		for _, file := range append(commit.Added, commit.Modified...) {
			if dependencies.IsManifestFile(filepath.Base(file)) {
				return true
			}
		}
	}
	return false
}

func (w *WebhookProcessor) pullRequestHasManifestChanges(ctx context.Context, repo *models.Repository, webhook *models.Webhook) bool {
	if webhook.EventPayload.PullRequestID == nil {
		return false
	}
	cfg, err := w.repo.GetOrganizationConfig(ctx, repo.OrganizationID)
	if err != nil || cfg == nil {
		return false
	}
	ownerRepo, _, err := utils.ParseRepositoryURL(repo.URL)
	if err != nil {
		return false
	}
	parts := strings.Split(ownerRepo, "/")
	if len(parts) != 2 {
		return false
	}
	files, err := w.githubFactory(cfg.GithubToken).GetPullRequestFiles(ctx, parts[0], parts[1], int64(*webhook.EventPayload.PullRequestID))
	if err != nil {
		utils.Warn("webhook processor: failed to fetch PR files for dependency scan", "repo_id", repo.ID, "error", err)
		return false
	}
	for _, file := range files {
		if dependencies.IsManifestFile(filepath.Base(file.Filename)) {
			return true
		}
	}
	return false
}

func (w *WebhookProcessor) tokenLimitForRepository(ctx context.Context, repo *models.Repository) int64 {
	cfg, err := w.repo.GetOrganizationConfig(ctx, repo.OrganizationID)
	if err != nil || cfg == nil || cfg.AnthropicTokensPerHour <= 0 {
		return 20000
	}
	return int64(cfg.AnthropicTokensPerHour)
}
