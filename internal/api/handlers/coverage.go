package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/services"
	"github.com/paulozy/idp-with-ai-backend/internal/utils"
)

const (
	headerCoverageFormat = "X-Coverage-Format"
	headerCommitSHA      = "X-Commit-SHA"
	headerBranch         = "X-Coverage-Branch"
)

type CoverageHandler struct {
	service *services.CoverageService
}

func NewCoverageHandler(service *services.CoverageService) *CoverageHandler {
	return &CoverageHandler{service: service}
}

// IngestCoverage uploads a CI-generated coverage report.
// @Summary      Upload a coverage report
// @Description  Authenticates with a coverage upload token (Bearer cov_*). The body is the raw report bytes; format and commit SHA come from headers.
// @Tags         coverage
// @Accept       octet-stream
// @Produce      json
// @Param        id                path      string true  "Repository ID"
// @Param        Authorization     header    string true  "Coverage upload token (Bearer cov_*)"
// @Param        X-Coverage-Format header    string true  "go|lcov|cobertura|jacoco"
// @Param        X-Commit-SHA      header    string true  "Commit SHA (7-64 hex chars)"
// @Param        X-Coverage-Branch header    string false "Branch name (optional)"
// @Param        body              body      string true  "Raw coverage report"
// @Success      200               {object}  models.CoverageUploadResponse
// @Failure      400               {object}  models.ErrorResponse "Invalid headers"
// @Failure      401               {object}  models.ErrorResponse "Invalid or expired token"
// @Failure      413               {object}  models.ErrorResponse "Body exceeds size limit"
// @Failure      415               {object}  models.ErrorResponse "Unsupported coverage format"
// @Failure      422               {object}  models.ErrorResponse "Failed to parse coverage payload"
// @Router       /repositories/{id}/coverage [post]
func (h *CoverageHandler) IngestCoverage(c *gin.Context) {
	repoID := c.Param("id")
	if repoID == "" {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: "missing repository id"})
		return
	}

	token := extractBearerToken(c)
	if token == "" {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{Error: "missing or malformed Authorization header"})
		return
	}

	format := c.GetHeader(headerCoverageFormat)
	sha := c.GetHeader(headerCommitSHA)
	branch := c.GetHeader(headerBranch)

	if format == "" {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: "missing X-Coverage-Format header"})
		return
	}
	if sha == "" {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: "missing X-Commit-SHA header"})
		return
	}

	upload, err := h.service.IngestCoverage(c.Request.Context(), services.IngestRequest{
		RepositoryID: repoID,
		Token:        token,
		Format:       format,
		CommitSHA:    sha,
		Branch:       branch,
		Body:         c.Request.Body,
	})
	if err != nil {
		mapCoverageError(c, err)
		return
	}

	warnings := []string(upload.Warnings)
	c.JSON(http.StatusOK, models.CoverageUploadResponse{
		ID:           upload.ID,
		CommitSHA:    upload.CommitSHA,
		Format:       string(upload.Format),
		LinesCovered: upload.LinesCovered,
		LinesTotal:   upload.LinesTotal,
		Percentage:   upload.Percentage,
		Status:       string(upload.Status),
		Warnings:     warnings,
	})
}

func extractBearerToken(c *gin.Context) string {
	auth := c.GetHeader("Authorization")
	if auth == "" {
		return ""
	}
	const prefix = "Bearer "
	if len(auth) <= len(prefix) || auth[:len(prefix)] != prefix {
		return ""
	}
	return auth[len(prefix):]
}

func mapCoverageError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, services.ErrCoverageInvalidFormat):
		c.JSON(http.StatusUnsupportedMediaType, models.ErrorResponse{Error: err.Error()})
	case errors.Is(err, services.ErrCoverageInvalidSHA):
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: err.Error()})
	case errors.Is(err, services.ErrCoverageBodyTooLarge):
		c.JSON(http.StatusRequestEntityTooLarge, models.ErrorResponse{Error: err.Error()})
	case errors.Is(err, services.ErrCoverageParseFailed):
		c.JSON(http.StatusUnprocessableEntity, models.ErrorResponse{Error: err.Error()})
	case errors.Is(err, services.ErrCoverageTokenInvalid),
		errors.Is(err, services.ErrCoverageTokenExpired),
		errors.Is(err, services.ErrCoverageTokenForeignRepo):
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{Error: err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "ingest coverage failed: " + err.Error()})
	}
}

// CreateCoverageToken creates a new upload token for the repository.
// @Summary      Create a coverage upload token
// @Tags         coverage
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id   path      string                            true  "Repository ID"
// @Param        body body      models.CreateCoverageTokenRequest true  "Token name + optional expiry"
// @Success      201  {object}  models.CreateCoverageTokenResponse
// @Failure      400  {object}  models.ErrorResponse
// @Failure      401  {object}  models.ErrorResponse
// @Router       /repositories/{id}/coverage/tokens [post]
func (h *CoverageHandler) CreateCoverageToken(c *gin.Context) {
	repoID := c.Param("id")
	if repoID == "" {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: "missing repository id"})
		return
	}

	var req models.CreateCoverageTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: "invalid body: " + err.Error()})
		return
	}

	// Best-effort: capture the creator for audit. If the auth context is
	// missing for any reason, we still create the token with a NULL author.
	uid, _ := utils.GetUserIDFromContext(c)

	plain, model, err := h.service.CreateUploadToken(c.Request.Context(), repoID, req.Name, uid, req.ExpiresAt)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "create token failed: " + err.Error()})
		return
	}

	c.JSON(http.StatusCreated, models.CreateCoverageTokenResponse{
		ID:        model.ID,
		Name:      model.Name,
		Token:     plain,
		ExpiresAt: model.ExpiresAt,
		CreatedAt: model.CreatedAt,
	})
}

// ListCoverageTokens lists all upload tokens for the repository (newest first).
// @Summary      List coverage upload tokens
// @Tags         coverage
// @Produce      json
// @Security     BearerAuth
// @Param        id   path      string true "Repository ID"
// @Success      200  {array}   models.CoverageTokenResponse
// @Failure      401  {object}  models.ErrorResponse
// @Router       /repositories/{id}/coverage/tokens [get]
func (h *CoverageHandler) ListCoverageTokens(c *gin.Context) {
	repoID := c.Param("id")
	if repoID == "" {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: "missing repository id"})
		return
	}
	tokens, err := h.service.ListUploadTokens(c.Request.Context(), repoID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "list tokens failed: " + err.Error()})
		return
	}
	out := make([]models.CoverageTokenResponse, 0, len(tokens))
	for _, t := range tokens {
		out = append(out, models.CoverageTokenToResponse(t))
	}
	c.JSON(http.StatusOK, out)
}

// RevokeCoverageToken revokes an existing upload token.
// @Summary      Revoke a coverage upload token
// @Tags         coverage
// @Produce      json
// @Security     BearerAuth
// @Param        id      path  string true "Repository ID"
// @Param        tokenID path  string true "Token ID"
// @Success      204
// @Failure      401     {object}  models.ErrorResponse
// @Failure      404     {object}  models.ErrorResponse
// @Router       /repositories/{id}/coverage/tokens/{tokenID} [delete]
func (h *CoverageHandler) RevokeCoverageToken(c *gin.Context) {
	repoID := c.Param("id")
	tokenID := c.Param("tokenID")
	if repoID == "" || tokenID == "" {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: "missing ids"})
		return
	}
	if err := h.service.RevokeUploadToken(c.Request.Context(), repoID, tokenID); err != nil {
		if errors.Is(err, services.ErrCoverageTokenNotFound) {
			c.JSON(http.StatusNotFound, models.ErrorResponse{Error: err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "revoke failed: " + err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}
