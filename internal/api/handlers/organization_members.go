package handlers

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/services"
	"github.com/paulozy/idp-with-ai-backend/internal/utils"
)

// OrganizationMemberHandler serves the organization's member list and its
// invites. Every route here is mounted behind RequireRole(admin).
type OrganizationMemberHandler struct {
	membership *services.MembershipService
}

func NewOrganizationMemberHandler(membership *services.MembershipService) *OrganizationMemberHandler {
	return &OrganizationMemberHandler{membership: membership}
}

// ListMembers returns everyone in the caller's organization.
// @Summary      List organization members
// @Tags         organization
// @Produce      json
// @Security     BearerAuth
// @Success      200 {object} models.MemberListResponse
// @Failure      401 {object} models.ErrorResponse
// @Failure      403 {object} models.ErrorResponse
// @Router       /organizations/members [get]
func (h *OrganizationMemberHandler) ListMembers(c *gin.Context) {
	orgID, ok := requireOrganization(c)
	if !ok {
		return
	}
	members, err := h.membership.ListMembers(c.Request.Context(), orgID)
	if err != nil {
		internalError(c, "failed to list organization members")
		return
	}
	c.JSON(http.StatusOK, models.MemberListResponse{Items: members, Total: len(members)})
}

// UpdateMemberRole changes a member's role within the organization.
// @Summary      Update member role
// @Tags         organization
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        userID path string true "User ID"
// @Param        body body models.UpdateMemberRoleRequest true "Role"
// @Success      204
// @Failure      400 {object} models.ErrorResponse
// @Failure      403 {object} models.ErrorResponse
// @Failure      404 {object} models.ErrorResponse
// @Failure      409 {object} models.ErrorResponse
// @Router       /organizations/members/{userID} [patch]
func (h *OrganizationMemberHandler) UpdateMemberRole(c *gin.Context) {
	orgID, ok := requireOrganization(c)
	if !ok {
		return
	}
	actingUserID, err := utils.GetUserIDFromContext(c)
	if err != nil {
		unauthorized(c)
		return
	}

	var req models.UpdateMemberRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: err.Error(),
		})
		return
	}

	err = h.membership.UpdateMemberRole(c.Request.Context(), orgID, actingUserID, c.Param("userID"), req.Role)
	if handleMembershipError(c, err) {
		return
	}
	c.Status(http.StatusNoContent)
}

// RemoveMember removes a member from the organization.
// @Summary      Remove organization member
// @Tags         organization
// @Produce      json
// @Security     BearerAuth
// @Param        userID path string true "User ID"
// @Success      204
// @Failure      403 {object} models.ErrorResponse
// @Failure      404 {object} models.ErrorResponse
// @Failure      409 {object} models.ErrorResponse
// @Router       /organizations/members/{userID} [delete]
func (h *OrganizationMemberHandler) RemoveMember(c *gin.Context) {
	orgID, ok := requireOrganization(c)
	if !ok {
		return
	}
	actingUserID, err := utils.GetUserIDFromContext(c)
	if err != nil {
		unauthorized(c)
		return
	}

	err = h.membership.RemoveMember(c.Request.Context(), orgID, actingUserID, c.Param("userID"))
	if handleMembershipError(c, err) {
		return
	}
	c.Status(http.StatusNoContent)
}

// CreateInvite mints an invite and returns its token exactly once.
// @Summary      Invite someone to the organization
// @Tags         organization
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body body models.CreateInviteRequest true "Invite"
// @Success      201 {object} models.CreateInviteResponse
// @Failure      400 {object} models.ErrorResponse
// @Failure      403 {object} models.ErrorResponse
// @Failure      409 {object} models.ErrorResponse
// @Router       /organizations/invites [post]
func (h *OrganizationMemberHandler) CreateInvite(c *gin.Context) {
	orgID, ok := requireOrganization(c)
	if !ok {
		return
	}
	actingUserID, _ := utils.GetUserIDFromContext(c)

	var req models.CreateInviteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: err.Error(),
		})
		return
	}

	invite, token, err := h.membership.CreateInvite(c.Request.Context(), orgID, actingUserID, req)
	if handleMembershipError(c, err) {
		return
	}

	// The plaintext token appears here and nowhere else, ever.
	c.JSON(http.StatusCreated, models.CreateInviteResponse{
		Invite: models.OrganizationInviteToResponse(invite, time.Now().UTC()),
		Token:  token,
	})
}

// ListInvites lists the organization's invites and their status.
// @Summary      List organization invites
// @Tags         organization
// @Produce      json
// @Security     BearerAuth
// @Success      200 {object} models.InviteListResponse
// @Failure      403 {object} models.ErrorResponse
// @Router       /organizations/invites [get]
func (h *OrganizationMemberHandler) ListInvites(c *gin.Context) {
	orgID, ok := requireOrganization(c)
	if !ok {
		return
	}
	invites, err := h.membership.ListInvites(c.Request.Context(), orgID)
	if err != nil {
		internalError(c, "failed to list organization invites")
		return
	}
	now := time.Now().UTC()
	items := make([]models.InviteResponse, 0, len(invites))
	for i := range invites {
		items = append(items, models.OrganizationInviteToResponse(&invites[i], now))
	}
	c.JSON(http.StatusOK, models.InviteListResponse{Items: items, Total: len(items)})
}

// RevokeInvite invalidates a pending invite.
// @Summary      Revoke an organization invite
// @Tags         organization
// @Produce      json
// @Security     BearerAuth
// @Param        id path string true "Invite ID"
// @Success      204
// @Failure      403 {object} models.ErrorResponse
// @Failure      404 {object} models.ErrorResponse
// @Router       /organizations/invites/{id} [delete]
func (h *OrganizationMemberHandler) RevokeInvite(c *gin.Context) {
	orgID, ok := requireOrganization(c)
	if !ok {
		return
	}
	err := h.membership.RevokeInvite(c.Request.Context(), orgID, c.Param("id"))
	if handleMembershipError(c, err) {
		return
	}
	c.Status(http.StatusNoContent)
}

// ── shared helpers ───────────────────────────────────────────────────────────

func requireOrganization(c *gin.Context) (string, bool) {
	orgID, err := utils.GetOrganizationIDFromContext(c)
	if err != nil || orgID == "" {
		unauthorized(c)
		return "", false
	}
	return orgID, true
}

func unauthorized(c *gin.Context) {
	c.JSON(http.StatusUnauthorized, models.ErrorResponse{
		Error:            "unauthorized",
		ErrorDescription: "missing or invalid authentication",
	})
}

func internalError(c *gin.Context, description string) {
	c.JSON(http.StatusInternalServerError, models.ErrorResponse{
		Error:            "internal_error",
		ErrorDescription: description,
	})
}

// handleMembershipError maps service errors to responses and reports whether it
// wrote one. Keeping the mapping in one place means every membership route
// answers the same way for the same condition.
func handleMembershipError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, services.ErrInviteInvalidEmail):
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:            "invalid_email",
			ErrorDescription: "a valid email address is required",
		})
	case errors.Is(err, services.ErrInviteInvalidRole):
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:            "invalid_role",
			ErrorDescription: "role must be one of viewer, developer, maintainer, admin",
		})
	case errors.Is(err, services.ErrInviteAlreadyMember):
		c.JSON(http.StatusConflict, models.ErrorResponse{
			Error:            "already_member",
			ErrorDescription: "that address already belongs to this organization",
		})
	case errors.Is(err, services.ErrLastAdmin):
		c.JSON(http.StatusConflict, models.ErrorResponse{
			Error:            "last_admin",
			ErrorDescription: "the organization must keep at least one admin",
		})
	case errors.Is(err, services.ErrSelfDemotion):
		c.JSON(http.StatusConflict, models.ErrorResponse{
			Error:            "self_change_not_allowed",
			ErrorDescription: "you cannot change or remove your own membership",
		})
	case errors.Is(err, services.ErrInviteNotFound):
		c.JSON(http.StatusNotFound, models.ErrorResponse{
			Error:            "not_found",
			ErrorDescription: "invite not found",
		})
	case errors.Is(err, services.ErrMemberNotFound):
		c.JSON(http.StatusNotFound, models.ErrorResponse{
			Error:            "not_found",
			ErrorDescription: "member not found",
		})
	default:
		internalError(c, "membership operation failed")
	}
	return true
}
