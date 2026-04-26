package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/services"
	"github.com/paulozy/idp-with-ai-backend/internal/utils"
)

type AuthHandler struct {
	authService *services.AuthService
}

func NewAuthHandler(authService *services.AuthService) *AuthHandler {
	return &AuthHandler{
		authService: authService,
	}
}

func (h *AuthHandler) LoginWithEmail(c *gin.Context) {
	var req models.LoginRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: err.Error(),
		})
		return
	}

	tokenResponse, err := h.authService.LoginWithEmail(c.Request.Context(), req.Email, req.Password)
	if err != nil {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{
			Error:            "authentication_failed",
			ErrorDescription: err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, tokenResponse)
}

func (h *AuthHandler) RegisterWithEmail(c *gin.Context) {
	var req struct {
		Email    string `json:"email" binding:"required,email"`
		FullName string `json:"full_name" binding:"required"`
		Password string `json:"password" binding:"required,min=8"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: err.Error(),
		})
		return
	}

	tokenResponse, err := h.authService.RegisterWithEmail(c.Request.Context(), req.Email, req.FullName, req.Password)
	if err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:            "registration_failed",
			ErrorDescription: err.Error(),
		})
		return
	}

	c.JSON(http.StatusCreated, tokenResponse)
}

func (h *AuthHandler) Logout(c *gin.Context) {
	token, err := utils.ExtractTokenFromHeader(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: err.Error(),
		})
		return
	}

	if err := h.authService.RevokeToken(c.Request.Context(), token); err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:            "logout_failed",
			ErrorDescription: err.Error(),
		})
		return
	}

	var req models.LogoutRequest
	if err := c.ShouldBindJSON(&req); err == nil && req.RefreshToken != "" {
		_ = h.authService.RevokeRefreshTokenFamily(c.Request.Context(), req.RefreshToken)
	}

	c.JSON(http.StatusNoContent, nil)
}

func (h *AuthHandler) RefreshTokens(c *gin.Context) {
	var req models.RefreshRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: err.Error(),
		})
		return
	}

	resp, err := h.authService.RefreshTokens(c.Request.Context(), req.RefreshToken)
	if err != nil {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{
			Error:            "invalid_grant",
			ErrorDescription: err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, resp)
}

func (h *AuthHandler) GetCurrentUser(c *gin.Context) {
	userID, err := utils.GetUserIDFromContext(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{
			Error:            "unauthorized",
			ErrorDescription: err.Error(),
		})
		return
	}

	claims, _ := utils.GetClaimsFromContext(c)

	c.JSON(http.StatusOK, models.UserInfo{
		ID:       userID,
		Email:    claims.Email,
		FullName: claims.FullName,
		Role:     claims.Role,
	})
}

func (h *AuthHandler) OAuthLogin(c *gin.Context) {
	provider := c.Param("provider")

	state, err := h.authService.GenerateOAuthState()
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:            "oauth_error",
			ErrorDescription: "failed to generate state: " + err.Error(),
		})
		return
	}

	authURL := h.authService.GetOAuthAuthURL(provider, state)
	if authURL == "" {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:            "invalid_provider",
			ErrorDescription: "unknown oauth provider: " + provider,
		})
		return
	}

	c.Redirect(http.StatusTemporaryRedirect, authURL)
}

func (h *AuthHandler) OAuthCallback(c *gin.Context) {
	provider := c.Param("provider")
	code := c.Query("code")
	state := c.Query("state")

	if code == "" || state == "" {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: "missing code or state parameter",
		})
		return
	}

	tokenResponse, err := h.authService.LoginWithOAuth(c.Request.Context(), provider, code, state)
	if err != nil {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{
			Error:            "oauth_authentication_failed",
			ErrorDescription: err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, tokenResponse)
}
