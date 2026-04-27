package handlers

import (
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	ghvalidation "github.com/paulozy/idp-with-ai-backend/internal/integrations/github"
	"github.com/paulozy/idp-with-ai-backend/internal/jobs"
	"github.com/paulozy/idp-with-ai-backend/internal/jobs/tasks"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
	"github.com/paulozy/idp-with-ai-backend/internal/utils"
)

type WebhookHandler struct {
	repo     storage.Repository
	enqueuer jobs.Enqueuer
}

func NewWebhookHandler(repo storage.Repository, enqueuer jobs.Enqueuer) *WebhookHandler {
	return &WebhookHandler{repo: repo, enqueuer: enqueuer}
}

func (h *WebhookHandler) HandleGitHubWebhook(c *gin.Context) {
	repoID := c.Param("repoID")
	if repoID == "" {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: "missing repository ID",
		})
		return
	}

	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:            "read_error",
			ErrorDescription: "failed to read request body",
		})
		return
	}

	ctx := c.Request.Context()

	webhookCfg, err := h.repo.GetWebhookConfigByRepoID(ctx, repoID)
	if err != nil {
		utils.Error("webhook handler: failed to fetch webhook config", "repo_id", repoID, "error", err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:            "internal_error",
			ErrorDescription: "failed to verify webhook",
		})
		return
	}
	if webhookCfg == nil {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{
			Error:            "unauthorized",
			ErrorDescription: "no webhook configured for this repository",
		})
		return
	}

	signature := c.GetHeader("X-Hub-Signature-256")
	if !ghvalidation.ValidateWebhookSignature(webhookCfg.Secret, body, signature) {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{
			Error:            "invalid_signature",
			ErrorDescription: "webhook signature validation failed",
		})
		return
	}

	deliveryID := c.GetHeader("X-GitHub-Delivery")
	if deliveryID == "" {
		deliveryID = "unknown-" + repoID + "-" + time.Now().Format("20060102150405")
	}

	existing, err := h.repo.GetWebhookByDeliveryID(ctx, deliveryID)
	if err != nil {
		utils.Error("webhook handler: failed to check idempotency", "delivery_id", deliveryID, "error", err)
	}
	if existing != nil {
		c.Status(http.StatusOK)
		return
	}

	eventType := resolveEventType(c.GetHeader("X-GitHub-Event"))

	webhook := &models.Webhook{
		RepositoryID: repoID,
		EventType:    eventType,
		EventPayload: models.WebhookEventPayload{
			EventType:    string(eventType),
			Provider:     "github",
			Timestamp:    time.Now().UTC(),
			RepositoryID: repoID,
			RawData:      map[string]interface{}{"body": string(body)},
		},
		Status:     "pending",
		DeliveryID: deliveryID,
		MaxRetries: 3,
	}

	if err := h.repo.CreateWebhook(ctx, webhook); err != nil {
		utils.Error("webhook handler: failed to persist webhook", "delivery_id", deliveryID, "error", err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:            "internal_error",
			ErrorDescription: "failed to record webhook delivery",
		})
		return
	}

	if err := h.enqueuer.Enqueue(ctx, tasks.TypeProcessWebhook, tasks.WebhookProcessPayload{WebhookID: webhook.ID}); err != nil {
		utils.Warn("webhook handler: failed to enqueue processing job", "webhook_id", webhook.ID, "error", err)
	}

	c.Status(http.StatusAccepted)
}

func resolveEventType(event string) models.WebhookEventType {
	switch event {
	case "push":
		return models.WebhookEventPush
	case "pull_request":
		return models.WebhookEventPullRequest
	case "issues":
		return models.WebhookEventIssue
	case "release":
		return models.WebhookEventRelease
	case "repository":
		return models.WebhookEventRepository
	case "workflow_run":
		return models.WebhookEventWorkflowRun
	default:
		return models.WebhookEventUnknown
	}
}
