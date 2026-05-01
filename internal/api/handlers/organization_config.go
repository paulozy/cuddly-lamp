package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/paulozy/idp-with-ai-backend/internal/i18n"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
	"github.com/paulozy/idp-with-ai-backend/internal/utils"
)

type OrganizationConfigHandler struct {
	repo storage.Repository
}

func NewOrganizationConfigHandler(repo storage.Repository) *OrganizationConfigHandler {
	return &OrganizationConfigHandler{repo: repo}
}

// GetConfig retrieve the organization configs
// @Summary Get organization Configs
// @Tags organizations
// @Produce json
// @Security BearerAuth
// @Success      200   {object}  models.OrganizationConfigResponse
// @Failure      401   {object}  models.ErrorResponse
// @Router       /organizations/configs [get]
func (h *OrganizationConfigHandler) GetConfig(c *gin.Context) {
	orgID, ok := h.requireAdmin(c)
	if !ok {
		return
	}

	cfg, err := h.getOrCreateConfig(c, orgID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:            "config_error",
			ErrorDescription: err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, models.OrganizationConfigToResponse(cfg))
}

// UpdateConfig update/set the organization configs
// @Summary Update/Set organization configs
// @Tags organizations
// @Accept       json
// @Produce      json
// @Param        body  body      models.UpdateOrganizationConfigRequest  true  "Update config request"
// @Security     BearerAuth
// @Success      200   {object}  models.OrganizationConfigResponse
// @Failure      401   {object}  models.ErrorResponse
// @Failure      400   {object}  models.ErrorResponse
// @Failure      500   {object}  models.ErrorResponse
// @Router       /organizations/configs [patch]
func (h *OrganizationConfigHandler) UpdateConfig(c *gin.Context) {
	orgID, ok := h.requireAdmin(c)
	if !ok {
		return
	}

	var req models.UpdateOrganizationConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: err.Error(),
		})
		return
	}

	if req.OutputLanguage != nil && *req.OutputLanguage != "" {
		canonical, _, err := i18n.Resolve(*req.OutputLanguage)
		if err != nil {
			c.JSON(http.StatusBadRequest, models.ErrorResponse{
				Error:            "invalid_output_language",
				ErrorDescription: err.Error(),
			})
			return
		}
		*req.OutputLanguage = canonical
	}

	cfg, err := h.getOrCreateConfig(c, orgID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:            "config_error",
			ErrorDescription: err.Error(),
		})
		return
	}

	applyOrganizationConfigUpdate(cfg, req)
	cfg.UpdatedAt = time.Now().UTC()
	if err := h.repo.UpsertOrganizationConfig(c.Request.Context(), cfg); err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:            "config_error",
			ErrorDescription: err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, models.OrganizationConfigToResponse(cfg))
}

func (h *OrganizationConfigHandler) requireAdmin(c *gin.Context) (string, bool) {
	claims, err := utils.GetClaimsFromContext(c)
	if err != nil || !utils.HasPermission(claims.OrganizationRole, models.RoleAdmin) {
		c.JSON(http.StatusForbidden, models.ErrorResponse{
			Error:            "forbidden",
			ErrorDescription: "admin role is required",
		})
		return "", false
	}
	if claims.OrganizationID == "" {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{
			Error:            "unauthorized",
			ErrorDescription: "missing organization",
		})
		return "", false
	}
	return claims.OrganizationID, true
}

func (h *OrganizationConfigHandler) getOrCreateConfig(c *gin.Context, orgID string) (*models.OrganizationConfig, error) {
	cfg, err := h.repo.GetOrganizationConfig(c.Request.Context(), orgID)
	if err != nil {
		return nil, err
	}
	if cfg != nil {
		return cfg, nil
	}
	cfg = &models.OrganizationConfig{
		OrganizationID: orgID,
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	}
	cfg.ApplyDefaults()
	if err := h.repo.UpsertOrganizationConfig(c.Request.Context(), cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func applyOrganizationConfigUpdate(cfg *models.OrganizationConfig, req models.UpdateOrganizationConfigRequest) {
	if req.AnthropicAPIKey != nil {
		cfg.AnthropicAPIKey = *req.AnthropicAPIKey
	}
	if req.AnthropicTokensPerHour != nil {
		cfg.AnthropicTokensPerHour = *req.AnthropicTokensPerHour
	}
	if req.GithubToken != nil {
		cfg.GithubToken = *req.GithubToken
	}
	if req.GitHubPRReviewEnabled != nil {
		cfg.GitHubPRReviewEnabled = *req.GitHubPRReviewEnabled
	}
	if req.WebhookBaseURL != nil {
		cfg.WebhookBaseURL = *req.WebhookBaseURL
	}
	if req.EmbeddingsProvider != nil {
		cfg.EmbeddingsProvider = *req.EmbeddingsProvider
	}
	if req.VoyageAPIKey != nil {
		cfg.VoyageAPIKey = *req.VoyageAPIKey
	}
	if req.EmbeddingsModel != nil {
		cfg.EmbeddingsModel = *req.EmbeddingsModel
	}
	if req.EmbeddingsDimensions != nil {
		cfg.EmbeddingsDimensions = *req.EmbeddingsDimensions
	}
	if req.GitHubClientID != nil {
		cfg.GitHubClientID = *req.GitHubClientID
	}
	if req.GitHubClientSecret != nil {
		cfg.GitHubClientSecret = *req.GitHubClientSecret
	}
	if req.GitHubCallbackURL != nil {
		cfg.GitHubCallbackURL = *req.GitHubCallbackURL
	}
	if req.GitLabClientID != nil {
		cfg.GitLabClientID = *req.GitLabClientID
	}
	if req.GitLabClientSecret != nil {
		cfg.GitLabClientSecret = *req.GitLabClientSecret
	}
	if req.GitLabCallbackURL != nil {
		cfg.GitLabCallbackURL = *req.GitLabCallbackURL
	}
	if req.OutputLanguage != nil {
		cfg.OutputLanguage = *req.OutputLanguage
	}
	cfg.ApplyDefaults()
}
