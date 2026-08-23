package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/services"
	"github.com/paulozy/idp-with-ai-backend/internal/utils"
)

// TeamHandler serves the organization's teams. Reads are open to any member so
// everyone can see who owns what; mutations are gated at maintainer by the
// router.
type TeamHandler struct {
	teams *services.TeamService
}

func NewTeamHandler(teams *services.TeamService) *TeamHandler {
	return &TeamHandler{teams: teams}
}

// ListTeams lists the organization's teams with member and repository counts.
// @Summary      List teams
// @Tags         teams
// @Produce      json
// @Security     BearerAuth
// @Success      200 {object} models.TeamListResponse
// @Router       /teams [get]
func (h *TeamHandler) ListTeams(c *gin.Context) {
	orgID, ok := requireOrganization(c)
	if !ok {
		return
	}
	teams, err := h.teams.ListTeams(c.Request.Context(), orgID, actorFromContext(c).UserID)
	if err != nil {
		internalError(c, "failed to list teams")
		return
	}
	items := make([]models.TeamResponse, 0, len(teams))
	for i := range teams {
		items = append(items, models.TeamToResponse(&teams[i]))
	}
	c.JSON(http.StatusOK, models.TeamListResponse{Items: items, Total: len(items)})
}

// CreateTeam creates a team; the creator joins it as lead.
// @Summary      Create a team
// @Tags         teams
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body body models.CreateTeamRequest true "Team"
// @Success      201 {object} models.TeamResponse
// @Failure      409 {object} models.ErrorResponse
// @Router       /teams [post]
func (h *TeamHandler) CreateTeam(c *gin.Context) {
	orgID, ok := requireOrganization(c)
	if !ok {
		return
	}
	actor := actorFromContext(c)

	var req models.CreateTeamRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: err.Error(),
		})
		return
	}

	team, err := h.teams.CreateTeam(c.Request.Context(), orgID, actor.UserID, req)
	if handleTeamError(c, err) {
		return
	}
	c.JSON(http.StatusCreated, models.TeamToResponse(team))
}

// GetTeam returns a single team.
// @Summary      Get a team
// @Tags         teams
// @Produce      json
// @Security     BearerAuth
// @Param        id path string true "Team ID"
// @Success      200 {object} models.TeamResponse
// @Failure      404 {object} models.ErrorResponse
// @Router       /teams/{id} [get]
func (h *TeamHandler) GetTeam(c *gin.Context) {
	orgID, ok := requireOrganization(c)
	if !ok {
		return
	}
	team, err := h.teams.GetTeam(c.Request.Context(), orgID, c.Param("id"))
	if handleTeamError(c, err) {
		return
	}
	c.JSON(http.StatusOK, models.TeamToResponse(team))
}

// UpdateTeam edits a team's name or description. The slug is immutable.
// @Summary      Update a team
// @Tags         teams
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id path string true "Team ID"
// @Param        body body models.UpdateTeamRequest true "Changes"
// @Success      200 {object} models.TeamResponse
// @Router       /teams/{id} [patch]
func (h *TeamHandler) UpdateTeam(c *gin.Context) {
	orgID, ok := requireOrganization(c)
	if !ok {
		return
	}
	var req models.UpdateTeamRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: err.Error(),
		})
		return
	}
	team, err := h.teams.UpdateTeam(c.Request.Context(), orgID, c.Param("id"), req)
	if handleTeamError(c, err) {
		return
	}
	c.JSON(http.StatusOK, models.TeamToResponse(team))
}

// DeleteTeam soft-deletes a team and releases the repositories it owned.
// @Summary      Delete a team
// @Tags         teams
// @Produce      json
// @Security     BearerAuth
// @Param        id path string true "Team ID"
// @Success      204
// @Router       /teams/{id} [delete]
func (h *TeamHandler) DeleteTeam(c *gin.Context) {
	orgID, ok := requireOrganization(c)
	if !ok {
		return
	}
	if handleTeamError(c, h.teams.DeleteTeam(c.Request.Context(), orgID, c.Param("id"))) {
		return
	}
	c.Status(http.StatusNoContent)
}

// ListMembers lists a team's members.
// @Summary      List team members
// @Tags         teams
// @Produce      json
// @Security     BearerAuth
// @Param        id path string true "Team ID"
// @Success      200 {object} models.TeamMemberListResponse
// @Router       /teams/{id}/members [get]
func (h *TeamHandler) ListMembers(c *gin.Context) {
	orgID, ok := requireOrganization(c)
	if !ok {
		return
	}
	members, err := h.teams.ListMembers(c.Request.Context(), orgID, c.Param("id"))
	if handleTeamError(c, err) {
		return
	}
	c.JSON(http.StatusOK, models.TeamMemberListResponse{Items: members, Total: len(members)})
}

// AddMember adds an organization member to a team.
// @Summary      Add a team member
// @Tags         teams
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id path string true "Team ID"
// @Param        body body models.AddTeamMemberRequest true "Member"
// @Success      204
// @Failure      409 {object} models.ErrorResponse
// @Router       /teams/{id}/members [post]
func (h *TeamHandler) AddMember(c *gin.Context) {
	orgID, ok := requireOrganization(c)
	if !ok {
		return
	}
	var req models.AddTeamMemberRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: err.Error(),
		})
		return
	}
	if handleTeamError(c, h.teams.AddMember(c.Request.Context(), orgID, c.Param("id"), req)) {
		return
	}
	c.Status(http.StatusNoContent)
}

// RemoveMember removes someone from a team.
// @Summary      Remove a team member
// @Tags         teams
// @Produce      json
// @Security     BearerAuth
// @Param        id path string true "Team ID"
// @Param        userID path string true "User ID"
// @Success      204
// @Router       /teams/{id}/members/{userID} [delete]
func (h *TeamHandler) RemoveMember(c *gin.Context) {
	orgID, ok := requireOrganization(c)
	if !ok {
		return
	}
	if handleTeamError(c, h.teams.RemoveMember(c.Request.Context(), orgID, c.Param("id"), c.Param("userID"))) {
		return
	}
	c.Status(http.StatusNoContent)
}

// SetRepositoryOwner assigns or clears a repository's owning team.
// @Summary      Set repository owner team
// @Tags         repositories
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id path string true "Repository ID"
// @Param        body body models.SetRepositoryOwnerRequest true "Owner (null team_id clears it)"
// @Success      204
// @Router       /repositories/{id}/owner [put]
func (h *TeamHandler) SetRepositoryOwner(c *gin.Context) {
	orgID, ok := requireOrganization(c)
	if !ok {
		return
	}
	var req models.SetRepositoryOwnerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: err.Error(),
		})
		return
	}
	err := h.teams.SetRepositoryOwner(c.Request.Context(), orgID, c.Param("id"), req.TeamID)
	if handleTeamError(c, err) {
		return
	}
	c.Status(http.StatusNoContent)
}

func handleTeamError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, services.ErrTeamInvalid):
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: "team name is required",
		})
	case errors.Is(err, services.ErrTeamSlugTaken):
		c.JSON(http.StatusConflict, models.ErrorResponse{
			Error:            "team_already_exists",
			ErrorDescription: "a team with this name already exists in the organization",
		})
	case errors.Is(err, services.ErrTeamNotLocal):
		c.JSON(http.StatusConflict, models.ErrorResponse{
			Error:            "team_not_local",
			ErrorDescription: "membership of an imported team is managed by its provider",
		})
	case errors.Is(err, services.ErrUserNotInOrganization):
		c.JSON(http.StatusConflict, models.ErrorResponse{
			Error:            "not_organization_member",
			ErrorDescription: "that user does not belong to this organization",
		})
	case errors.Is(err, services.ErrTeamNotFound):
		c.JSON(http.StatusNotFound, models.ErrorResponse{
			Error:            "not_found",
			ErrorDescription: "team not found",
		})
	case errors.Is(err, services.ErrRepositoryNotFound):
		c.JSON(http.StatusNotFound, models.ErrorResponse{
			Error:            "not_found",
			ErrorDescription: "repository not found",
		})
	default:
		// Log the cause before collapsing it into a generic 500. Without this
		// the only signal a failing team operation gives is "team operation
		// failed", which says nothing about what actually broke.
		utils.Error("team: unexpected failure",
			"path", c.FullPath(), "method", c.Request.Method, "error", err)
		internalError(c, "team operation failed")
	}
	return true
}
