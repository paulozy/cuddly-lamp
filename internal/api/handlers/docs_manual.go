package handlers

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/utils"
	"gorm.io/datatypes"
)

// Documentation written by a person, rather than generated.
//
// This lives in its own file to make one thing obvious: the manual path shares
// no dependency with the generation path. Generating requires an organization
// Anthropic key, a credential for the repository's host, room in the hourly
// token budget, and a working Redis/asynq — four ways to answer 503 or 429
// before any document exists. Writing one requires a database.
//
// That asymmetry is why the manual action is the primary one in the UI: it is
// the one that always works. It is also why these handlers are synchronous and
// answer 201 rather than 202 — there is no job to wait for.

// CreateRepositoryDoc stores a hand-written document for a repository.
// @Summary      Create a documentation entry by hand for a repository
// @Tags         docs
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path  string                          true  "Repository ID"
// @Param        body  body  models.CreateManualDocRequest   true  "Document type and Markdown"
// @Success      201   {object}  models.DocGenerationSummary
// @Failure      400   {object}  models.ErrorResponse
// @Failure      401   {object}  models.ErrorResponse
// @Failure      403   {object}  models.ErrorResponse
// @Failure      404   {object}  models.ErrorResponse
// @Router       /repositories/{id}/docs/manual [post]
func (h *DocsHandler) CreateRepositoryDoc(c *gin.Context) {
	repository, ok := h.fetchAccessibleRepository(c, c.Param("id"))
	if !ok {
		return
	}
	userID, err := utils.GetUserIDFromContext(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{Error: "unauthorized", ErrorDescription: "missing or invalid user"})
		return
	}

	req, docType, ok := bindManualDoc(c, normalizeDocTypes)
	if !ok {
		return
	}

	repoID := repository.ID
	doc := newManualDoc(repository.OrganizationID, userID, docType, req.Content)
	doc.Scope = models.DocGenerationScopeRepo
	doc.RepositoryID = &repoID

	h.persistManualDoc(c, doc)
}

// CreateOrgDoc stores a hand-written organization-wide document.
// @Summary      Create an organization documentation entry by hand
// @Tags         docs
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body  models.CreateManualDocRequest  true  "Document type and Markdown"
// @Success      201   {object}  models.DocGenerationSummary
// @Failure      400   {object}  models.ErrorResponse
// @Failure      401   {object}  models.ErrorResponse
// @Failure      403   {object}  models.ErrorResponse
// @Router       /organizations/docs/manual [post]
func (h *DocsHandler) CreateOrgDoc(c *gin.Context) {
	// Same gate as generating an org doc: org-wide documentation speaks for the
	// whole organization, so writing one by hand is no less privileged than
	// asking Claude to.
	orgID, ok := h.requireOrgAdmin(c)
	if !ok {
		return
	}
	userID, err := utils.GetUserIDFromContext(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{Error: "unauthorized", ErrorDescription: "missing or invalid user"})
		return
	}

	req, docType, ok := bindManualDoc(c, normalizeOrgDocTypes)
	if !ok {
		return
	}

	doc := newManualDoc(orgID, userID, docType, req.Content)
	doc.Scope = models.DocGenerationScopeOrg

	h.persistManualDoc(c, doc)
}

// bindManualDoc parses the body and validates the single type against the
// scope's vocabulary, which differs: `service_doc` describes one service and so
// has no org-wide meaning.
func bindManualDoc(
	c *gin.Context,
	normalize func([]string) ([]string, error),
) (models.CreateManualDocRequest, string, bool) {
	var req models.CreateManualDocRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: "invalid_request", ErrorDescription: err.Error()})
		return req, "", false
	}

	// Reuse the generation path's vocabulary rather than a second list: a type
	// only writable by hand, or only generatable, would be a trap.
	types, err := normalize([]string{req.Type})
	if err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: "invalid_request", ErrorDescription: err.Error()})
		return req, "", false
	}

	if strings.TrimSpace(req.Content) == "" {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: "content must not be empty",
		})
		return req, "", false
	}

	return req, types[0], true
}

// newManualDoc builds the row. It is `completed` on arrival — there is nothing
// to run, so any other status would leave the UI polling for an event that will
// never come (see isTerminalDocStatus on the client).
func newManualDoc(orgID, userID, docType, content string) *models.DocGeneration {
	now := time.Now().UTC()
	return &models.DocGeneration{
		ID:                uuid.NewString(),
		OrganizationID:    orgID,
		Source:            models.DocSourceManual,
		Status:            models.DocGenerationStatusCompleted,
		Types:             datatypes.JSONSlice[string]{docType},
		Content:           datatypes.NewJSONType(map[string]string{docType: content}),
		TokensUsed:        0,
		TriggeredByUserID: userID,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
}

func (h *DocsHandler) persistManualDoc(c *gin.Context, doc *models.DocGeneration) {
	if !doc.IsValid() {
		// Defensive: the two callers set scope and repository together, so
		// reaching this means a new caller got the pairing wrong.
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: "document scope and repository do not agree",
		})
		return
	}
	if err := h.repo.CreateDocGeneration(c.Request.Context(), doc); err != nil {
		utils.Error("docs handler: create manual doc failed", "scope", string(doc.Scope), "error", err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:            "internal_error",
			ErrorDescription: "failed to store the document",
		})
		return
	}
	c.JSON(http.StatusCreated, models.ToDocGenerationSummary(doc))
}
