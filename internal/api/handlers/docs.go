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
	"github.com/paulozy/idp-with-ai-backend/internal/docs"
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

	repoID := repository.ID
	doc := &models.DocGeneration{
		ID:                uuid.NewString(),
		OrganizationID:    repository.OrganizationID,
		Scope:             models.DocGenerationScopeRepo,
		RepositoryID:      &repoID,
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

// ListRepositoryDocs returns the documentation generation history for a
// repository. The response uses the lightweight summary projection (without
// the full Markdown `content` blob) so clients can paginate cheaply.
// @Summary      List documentation generations for a repository
// @Tags         docs
// @Produce      json
// @Security     BearerAuth
// @Param        id      path   string  true  "Repository ID"
// @Param        status  query  string  false  "Filter by status (pending, in_progress, completed, failed)"
// @Success      200  {object}  models.DocGenerationListResponse
// @Failure      400  {object}  models.ErrorResponse
// @Failure      401  {object}  models.ErrorResponse
// @Failure      403  {object}  models.ErrorResponse
// @Failure      404  {object}  models.ErrorResponse
// @Router       /repositories/{id}/docs [get]
func (h *DocsHandler) ListRepositoryDocs(c *gin.Context) {
	repository, ok := h.fetchAccessibleRepository(c, c.Param("id"))
	if !ok {
		return
	}

	statusFilter := strings.TrimSpace(c.Query("status"))
	if statusFilter != "" && !isValidDocStatusFilter(statusFilter) {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: "status must be one of: pending, in_progress, completed, failed",
		})
		return
	}

	docs, err := h.repo.ListDocGenerationsForRepo(c.Request.Context(), repository.ID)
	if err != nil {
		utils.Error("docs handler: list doc generations failed", "repo_id", repository.ID, "error", err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "internal_error", ErrorDescription: "failed to list documentation generations"})
		return
	}

	items := make([]models.DocGenerationSummary, 0, len(docs))
	for i := range docs {
		if statusFilter != "" && string(docs[i].Status) != statusFilter {
			continue
		}
		items = append(items, models.ToDocGenerationSummary(&docs[i]))
	}
	c.JSON(http.StatusOK, models.DocGenerationListResponse{Items: items, Total: len(items)})
}

// GetDocGeneration returns the full doc generation record, including the
// Markdown `content` keyed by documentation type.
// @Summary      Get a documentation generation
// @Tags         docs
// @Produce      json
// @Security     BearerAuth
// @Param        id   path      string  true  "DocGeneration ID"
// @Success      200  {object}  models.DocGeneration
// @Failure      400  {object}  models.ErrorResponse
// @Failure      401  {object}  models.ErrorResponse
// @Failure      403  {object}  models.ErrorResponse
// @Failure      404  {object}  models.ErrorResponse
// @Router       /docs/{id} [get]
func (h *DocsHandler) GetDocGeneration(c *gin.Context) {
	docID := strings.TrimSpace(c.Param("id"))
	if docID == "" {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: "invalid_request", ErrorDescription: "documentation id is required"})
		return
	}

	doc, err := h.repo.GetDocGeneration(c.Request.Context(), docID)
	if err != nil {
		utils.Error("docs handler: get doc generation failed", "doc_id", docID, "error", err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "internal_error", ErrorDescription: "failed to fetch documentation"})
		return
	}
	if doc == nil {
		c.JSON(http.StatusNotFound, models.ErrorResponse{Error: "not_found", ErrorDescription: "documentation not found"})
		return
	}

	orgID, err := utils.GetOrganizationIDFromContext(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{Error: "unauthorized", ErrorDescription: "missing or invalid organization"})
		return
	}

	// Org-scope docs check the organization directly. Repo-scope docs still
	// resolve through the linked repository (kept for the existing flow).
	if doc.Scope == models.DocGenerationScopeOrg {
		if doc.OrganizationID != orgID {
			c.JSON(http.StatusForbidden, models.ErrorResponse{Error: "forbidden", ErrorDescription: "you do not have access to this documentation"})
			return
		}
		c.JSON(http.StatusOK, doc)
		return
	}

	if doc.RepositoryID == nil {
		c.JSON(http.StatusNotFound, models.ErrorResponse{Error: "not_found", ErrorDescription: "documentation not found"})
		return
	}
	repository, err := h.repo.GetRepository(c.Request.Context(), *doc.RepositoryID)
	if err != nil || repository == nil {
		c.JSON(http.StatusNotFound, models.ErrorResponse{Error: "not_found", ErrorDescription: "documentation not found"})
		return
	}
	if repository.OrganizationID != orgID {
		c.JSON(http.StatusForbidden, models.ErrorResponse{Error: "forbidden", ErrorDescription: "you do not have access to this documentation"})
		return
	}

	c.JSON(http.StatusOK, doc)
}

func isValidDocStatusFilter(s string) bool {
	switch models.DocGenerationStatus(s) {
	case models.DocGenerationStatusPending,
		models.DocGenerationStatusInProgress,
		models.DocGenerationStatusCompleted,
		models.DocGenerationStatusFailed:
		return true
	}
	return false
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

// normalizeOrgDocTypes only accepts the types valid for org-wide generation
// (adr / architecture / guidelines). Repo-only types are rejected.
func normalizeOrgDocTypes(raw []string) ([]string, error) {
	seen := map[string]bool{}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		t := strings.TrimSpace(item)
		switch ai.DocumentationType(t) {
		case ai.DocumentationTypeADR, ai.DocumentationTypeArchitecture, ai.DocumentationTypeGuidelines:
			if !seen[t] {
				seen[t] = true
				out = append(out, t)
			}
		default:
			return nil, fmt.Errorf("org documentation type must be one of: adr, architecture, guidelines")
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("at least one documentation type is required")
	}
	return out, nil
}

// requireOrgAdmin returns the calling user's organization id when they hold
// the admin role. Anything else short-circuits with a 401/403 and returns
// (_, false). Used by the org-scope endpoints (admin-only by design).
func (h *DocsHandler) requireOrgAdmin(c *gin.Context) (string, bool) {
	orgID, err := utils.GetOrganizationIDFromContext(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{Error: "unauthorized", ErrorDescription: "missing or invalid organization"})
		return "", false
	}
	claims, err := utils.GetClaimsFromContext(c)
	if err != nil || claims == nil {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{Error: "unauthorized", ErrorDescription: "missing or invalid claims"})
		return "", false
	}
	// `OrganizationRole` is the per-org role on the JWT (admin, maintainer,
	// developer, viewer). Only `admin` may generate or edit org-wide docs.
	if !strings.EqualFold(string(claims.OrganizationRole), "admin") {
		c.JSON(http.StatusForbidden, models.ErrorResponse{Error: "forbidden", ErrorDescription: "admin role is required for organization documentation"})
		return "", false
	}
	return orgID, true
}

// GenerateOrgDocs queues org-wide documentation generation. The request body
// must specify one or more `types` (adr / architecture / guidelines); when
// type=adr, `template_id` is also required.
// @Summary      Generate organization-wide documentation
// @Tags         docs
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body      models.GenerateOrgDocsRequest true  "Org-wide doc generation options"
// @Success      202   {object}  models.DocGenerationAcceptedResponse
// @Failure      400   {object}  models.ErrorResponse
// @Failure      401   {object}  models.ErrorResponse
// @Failure      403   {object}  models.ErrorResponse
// @Failure      429   {object}  models.ErrorResponse
// @Failure      503   {object}  models.ErrorResponse
// @Router       /organizations/docs/generate [post]
func (h *DocsHandler) GenerateOrgDocs(c *gin.Context) {
	orgID, ok := h.requireOrgAdmin(c)
	if !ok {
		return
	}
	userID, _ := utils.GetUserIDFromContext(c)

	var req models.GenerateOrgDocsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: "invalid_request", ErrorDescription: err.Error()})
		return
	}
	types, err := normalizeOrgDocTypes(req.Types)
	if err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: "invalid_request", ErrorDescription: err.Error()})
		return
	}
	// ADR generation needs a template — Architecture and Guidelines have a
	// single canonical template each (architecture-overview / guidelines-
	// engineering) so we don't enforce template_id for those.
	templateID := strings.TrimSpace(req.TemplateID)
	for _, t := range types {
		if ai.DocumentationType(t) == ai.DocumentationTypeADR {
			if _, found := docs.GetTemplate(templateID); !found {
				c.JSON(http.StatusBadRequest, models.ErrorResponse{
					Error:            "invalid_request",
					ErrorDescription: "ADR generation requires a valid template_id",
				})
				return
			}
		}
	}

	cfg, err := h.repo.GetOrganizationConfig(c.Request.Context(), orgID)
	if err != nil || cfg == nil || cfg.AnthropicAPIKey == "" {
		c.JSON(http.StatusServiceUnavailable, models.ErrorResponse{
			Error:            "docs_generation_unavailable",
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

	doc := &models.DocGeneration{
		ID:                uuid.NewString(),
		OrganizationID:    orgID,
		Scope:             models.DocGenerationScopeOrg,
		Status:            models.DocGenerationStatusPending,
		Types:             datatypes.JSONSlice[string](types),
		Content:           datatypes.NewJSONType(map[string]string{}),
		TriggeredByUserID: userID,
		UserPrompt:        strings.TrimSpace(req.Prompt),
		CreatedAt:         time.Now().UTC(),
		UpdatedAt:         time.Now().UTC(),
	}
	if templateID != "" {
		t := templateID
		doc.TemplateID = &t
	}
	if err := h.repo.CreateDocGeneration(c.Request.Context(), doc); err != nil {
		utils.Error("docs handler: create org doc generation failed", "org_id", orgID, "error", err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "internal_error", ErrorDescription: "failed to create documentation job"})
		return
	}

	payload := tasks.GenerateOrgDocsPayload{
		DocGenerationID: doc.ID,
		OrganizationID:  orgID,
		Types:           types,
		TemplateID:      templateID,
		UserPrompt:      strings.TrimSpace(req.Prompt),
		TriggeredByID:   userID,
	}
	taskID := fmt.Sprintf("docs:org:%s:%s", orgID, doc.ID)
	if err := h.enqueuer.Enqueue(c.Request.Context(), tasks.TypeGenerateOrgDocs, payload, asynq.TaskID(taskID), asynq.Retention(10*time.Minute)); err != nil {
		if errors.Is(err, asynq.ErrTaskIDConflict) {
			c.JSON(http.StatusConflict, models.ErrorResponse{Error: "docs_generation_in_progress", ErrorDescription: "documentation generation for this organization is already queued or running"})
			return
		}
		utils.Error("docs handler: enqueue org failed", "doc_generation_id", doc.ID, "error", err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "queue_error", ErrorDescription: "failed to enqueue org documentation generation"})
		return
	}

	c.JSON(http.StatusAccepted, models.DocGenerationAcceptedResponse{ID: doc.ID, Status: doc.Status})
}

// ListOrgDocs returns the org-wide documentation history (head rows only —
// the worker creates new rows when the user manually edits a doc).
// @Summary      List organization-wide documentation
// @Tags         docs
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  models.DocGenerationListResponse
// @Failure      401  {object}  models.ErrorResponse
// @Router       /organizations/docs [get]
func (h *DocsHandler) ListOrgDocs(c *gin.Context) {
	orgID, err := utils.GetOrganizationIDFromContext(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{Error: "unauthorized", ErrorDescription: "missing or invalid organization"})
		return
	}

	rows, err := h.repo.ListOrgDocGenerations(c.Request.Context(), orgID)
	if err != nil {
		utils.Error("docs handler: list org docs failed", "org_id", orgID, "error", err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "internal_error", ErrorDescription: "failed to list documentation"})
		return
	}

	items := make([]models.DocGenerationSummary, 0, len(rows))
	for i := range rows {
		items = append(items, models.ToDocGenerationSummary(&rows[i]))
	}
	c.JSON(http.StatusOK, models.DocGenerationListResponse{Items: items, Total: len(items)})
}

// UpdateDocContent commits a manual edit. Instead of mutating the existing
// row we insert a new row that supersedes the previous one — the chain
// preserves history and the listing endpoint only surfaces the head.
// @Summary      Edit a documentation generation (creates a new version)
// @Tags         docs
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path  string                          true  "DocGeneration ID"
// @Param        body  body  models.UpdateDocContentRequest  true  "New content map"
// @Success      200   {object}  models.DocGeneration
// @Failure      400   {object}  models.ErrorResponse
// @Failure      401   {object}  models.ErrorResponse
// @Failure      403   {object}  models.ErrorResponse
// @Failure      404   {object}  models.ErrorResponse
// @Router       /docs/{id} [patch]
func (h *DocsHandler) UpdateDocContent(c *gin.Context) {
	docID := strings.TrimSpace(c.Param("id"))
	if docID == "" {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: "invalid_request", ErrorDescription: "documentation id is required"})
		return
	}
	orgID, ok := h.requireOrgAdmin(c)
	if !ok {
		return
	}
	userID, _ := utils.GetUserIDFromContext(c)

	var req models.UpdateDocContentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: "invalid_request", ErrorDescription: err.Error()})
		return
	}
	if len(req.Content) == 0 {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: "invalid_request", ErrorDescription: "content must not be empty"})
		return
	}

	prev, err := h.repo.GetDocGeneration(c.Request.Context(), docID)
	if err != nil || prev == nil {
		c.JSON(http.StatusNotFound, models.ErrorResponse{Error: "not_found", ErrorDescription: "documentation not found"})
		return
	}
	if prev.OrganizationID != orgID {
		c.JSON(http.StatusForbidden, models.ErrorResponse{Error: "forbidden", ErrorDescription: "you do not have access to this documentation"})
		return
	}

	// Merge the patch over the previous content map: missing keys are
	// inherited from the previous version. Then create a new row that
	// supersedes the previous one.
	merged := map[string]string{}
	for k, v := range prev.Content.Data() {
		merged[k] = v
	}
	for k, v := range req.Content {
		merged[k] = v
	}

	now := time.Now().UTC()
	prevID := prev.ID
	next := &models.DocGeneration{
		ID:                uuid.NewString(),
		OrganizationID:    prev.OrganizationID,
		Scope:             prev.Scope,
		RepositoryID:      prev.RepositoryID,
		TemplateID:        prev.TemplateID,
		Status:            models.DocGenerationStatusCompleted,
		Types:             prev.Types,
		Content:           datatypes.NewJSONType(merged),
		TokensUsed:        0,
		TriggeredByUserID: userID,
		UserPrompt:        prev.UserPrompt,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if err := h.repo.CreateDocGeneration(c.Request.Context(), next); err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "internal_error", ErrorDescription: "failed to persist new version"})
		return
	}

	// Mark the previous row as superseded so the listing endpoint hides it.
	prev.SupersededByID = &next.ID
	prev.UpdatedAt = now
	if err := h.repo.UpdateDocGeneration(c.Request.Context(), prev); err != nil {
		// Best-effort — the new row is already stored; rolling back here
		// would leave inconsistent state. Log and continue.
		utils.Error("docs handler: mark superseded failed", "previous_id", prevID, "error", err)
	}

	c.JSON(http.StatusOK, next)
}

// ListDocTemplates returns the registry of org-wide doc templates that the
// frontend renders in its template-gallery modal.
// @Summary      List documentation templates
// @Tags         docs
// @Produce      json
// @Security     BearerAuth
// @Success      200  {array}   docs.DocTemplate
// @Failure      401  {object}  models.ErrorResponse
// @Router       /docs/templates [get]
func (h *DocsHandler) ListDocTemplates(c *gin.Context) {
	if _, err := utils.GetOrganizationIDFromContext(c); err != nil {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{Error: "unauthorized", ErrorDescription: "missing or invalid organization"})
		return
	}
	c.JSON(http.StatusOK, docs.Templates())
}
