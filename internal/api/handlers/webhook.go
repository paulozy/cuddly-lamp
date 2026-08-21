package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	ghvalidation "github.com/paulozy/idp-with-ai-backend/internal/integrations/github"
	"github.com/paulozy/idp-with-ai-backend/internal/integrations/gitlab"
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

// HandleGitHubWebhook receives and processes GitHub webhook events.
// @Summary      Receive GitHub webhook
// @Tags         webhooks
// @Accept       json
// @Param        repoID                 path    string  true  "Repository ID"
// @Param        X-Hub-Signature-256    header  string  true  "HMAC-SHA256 signature"
// @Param        X-GitHub-Delivery      header  string  true  "Unique delivery ID (idempotency)"
// @Param        X-GitHub-Event         header  string  true  "Event type (push, pull_request, issues, etc.)"
// @Success      202                    "Webhook accepted for processing"
// @Success      200                    "Duplicate delivery (already processed)"
// @Failure      400  {object}  models.ErrorResponse  "Missing repository ID"
// @Failure      401  {object}  models.ErrorResponse  "Invalid signature or no webhook configured"
// @Failure      500  {object}  models.ErrorResponse  "Internal server error"
// @Router       /webhooks/github/{repoID} [post]
func (h *WebhookHandler) HandleGitHubWebhook(c *gin.Context) {
	repoID, body, webhookCfg, ok := h.acceptDelivery(c)
	if !ok {
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

	eventType := resolveEventType(c.GetHeader("X-GitHub-Event"))
	eventPayload := models.WebhookEventPayload{
		EventType:    string(eventType),
		Provider:     "github",
		Timestamp:    time.Now().UTC(),
		RepositoryID: repoID,
		RawData:      map[string]interface{}{"body": string(body)},
	}
	enrichGitHubEventPayload(body, &eventPayload)

	h.recordDelivery(c, repoID, deliveryID, eventType, eventPayload)
}

// HandleGitLabWebhook receives and processes GitLab webhook events.
//
// GitLab authenticates deliveries with a shared secret echoed back in a header
// rather than by signing the body, so there is no HMAC to recompute here — see
// gitlab.ValidateWebhookToken.
//
// @Summary      Receive GitLab webhook
// @Tags         webhooks
// @Accept       json
// @Param        repoID                path    string  true   "Repository ID"
// @Param        X-Gitlab-Token        header  string  true   "Webhook secret token"
// @Param        X-Gitlab-Event        header  string  true   "Event name (Push Hook, Merge Request Hook, ...)"
// @Param        X-Gitlab-Event-UUID   header  string  false  "Delivery ID (idempotency); derived from the payload when absent"
// @Success      202                   "Webhook accepted for processing"
// @Success      200                   "Duplicate delivery (already processed)"
// @Failure      400  {object}  models.ErrorResponse  "Missing repository ID"
// @Failure      401  {object}  models.ErrorResponse  "Invalid token or no webhook configured"
// @Failure      500  {object}  models.ErrorResponse  "Internal server error"
// @Router       /webhooks/gitlab/{repoID} [post]
func (h *WebhookHandler) HandleGitLabWebhook(c *gin.Context) {
	repoID, body, webhookCfg, ok := h.acceptDelivery(c)
	if !ok {
		return
	}

	if !gitlab.ValidateWebhookToken(webhookCfg.Secret, c.GetHeader("X-Gitlab-Token")) {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{
			Error:            "invalid_token",
			ErrorDescription: "webhook token validation failed",
		})
		return
	}

	var raw gitlabEventBody
	// A body that will not parse is still authenticated, so it is recorded
	// rather than rejected — the event type simply stays unknown.
	_ = json.Unmarshal(body, &raw)

	eventType := resolveGitLabEventType(raw.ObjectKind, c.GetHeader("X-Gitlab-Event"))
	eventPayload := models.WebhookEventPayload{
		EventType:    string(eventType),
		Provider:     "gitlab",
		Timestamp:    time.Now().UTC(),
		RepositoryID: repoID,
		RawData:      map[string]interface{}{"body": string(body)},
	}
	raw.enrich(&eventPayload)

	h.recordDelivery(c, repoID, gitlabDeliveryID(c.GetHeader("X-Gitlab-Event-UUID"), repoID, raw), eventType, eventPayload)
}

// acceptDelivery performs the checks every provider's receiver shares: a
// repository ID, a readable body, and a webhook configuration to authenticate
// against. It writes the error response itself and reports whether to continue.
func (h *WebhookHandler) acceptDelivery(c *gin.Context) (string, []byte, *models.WebhookConfig, bool) {
	repoID := c.Param("repoID")
	if repoID == "" {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: "missing repository ID",
		})
		return "", nil, nil, false
	}

	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:            "read_error",
			ErrorDescription: "failed to read request body",
		})
		return "", nil, nil, false
	}

	webhookCfg, err := h.repo.GetWebhookConfigByRepoID(c.Request.Context(), repoID)
	if err != nil {
		utils.Error("webhook handler: failed to fetch webhook config", "repo_id", repoID, "error", err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:            "internal_error",
			ErrorDescription: "failed to verify webhook",
		})
		return "", nil, nil, false
	}
	if webhookCfg == nil {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{
			Error:            "unauthorized",
			ErrorDescription: "no webhook configured for this repository",
		})
		return "", nil, nil, false
	}

	return repoID, body, webhookCfg, true
}

// recordDelivery is the shared tail of every receiver: drop duplicates, persist
// the delivery, and hand it to the worker.
func (h *WebhookHandler) recordDelivery(c *gin.Context, repoID, deliveryID string, eventType models.WebhookEventType, payload models.WebhookEventPayload) {
	ctx := c.Request.Context()

	existing, err := h.repo.GetWebhookByDeliveryID(ctx, deliveryID)
	if err != nil {
		utils.Error("webhook handler: failed to check idempotency", "delivery_id", deliveryID, "error", err)
	}
	if existing != nil {
		c.Status(http.StatusOK)
		return
	}

	webhook := &models.Webhook{
		RepositoryID: repoID,
		EventType:    eventType,
		EventPayload: payload,
		Status:       "pending",
		DeliveryID:   deliveryID,
		MaxRetries:   3,
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

func enrichGitHubEventPayload(body []byte, payload *models.WebhookEventPayload) {
	var raw struct {
		Ref        string `json:"ref"`
		After      string `json:"after"`
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
		PullRequest struct {
			ID     int `json:"id"`
			Number int `json:"number"`
			Head   struct {
				Ref string `json:"ref"`
				SHA string `json:"sha"`
			} `json:"head"`
		} `json:"pull_request"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return
	}
	payload.RepositoryName = raw.Repository.FullName
	if raw.Ref != "" {
		payload.Branch = strings.TrimPrefix(raw.Ref, "refs/heads/")
	}
	if raw.After != "" {
		payload.CommitSHA = raw.After
	}
	if raw.PullRequest.Number > 0 {
		payload.PullRequestID = &raw.PullRequest.Number
		payload.PullRequestNumber = &raw.PullRequest.Number
		payload.Branch = raw.PullRequest.Head.Ref
		payload.CommitSHA = raw.PullRequest.Head.SHA
	}
}

// gitlabEventBody is the subset of GitLab's payloads the platform reads. Push
// and merge request events share the envelope but carry their details in
// different places, which is what `enrich` reconciles.
type gitlabEventBody struct {
	ObjectKind string `json:"object_kind"`
	Ref        string `json:"ref"`
	// CheckoutSHA is the commit the push landed on; `after` carries the same
	// value and is kept as a fallback for older payloads.
	CheckoutSHA string `json:"checkout_sha"`
	After       string `json:"after"`
	UserName    string `json:"user_name"`
	Project     struct {
		ID                int    `json:"id"`
		PathWithNamespace string `json:"path_with_namespace"`
	} `json:"project"`
	User struct {
		Username string `json:"username"`
		Name     string `json:"name"`
	} `json:"user"`
	ObjectAttributes struct {
		IID          int    `json:"iid"`
		Action       string `json:"action"`
		SourceBranch string `json:"source_branch"`
		TargetBranch string `json:"target_branch"`
		LastCommit   struct {
			ID      string `json:"id"`
			Message string `json:"message"`
		} `json:"last_commit"`
	} `json:"object_attributes"`
}

// enrich fills the neutral payload the worker already understands, so nothing
// downstream needs to know a delivery came from GitLab.
func (raw gitlabEventBody) enrich(payload *models.WebhookEventPayload) {
	payload.RepositoryName = raw.Project.PathWithNamespace
	if actor := raw.actorName(); actor != "" {
		payload.ActorName = actor
	}

	if raw.Ref != "" {
		payload.Branch = strings.TrimPrefix(raw.Ref, "refs/heads/")
	}
	if sha := raw.pushSHA(); sha != "" {
		payload.CommitSHA = sha
	}

	if raw.ObjectKind == "merge_request" && raw.ObjectAttributes.IID > 0 {
		iid := raw.ObjectAttributes.IID
		// GitLab's IID is the per-project number, which is the same thing
		// GitHub's PR number is — so it belongs in both fields, as the GitHub
		// receiver also does.
		payload.PullRequestID = &iid
		payload.PullRequestNumber = &iid
		payload.Branch = raw.ObjectAttributes.SourceBranch
		payload.CommitSHA = raw.ObjectAttributes.LastCommit.ID
		payload.CommitMessage = raw.ObjectAttributes.LastCommit.Message
	}
}

func (raw gitlabEventBody) actorName() string {
	if raw.User.Username != "" {
		return raw.User.Username
	}
	if raw.User.Name != "" {
		return raw.User.Name
	}
	return raw.UserName
}

func (raw gitlabEventBody) pushSHA() string {
	if raw.CheckoutSHA != "" {
		return raw.CheckoutSHA
	}
	return raw.After
}

// gitlabDeliveryID produces the idempotency key for a GitLab delivery.
//
// GitLab sends X-Gitlab-Event-UUID, which is exactly the delivery identity
// GitHub provides, so it is preferred. When it is absent (older instances, or a
// recursive delivery reusing an ID), the key is derived from what identifies
// the event itself: kind, project and the commit or merge request it concerns.
// Deriving it means a retried delivery is recognized as the same event rather
// than replayed as a new one.
func gitlabDeliveryID(eventUUID, repoID string, raw gitlabEventBody) string {
	if uuid := strings.TrimSpace(eventUUID); uuid != "" {
		return "gitlab-" + uuid
	}

	kind := raw.ObjectKind
	if kind == "" {
		kind = "unknown"
	}
	subject := raw.pushSHA()
	if raw.ObjectAttributes.IID > 0 {
		subject = fmt.Sprintf("mr-%d-%s-%s", raw.ObjectAttributes.IID, raw.ObjectAttributes.Action, raw.ObjectAttributes.LastCommit.ID)
	}
	if subject == "" {
		// Nothing in the payload identifies the event; fall back to time so a
		// delivery is still recorded rather than colliding with an unrelated one.
		subject = time.Now().UTC().Format("20060102150405.000000")
	}
	return fmt.Sprintf("gitlab-%s-%s-%d-%s", repoID, kind, raw.Project.ID, subject)
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

// resolveGitLabEventType prefers the payload's object_kind over the
// X-Gitlab-Event header: the header is a display name ("Merge Request Hook")
// whose formatting has changed across versions, while object_kind is the
// machine-readable field the payload is built around.
func resolveGitLabEventType(objectKind, header string) models.WebhookEventType {
	kind := objectKind
	if kind == "" {
		kind = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(header), "Hook")))
		kind = strings.ReplaceAll(kind, " ", "_")
	}

	switch kind {
	case "push":
		return models.WebhookEventPush
	case "merge_request":
		return models.WebhookEventMergeRequest
	case "tag_push":
		return models.WebhookEventTag
	case "issue", "issues":
		return models.WebhookEventIssue
	case "pipeline":
		return models.WebhookEventPipeline
	case "release":
		return models.WebhookEventRelease
	default:
		return models.WebhookEventUnknown
	}
}
