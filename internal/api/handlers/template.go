package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/paulozy/idp-with-ai-backend/internal/ai"
	"github.com/paulozy/idp-with-ai-backend/internal/jobs"
	"github.com/paulozy/idp-with-ai-backend/internal/jobs/tasks"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
	"github.com/paulozy/idp-with-ai-backend/internal/utils"
	"gorm.io/datatypes"
)

type TemplateHandler struct {
	repo     storage.Repository
	enqueuer jobs.Enqueuer
}

func NewTemplateHandler(repo storage.Repository, enqueuer jobs.Enqueuer) *TemplateHandler {
	return &TemplateHandler{repo: repo, enqueuer: enqueuer}
}

// GenerateForRepository queues AI template generation using repository stack context.
// @Summary      Generate repository code template
// @Tags         templates
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path      string                         true  "Repository ID"
// @Param        body  body      models.GenerateTemplateRequest true  "Template generation prompt"
// @Success      202   {object}  models.TemplateAcceptedResponse
// @Failure      400   {object}  models.ErrorResponse
// @Failure      401   {object}  models.ErrorResponse
// @Failure      403   {object}  models.ErrorResponse
// @Failure      404   {object}  models.ErrorResponse
// @Failure      429   {object}  models.ErrorResponse
// @Failure      503   {object}  models.ErrorResponse
// @Router       /repositories/{id}/templates [post]
func (h *TemplateHandler) GenerateForRepository(c *gin.Context) {
	repository, ok := h.fetchAccessibleRepository(c, c.Param("id"))
	if !ok {
		return
	}
	h.generate(c, repository)
}

// GenerateForOrganization queues AI template generation without repository context.
// @Summary      Generate organization code template
// @Tags         templates
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body      models.GenerateTemplateRequest true  "Template generation prompt"
// @Success      202   {object}  models.TemplateAcceptedResponse
// @Failure      400   {object}  models.ErrorResponse
// @Failure      401   {object}  models.ErrorResponse
// @Failure      429   {object}  models.ErrorResponse
// @Failure      503   {object}  models.ErrorResponse
// @Router       /templates [post]
func (h *TemplateHandler) GenerateForOrganization(c *gin.Context) {
	h.generate(c, nil)
}

func (h *TemplateHandler) generate(c *gin.Context, repository *models.Repository) {
	orgID, err := utils.GetOrganizationIDFromContext(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{Error: "unauthorized", ErrorDescription: "missing or invalid organization"})
		return
	}
	userID, err := utils.GetUserIDFromContext(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{Error: "unauthorized", ErrorDescription: "missing or invalid user"})
		return
	}

	var req models.GenerateTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: "invalid_request", ErrorDescription: err.Error()})
		return
	}
	req.Prompt = strings.TrimSpace(req.Prompt)
	req.StackHint = strings.TrimSpace(req.StackHint)
	if req.Prompt == "" {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: "invalid_request", ErrorDescription: "prompt is required"})
		return
	}

	cfg, err := h.repo.GetOrganizationConfig(c.Request.Context(), orgID)
	if err != nil || cfg == nil || cfg.AnthropicAPIKey == "" {
		c.JSON(http.StatusServiceUnavailable, models.ErrorResponse{
			Error:            "template_generation_unavailable",
			ErrorDescription: "anthropic api key is not configured for this organization",
		})
		return
	}
	used, err := h.repo.SumTokensUsedSince(c.Request.Context(), orgID, time.Now().UTC().Add(-time.Hour))
	if err == nil && used >= int64(cfg.AnthropicTokensPerHour) {
		c.JSON(http.StatusTooManyRequests, models.ErrorResponse{
			Error:            "rate_limit_exceeded",
			ErrorDescription: fmt.Sprintf("token budget exhausted (%d/%d tokens used in last hour)", used, cfg.AnthropicTokensPerHour),
		})
		return
	}

	var repoID *string
	payloadRepoID := ""
	if repository != nil {
		payloadRepoID = repository.ID
		repoID = &payloadRepoID
	}
	template := &models.CodeTemplate{
		ID:              uuid.NewString(),
		OrganizationID:  orgID,
		RepositoryID:    repoID,
		CreatedByUserID: &userID,
		Prompt:          req.Prompt,
		StackHint:       req.StackHint,
		StackSnapshot:   datatypes.NewJSONType(ai.StackProfile{}),
		Status:          models.TemplateStatusPending,
		Files:           datatypes.NewJSONType([]ai.GeneratedFile{}),
		CreatedAt:       time.Now().UTC(),
		UpdatedAt:       time.Now().UTC(),
	}
	if err := h.repo.CreateCodeTemplate(c.Request.Context(), template); err != nil {
		utils.Error("template handler: create template failed", "error", err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "internal_error", ErrorDescription: "failed to create template"})
		return
	}

	payload := tasks.GenerateTemplatePayload{
		TemplateID:     template.ID,
		OrganizationID: orgID,
		RepositoryID:   payloadRepoID,
		Prompt:         req.Prompt,
		StackHint:      req.StackHint,
		TriggeredByID:  userID,
	}
	taskID := fmt.Sprintf("template:manual:%s", template.ID)
	if err := h.enqueuer.Enqueue(c.Request.Context(), tasks.TypeGenerateTemplate, payload, asynq.TaskID(taskID), asynq.Retention(10*time.Minute)); err != nil {
		if errors.Is(err, asynq.ErrTaskIDConflict) {
			c.JSON(http.StatusConflict, models.ErrorResponse{Error: "template_generation_in_progress", ErrorDescription: "template generation is already queued or running"})
			return
		}
		utils.Error("template handler: enqueue failed", "template_id", template.ID, "error", err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "queue_error", ErrorDescription: "failed to enqueue template generation"})
		return
	}

	c.JSON(http.StatusAccepted, models.TemplateAcceptedResponse{ID: template.ID, Status: template.Status})
}

// GetTemplate retrieves a generated template by ID.
// @Summary      Get code template
// @Tags         templates
// @Produce      json
// @Security     BearerAuth
// @Param        id   path      string              true  "Template ID"
// @Success      200  {object}  models.CodeTemplate
// @Failure      400  {object}  models.ErrorResponse
// @Failure      401  {object}  models.ErrorResponse
// @Failure      403  {object}  models.ErrorResponse
// @Failure      404  {object}  models.ErrorResponse
// @Router       /templates/{id} [get]
func (h *TemplateHandler) GetTemplate(c *gin.Context) {
	template, ok := h.fetchAccessibleTemplate(c, c.Param("id"))
	if !ok {
		return
	}
	c.JSON(http.StatusOK, template)
}

// ListTemplates lists templates for the authenticated organization.
// @Summary      List code templates
// @Tags         templates
// @Produce      json
// @Security     BearerAuth
// @Param        pinned  query     bool    false  "Filter pinned templates"
// @Param        status  query     string  false  "Filter by status: pending, generating, completed, failed"
// @Param        limit   query     int     false  "Result limit (default 20)"
// @Param        offset  query     int     false  "Result offset (default 0)"
// @Success      200     {object}  models.TemplateListResponse
// @Failure      400     {object}  models.ErrorResponse
// @Failure      401     {object}  models.ErrorResponse
// @Router       /templates [get]
func (h *TemplateHandler) ListTemplates(c *gin.Context) {
	orgID, err := utils.GetOrganizationIDFromContext(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{Error: "unauthorized", ErrorDescription: "missing or invalid organization"})
		return
	}

	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if offset < 0 {
		offset = 0
	}

	var pinned *bool
	if raw := c.Query("pinned"); raw != "" {
		value, err := strconv.ParseBool(raw)
		if err != nil {
			c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: "invalid_request", ErrorDescription: "pinned must be true or false"})
			return
		}
		pinned = &value
	}

	status := strings.TrimSpace(c.Query("status"))
	if status != "" && !validTemplateStatus(status) {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: "invalid_request", ErrorDescription: "status must be one of: pending, generating, completed, failed"})
		return
	}

	templates, total, err := h.repo.ListCodeTemplates(c.Request.Context(), storage.CodeTemplateFilter{
		OrganizationID: orgID,
		IsPinned:       pinned,
		Status:         status,
		Limit:          limit,
		Offset:         offset,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "internal_error", ErrorDescription: "failed to fetch templates"})
		return
	}
	c.JSON(http.StatusOK, models.TemplateListResponse{Total: total, Templates: templates, Limit: limit, Offset: offset})
}

// PinTemplate pins or unpins a template for team reuse.
// @Summary      Pin code template
// @Tags         templates
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path      string                    true  "Template ID"
// @Param        body  body      models.PinTemplateRequest true  "Pin settings"
// @Success      200   {object}  models.CodeTemplate
// @Failure      400   {object}  models.ErrorResponse
// @Failure      401   {object}  models.ErrorResponse
// @Failure      403   {object}  models.ErrorResponse
// @Failure      404   {object}  models.ErrorResponse
// @Router       /templates/{id}/pin [patch]
func (h *TemplateHandler) PinTemplate(c *gin.Context) {
	template, ok := h.fetchAccessibleTemplate(c, c.Param("id"))
	if !ok {
		return
	}
	userID, err := utils.GetUserIDFromContext(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{Error: "unauthorized", ErrorDescription: "missing or invalid user"})
		return
	}

	var req models.PinTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: "invalid_request", ErrorDescription: err.Error()})
		return
	}
	now := time.Now().UTC()
	template.IsPinned = req.IsPinned
	template.Name = strings.TrimSpace(req.Name)
	if req.IsPinned {
		template.PinnedByUserID = &userID
		template.PinnedAt = &now
	} else {
		template.PinnedByUserID = nil
		template.PinnedAt = nil
	}
	if err := h.repo.UpdateCodeTemplate(c.Request.Context(), template); err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "internal_error", ErrorDescription: "failed to update template"})
		return
	}
	c.JSON(http.StatusOK, template)
}

func (h *TemplateHandler) fetchAccessibleTemplate(c *gin.Context, templateID string) (*models.CodeTemplate, bool) {
	if templateID == "" {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: "invalid_request", ErrorDescription: "template id is required"})
		return nil, false
	}
	template, err := h.repo.GetCodeTemplate(c.Request.Context(), templateID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "internal_error", ErrorDescription: "failed to fetch template"})
		return nil, false
	}
	if template == nil {
		c.JSON(http.StatusNotFound, models.ErrorResponse{Error: "not_found", ErrorDescription: "template not found"})
		return nil, false
	}
	orgID, err := utils.GetOrganizationIDFromContext(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{Error: "unauthorized", ErrorDescription: "missing or invalid organization"})
		return nil, false
	}
	if template.OrganizationID != orgID {
		c.JSON(http.StatusForbidden, models.ErrorResponse{Error: "forbidden", ErrorDescription: "you do not have access to this template"})
		return nil, false
	}
	return template, true
}

func (h *TemplateHandler) fetchAccessibleRepository(c *gin.Context, repoID string) (*models.Repository, bool) {
	if repoID == "" {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: "invalid_request", ErrorDescription: "repository id is required"})
		return nil, false
	}
	repository, err := h.repo.GetRepository(c.Request.Context(), repoID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "internal_error", ErrorDescription: "failed to fetch repository"})
		return nil, false
	}
	if repository == nil {
		c.JSON(http.StatusNotFound, models.ErrorResponse{Error: "not_found", ErrorDescription: "repository not found"})
		return nil, false
	}
	orgID, err := utils.GetOrganizationIDFromContext(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{Error: "unauthorized", ErrorDescription: "missing or invalid organization"})
		return nil, false
	}
	if repository.OrganizationID != orgID {
		c.JSON(http.StatusForbidden, models.ErrorResponse{Error: "forbidden", ErrorDescription: "you do not have access to this repository"})
		return nil, false
	}
	return repository, true
}

func validTemplateStatus(status string) bool {
	switch models.TemplateStatus(status) {
	case models.TemplateStatusPending, models.TemplateStatusGenerating, models.TemplateStatusCompleted, models.TemplateStatusFailed:
		return true
	default:
		return false
	}
}
