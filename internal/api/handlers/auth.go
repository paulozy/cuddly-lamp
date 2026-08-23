package handlers

import (
	"errors"
	"net/http"
	"strings"

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

// LoginWithEmail authenticates a user via email + password.
// @Summary      Login with email
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        body  body      models.LoginRequest  true  "Credentials"
// @Success      200   {object}  models.TokenResponse
// @Success      202   {object}  models.OrganizationSelectionResponse
// @Failure      400   {object}  models.ErrorResponse
// @Failure      401   {object}  models.ErrorResponse
// @Router       /auth/login [post]
func (h *AuthHandler) LoginWithEmail(c *gin.Context) {
	var req models.LoginRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: err.Error(),
		})
		return
	}

	tokenResponse, selectionResponse, err := h.authService.LoginWithEmail(c.Request.Context(), req.Email, req.Password, c.Param("slug"))
	if err != nil {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{
			Error:            "authentication_failed",
			ErrorDescription: err.Error(),
		})
		return
	}

	if selectionResponse != nil {
		c.JSON(http.StatusAccepted, selectionResponse)
		return
	}

	c.JSON(http.StatusOK, tokenResponse)
}

// SelectOrganization completes login after the user selects an organization.
// @Summary      Select organization after login
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        body  body      models.SelectOrganizationRequest  true  "Organization selection"
// @Success      200   {object}  models.TokenResponse
// @Failure      400   {object}  models.ErrorResponse
// @Failure      401   {object}  models.ErrorResponse
// @Router       /auth/select-organization [post]
func (h *AuthHandler) SelectOrganization(c *gin.Context) {
	var req models.SelectOrganizationRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: err.Error(),
		})
		return
	}

	tokenResponse, err := h.authService.SelectOrganization(c.Request.Context(), req.LoginTicket, req.OrganizationID)
	if err != nil {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{
			Error:            "organization_selection_failed",
			ErrorDescription: err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, tokenResponse)
}

// RegisterWithEmail registers a new user via email + password.
// @Summary      Register with email
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        body  body      models.RegisterRequest  true  "User registration details"
// @Success      201   {object}  models.TokenResponse
// @Failure      400   {object}  models.ErrorResponse
// @Router       /auth/register [post]
func (h *AuthHandler) RegisterWithEmail(c *gin.Context) {
	var req models.RegisterRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: err.Error(),
		})
		return
	}

	if req.OrganizationSlug == "" {
		req.OrganizationSlug = c.Param("slug")
	}
	if req.OrganizationName == "" {
		req.OrganizationName = req.OrganizationSlug
	}
	// With an invite the organization comes from the token, so the caller does
	// not need to name it (and must not be able to override it).
	if req.OrganizationName == "" && strings.TrimSpace(req.InviteToken) == "" {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: "organization_name is required unless an invite_token is provided",
		})
		return
	}

	tokenResponse, err := h.authService.RegisterWithEmail(c.Request.Context(), req.Email, req.FullName, req.Password, req.OrganizationName, req.OrganizationSlug, req.InviteToken)
	if err != nil {
		// These two are distinct so the UI can tell the user what to do: ask for
		// an invite, versus pick a different organization name.
		switch {
		case errors.Is(err, services.ErrOrganizationRequiresInvite):
			c.JSON(http.StatusForbidden, models.ErrorResponse{
				Error:            "organization_requires_invite",
				ErrorDescription: "an organization with this name already exists; ask an admin for an invite",
			})
		case errors.Is(err, services.ErrInviteRejected):
			c.JSON(http.StatusForbidden, models.ErrorResponse{
				Error:            "invite_rejected",
				ErrorDescription: "invite is invalid, expired, or issued to a different email",
			})
		default:
			c.JSON(http.StatusBadRequest, models.ErrorResponse{
				Error:            "registration_failed",
				ErrorDescription: err.Error(),
			})
		}
		return
	}

	c.JSON(http.StatusCreated, tokenResponse)
}

// RefreshTokens rotates refresh token and returns new JWT pair.
// @Summary      Refresh tokens
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        body  body      models.RefreshRequest  true  "Refresh token"
// @Success      200   {object}  models.TokenResponse
// @Failure      400   {object}  models.ErrorResponse
// @Failure      401   {object}  models.ErrorResponse
// @Router       /auth/refresh [post]
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

// Logout revokes access token and refresh token family.
// @Summary      Logout user
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        body  body      models.LogoutRequest  false  "Optional refresh token to revoke family"
// @Security     BearerAuth
// @Success      204
// @Failure      400   {object}  models.ErrorResponse
// @Failure      500   {object}  models.ErrorResponse
// @Router       /auth/logout [post]
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

// GetCurrentUser retrieves the current authenticated user's info.
// @Summary      Get current user
// @Tags         users
// @Produce      json
// @Security     BearerAuth
// @Success      200   {object}  models.UserInfo
// @Failure      401   {object}  models.ErrorResponse
// @Router       /users/me [get]
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

	org := &models.OrganizationInfo{
		ID:   claims.OrganizationID,
		Slug: claims.OrganizationSlug,
		Role: claims.OrganizationRole,
	}

	// The organization's display name is not on the token — the claims carry
	// id, slug and role — so it is read here rather than added to the JWT.
	// Putting it on the token would grow every request and, worse, would only
	// reach sessions that logged in again; this works for the ones already
	// open. The frontend showed a hardcoded "Organização" for want of it.
	//
	// A failed lookup is not worth failing the request over: the client falls
	// back to a generic label, which is exactly the old behaviour.
	if claims.OrganizationSlug != "" {
		if found, err := h.authService.GetOrganizationBySlug(c.Request.Context(), claims.OrganizationSlug); err != nil {
			utils.Warn("auth handler: could not resolve the organization name",
				"slug", claims.OrganizationSlug, "error", err)
		} else if found != nil {
			org.Name = found.Name
		}
	}

	c.JSON(http.StatusOK, models.UserInfo{
		ID:           userID,
		Email:        claims.Email,
		FullName:     claims.FullName,
		Role:         claims.Role,
		Organization: org,
	})
}

// OAuthLogin initiates OAuth authentication flow.
// @Summary      Initiate OAuth flow
// @Tags         auth
// @Param        provider  path      string  true  "OAuth provider (github, gitlab)"
// @Param        organization_name  query  string  false  "Organization name for onboarding without an existing organization"
// @Param        organization_slug  query  string  false  "Optional organization slug for onboarding"
// @Param        invite_token       query  string  false  "Invite token when joining an existing organization"
// @Success      307
// @Failure      400  {object}  models.ErrorResponse
// @Failure      500  {object}  models.ErrorResponse
// @Router       /auth/{provider} [get]
func (h *AuthHandler) OAuthLogin(c *gin.Context) {
	provider := c.Param("provider")
	orgSlug := c.Param("slug")

	stateInput := services.OAuthStateInput{InviteToken: c.Query("invite_token")}
	if orgSlug != "" {
		org, err := h.authService.GetOrganizationBySlug(c.Request.Context(), orgSlug)
		if err != nil {
			c.JSON(http.StatusBadRequest, models.ErrorResponse{
				Error:            "invalid_organization",
				ErrorDescription: err.Error(),
			})
			return
		}
		stateInput.OrganizationID = org.ID
	} else {
		stateInput.OrganizationName = c.Query("organization_name")
		stateInput.OrganizationSlug = c.Query("organization_slug")
		if stateInput.OrganizationName == "" && stateInput.InviteToken == "" {
			c.JSON(http.StatusBadRequest, models.ErrorResponse{
				Error:            "invalid_request",
				ErrorDescription: "organization_name is required unless an invite_token is provided",
			})
			return
		}
	}

	state, err := h.authService.GenerateOAuthState(stateInput)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:            "oauth_error",
			ErrorDescription: "failed to generate state: " + err.Error(),
		})
		return
	}

	authURL := h.authService.GetOAuthAuthURL(c.Request.Context(), orgSlug, provider, state)
	if authURL == "" {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:            "invalid_provider",
			ErrorDescription: "unknown oauth provider: " + provider,
		})
		return
	}

	c.Redirect(http.StatusTemporaryRedirect, authURL)
}

// OAuthCallback handles OAuth provider callback.
// @Summary      OAuth callback
// @Tags         auth
// @Produce      json
// @Param        provider  path      string  true  "OAuth provider (github, gitlab)"
// @Param        code      query     string  true  "Authorization code"
// @Param        state     query     string  true  "State token for CSRF validation"
// @Success      200       {object}  models.TokenResponse
// @Failure      400       {object}  models.ErrorResponse
// @Failure      401       {object}  models.ErrorResponse
// @Router       /auth/{provider}/callback [get]
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

	tokenResponse, err := h.authService.LoginWithOAuth(c.Request.Context(), c.Param("slug"), provider, code, state)
	if err != nil {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{
			Error:            "oauth_authentication_failed",
			ErrorDescription: err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, tokenResponse)
}
