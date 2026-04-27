package workers

import (
	"context"
	"encoding/json"
	"fmt"
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
	case models.WebhookEventPush, models.WebhookEventPullRequest:
		utils.Info("webhook processor: triggering sync", "event", webhook.EventType, "repo_id", repoID)
		syncPayload := tasks.SyncRepoPayload{RepositoryID: repoID}
		if err := w.enqueuer.Enqueue(ctx, tasks.TypeSyncRepo, syncPayload); err != nil {
			return fmt.Errorf("enqueue sync job: %w", err)
		}
	default:
		utils.Info("webhook processor: ignoring event type", "event", webhook.EventType)
	}
	return nil
}
