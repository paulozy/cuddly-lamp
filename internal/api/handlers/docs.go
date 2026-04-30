package handlers

import (
	"errors"
	"fmt"
	"net/http"
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

type DocsHandler struct {
	repo     storage.Repository
	enqueuer jobs.Enqueuer
}

func NewDocsHandler(repo storage.Repository, enqueuer jobs.Enqueuer) *DocsHandler {
	return &DocsHandler{repo: repo, enqueuer: enqueuer}
}

// GenerateRepositoryDocs queues AI documentation generation for a repository.
// @Summary      Generate repository documentation
// @Tags         docs
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path      string                     true  "Repository ID"
// @Param        body  body      models.GenerateDocsRequest true  "Documentation generation options"
// @Success      202   {object}  models.DocGenerationAcceptedResponse
// @Failure      400   {object}  models.ErrorResponse
// @Failure      401   {object}  models.ErrorResponse
// @Failure      403   {object}  models.ErrorResponse
// @Failure      404   {object}  models.ErrorResponse
// @Failure      429   {object}  models.ErrorResponse
// @Failure      503   {object}  models.ErrorResponse
// @Router       /repositories/{id}/docs/generate [post]
func (h *DocsHandler) GenerateRepositoryDocs(c *gin.Context) {
	repository, ok := h.fetchAccessibleRepository(c, c.Param("id"))
	if !ok {
		return
	}
	userID, err := utils.GetUserIDFromContext(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{Error: "unauthorized", ErrorDescription: "missing or invalid user"})
		return
	}

	var req models.GenerateDocsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: "invalid_request", ErrorDescription: err.Error()})
		return
	}
	types, err := normalizeDocTypes(req.Types)
	if err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: "invalid_request", ErrorDescription: err.Error()})
		return
	}

	cfg, err := h.repo.GetOrganizationConfig(c.Request.Context(), repository.OrganizationID)
	if err != nil || cfg == nil || cfg.AnthropicAPIKey == "" {
		c.JSON(http.StatusServiceUnavailable, models.ErrorResponse{
			Error:            "docs_generation_unavailable",
			ErrorDescription: "anthropic api key is not configured for this organization",
		})
		return
	}
	if cfg.GithubToken == "" {
		c.JSON(http.StatusServiceUnavailable, models.ErrorResponse{
			Error:            "docs_generation_unavailable",
			ErrorDescription: "github token is not configured for this organization",
		})
		return
	}
	used, err := h.repo.SumTokensUsedSince(c.Request.Context(), repository.OrganizationID, time.Now().UTC().Add(-time.Hour))
	if err == nil && used >= int64(cfg.AnthropicTokensPerHour) {
		c.JSON(http.StatusTooManyRequests, models.ErrorResponse{
			Error:            "rate_limit_exceeded",
			ErrorDescription: fmt.Sprintf("token budget exhausted (%d/%d tokens used in last hour)", used, cfg.AnthropicTokensPerHour),
		})
		return
	}

	doc := &models.DocGeneration{
		ID:                uuid.NewString(),
		RepositoryID:      repository.ID,
		Status:            models.DocGenerationStatusPending,
		Types:             datatypes.JSONSlice[string](types),
		Branch:            strings.TrimSpace(req.Branch),
		Content:           datatypes.NewJSONType(map[string]string{}),
		TriggeredByUserID: userID,
		CreatedAt:         time.Now().UTC(),
		UpdatedAt:         time.Now().UTC(),
	}
	if err := h.repo.CreateDocGeneration(c.Request.Context(), doc); err != nil {
		utils.Error("docs handler: create doc generation failed", "repo_id", repository.ID, "error", err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "internal_error", ErrorDescription: "failed to create documentation job"})
		return
	}

	payload := tasks.GenerateDocsPayload{
		DocGenerationID: doc.ID,
		RepositoryID:    repository.ID,
		Types:           types,
		Branch:          doc.Branch,
		TriggeredByID:   userID,
	}
	taskID := fmt.Sprintf("docs:manual:%s", repository.ID)
	if err := h.enqueuer.Enqueue(c.Request.Context(), tasks.TypeGenerateDocs, payload, asynq.TaskID(taskID), asynq.Retention(10*time.Minute)); err != nil {
		if errors.Is(err, asynq.ErrTaskIDConflict) {
			c.JSON(http.StatusConflict, models.ErrorResponse{Error: "docs_generation_in_progress", ErrorDescription: "documentation generation for this repository is already queued or running"})
			return
		}
		utils.Error("docs handler: enqueue failed", "doc_generation_id", doc.ID, "error", err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "queue_error", ErrorDescription: "failed to enqueue documentation generation"})
		return
	}

	c.JSON(http.StatusAccepted, models.DocGenerationAcceptedResponse{ID: doc.ID, Status: doc.Status})
}

func (h *DocsHandler) fetchAccessibleRepository(c *gin.Context, repoID string) (*models.Repository, bool) {
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

func normalizeDocTypes(raw []string) ([]string, error) {
	seen := map[string]bool{}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		docType := strings.TrimSpace(item)
		switch ai.DocumentationType(docType) {
		case ai.DocumentationTypeADR, ai.DocumentationTypeArchitecture, ai.DocumentationTypeServiceDoc, ai.DocumentationTypeGuidelines:
			if !seen[docType] {
				seen[docType] = true
				out = append(out, docType)
			}
		default:
			return nil, fmt.Errorf("types must contain only: adr, architecture, service_doc, guidelines")
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("at least one documentation type is required")
	}
	return out, nil
}
