package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/services"
	"github.com/paulozy/idp-with-ai-backend/internal/utils"
)

type RepositoryRelationshipHandler struct {
	service *services.RepositoryRelationshipService
}

func NewRepositoryRelationshipHandler(service *services.RepositoryRelationshipService) *RepositoryRelationshipHandler {
	return &RepositoryRelationshipHandler{service: service}
}

func (h *RepositoryRelationshipHandler) GetGraph(c *gin.Context) {
	orgID, ok := organizationIDFromContext(c)
	if !ok {
		return
	}

	filter := services.RepositoryGraphFilter{
		RepositoryID:    c.Query("repository_id"),
		Kind:            models.RepositoryRelationshipKind(c.Query("kind")),
		Source:          models.RepositoryRelationshipSource(c.Query("source")),
		IncludeMetadata: c.DefaultQuery("include_metadata", "true") != "false",
	}
	resp, err := h.service.GetGraph(c.Request.Context(), orgID, filter)
	if err != nil {
		relationshipErrToJSON(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (h *RepositoryRelationshipHandler) CreateRelationship(c *gin.Context) {
	orgID, ok := organizationIDFromContext(c)
	if !ok {
		return
	}
	userID, err := utils.GetUserIDFromContext(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{
			Error:            "unauthorized",
			ErrorDescription: "missing or invalid authentication",
		})
		return
	}

	var req models.CreateRepositoryRelationshipRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: err.Error(),
		})
		return
	}
	resp, err := h.service.CreateRelationship(c.Request.Context(), orgID, userID, req)
	if err != nil {
		relationshipErrToJSON(c, err)
		return
	}
	c.JSON(http.StatusCreated, resp)
}

func (h *RepositoryRelationshipHandler) UpdateRelationship(c *gin.Context) {
	orgID, ok := organizationIDFromContext(c)
	if !ok {
		return
	}

	var req models.UpdateRepositoryRelationshipRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: err.Error(),
		})
		return
	}
	resp, err := h.service.UpdateRelationship(c.Request.Context(), orgID, c.Param("id"), req)
	if err != nil {
		relationshipErrToJSON(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (h *RepositoryRelationshipHandler) DeleteRelationship(c *gin.Context) {
	orgID, ok := organizationIDFromContext(c)
	if !ok {
		return
	}
	if err := h.service.DeleteRelationship(c.Request.Context(), orgID, c.Param("id")); err != nil {
		relationshipErrToJSON(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func organizationIDFromContext(c *gin.Context) (string, bool) {
	orgID, err := utils.GetOrganizationIDFromContext(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{
			Error:            "unauthorized",
			ErrorDescription: "missing or invalid organization",
		})
		return "", false
	}
	return orgID, true
}

func relationshipErrToJSON(c *gin.Context, err error) {
	switch {
	case errors.Is(err, services.ErrRepositoryNotFound):
		c.JSON(http.StatusNotFound, models.ErrorResponse{
			Error:            "repository_not_found",
			ErrorDescription: "repository not found",
		})
	case errors.Is(err, services.ErrRepositoryRelationshipNotFound):
		c.JSON(http.StatusNotFound, models.ErrorResponse{
			Error:            "relationship_not_found",
			ErrorDescription: "repository relationship not found",
		})
	case errors.Is(err, services.ErrForbidden):
		c.JSON(http.StatusForbidden, models.ErrorResponse{
			Error:            "forbidden",
			ErrorDescription: "you do not have access to this relationship",
		})
	case errors.Is(err, services.ErrInvalidRepositoryRelationship):
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:            "invalid_relationship",
			ErrorDescription: "invalid repository relationship",
		})
	default:
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:            "internal_error",
			ErrorDescription: err.Error(),
		})
	}
}
