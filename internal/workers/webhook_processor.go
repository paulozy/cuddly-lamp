package workers

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/hibiken/asynq"
	"github.com/paulozy/idp-with-ai-backend/internal/jobs"
	"github.com/paulozy/idp-with-ai-backend/internal/jobs/tasks"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/services"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
	"github.com/paulozy/idp-with-ai-backend/internal/utils"
)

type WebhookProcessor struct {
	repo        storage.Repository
	syncService *services.SyncService
	enqueuer    jobs.Enqueuer
}

func NewWebhookProcessor(repo storage.Repository, svc *services.SyncService, enqueuer jobs.Enqueuer) *WebhookProcessor {
	return &WebhookProcessor{
		repo:        repo,
		syncService: svc,
		enqueuer:    enqueuer,
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

		// Mark embeddings as stale when the push touches indexable code. We
		// only escalate `indexed` → `stale` so we don't regress a job that's
		// already pending/indexing/failed.
		repoForStale, err := w.repo.GetRepository(ctx, repoID)
		if err == nil && repoForStale != nil &&
			repoForStale.EmbeddingsStatus == models.EmbeddingsStatusIndexed &&
			webhookPayloadHasIndexableChanges(webhook.EventPayload.RawData) {
			repoForStale.EmbeddingsStatus = models.EmbeddingsStatusStale
			if err := w.repo.UpdateRepository(ctx, repoForStale); err != nil {
				utils.Warn("webhook processor: mark embeddings stale failed", "repo_id", repoID, "error", err)
			}
		}

	default:
		utils.Info("webhook processor: ignoring event type", "event", webhook.EventType)
	}
	return nil
}

// indexableExtensions is the set of source-code extensions that the embedding
// chunker recognises. A push that only touches docs (README/CHANGELOG/etc.)
// shouldn't invalidate the index.
var indexableExtensions = map[string]struct{}{
	".go": {}, ".ts": {}, ".tsx": {}, ".js": {}, ".jsx": {},
	".py": {}, ".rs": {}, ".java": {}, ".rb": {}, ".php": {},
	".cs": {}, ".kt": {}, ".swift": {}, ".cpp": {}, ".c": {}, ".h": {}, ".hpp": {},
	".scala": {}, ".sql": {}, ".sh": {}, ".bash": {},
}

// webhookPayloadHasIndexableChanges returns true when at least one
// added/modified file in any of the push's commits has a source-code
// extension that the embedding worker would index. Pure-doc pushes return
// false so we don't mark indexes as `stale` for README edits.
func webhookPayloadHasIndexableChanges(raw map[string]interface{}) bool {
	body, _ := raw["body"].(string)
	if body == "" {
		return false
	}
	var payload struct {
		Commits []struct {
			Added    []string `json:"added"`
			Modified []string `json:"modified"`
		} `json:"commits"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return false
	}
	for _, commit := range payload.Commits {
		for _, file := range append(commit.Added, commit.Modified...) {
			ext := strings.ToLower(filepath.Ext(file))
			if _, ok := indexableExtensions[ext]; ok {
				return true
			}
		}
	}
	return false
}
