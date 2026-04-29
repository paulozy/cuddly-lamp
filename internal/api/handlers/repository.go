package handlers

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/services"
	"github.com/paulozy/idp-with-ai-backend/internal/utils"
)

type RepositoryHandler struct {
	repoService *services.RepositoryService
}

func NewRepositoryHandler(repoService *services.RepositoryService) *RepositoryHandler {
	return &RepositoryHandler{repoService: repoService}
}

// CreateRepository creates a new repository and triggers initial sync.
// @Summary      Create repository
// @Tags         repositories
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body      models.CreateRepositoryRequest  true  "Repository details"
// @Success      201   {object}  models.RepositoryResponse
// @Failure      400   {object}  models.ErrorResponse
// @Failure      401   {object}  models.ErrorResponse
// @Failure      409   {object}  models.ErrorResponse  "Repository already exists"
// @Router       /repositories [post]
func (h *RepositoryHandler) CreateRepository(c *gin.Context) {
	var req models.CreateRepositoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: err.Error(),
		})
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
	orgID, err := utils.GetOrganizationIDFromContext(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{
			Error:            "unauthorized",
			ErrorDescription: "missing or invalid organization",
		})
		return
	}

	resp, err := h.repoService.CreateRepository(c.Request.Context(), orgID, userID, req)
	if err != nil {
		if errors.Is(err, services.ErrRepositoryAlreadyExists) {
			c.JSON(http.StatusConflict, models.ErrorResponse{
				Error:            "repository_exists",
				ErrorDescription: "a repository with this URL already exists",
			})
			return
		}
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:            "create_failed",
			ErrorDescription: err.Error(),
		})
		return
	}

	c.JSON(http.StatusCreated, resp)
}

// GetRepository retrieves a single repository by ID.
// @Summary      Get repository
// @Tags         repositories
// @Produce      json
// @Security     BearerAuth
// @Param        id  path      string  true  "Repository ID"
// @Success      200   {object}  models.RepositoryResponse
// @Failure      401   {object}  models.ErrorResponse
// @Failure      403   {object}  models.ErrorResponse  "Access denied"
// @Failure      404   {object}  models.ErrorResponse
// @Router       /repositories/{id} [get]
func (h *RepositoryHandler) GetRepository(c *gin.Context) {
	id := c.Param("id")

	orgID, err := utils.GetOrganizationIDFromContext(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{
			Error:            "unauthorized",
			ErrorDescription: "missing or invalid authentication",
		})
		return
	}

	resp, err := h.repoService.GetRepository(c.Request.Context(), id, orgID)
	if err != nil {
		repoErrToJSON(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// ListRepositories lists all repositories for the authenticated user.
// @Summary      List repositories
// @Tags         repositories
// @Produce      json
// @Security     BearerAuth
// @Param        limit   query     int  false  "Items per page"  default(20)
// @Param        offset  query     int  false  "Pagination offset"  default(0)
// @Success      200     {object}  models.RepositoryListResponse
// @Failure      401     {object}  models.ErrorResponse
// @Failure      500     {object}  models.ErrorResponse
// @Router       /repositories [get]
func (h *RepositoryHandler) ListRepositories(c *gin.Context) {
	orgID, err := utils.GetOrganizationIDFromContext(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{
			Error:            "unauthorized",
			ErrorDescription: "missing or invalid authentication",
		})
		return
	}

	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))

	resp, err := h.repoService.ListRepositories(c.Request.Context(), orgID, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:            "list_failed",
			ErrorDescription: err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, resp)
}

// UpdateRepository updates repository details.
// @Summary      Update repository
// @Tags         repositories
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path      string                          true  "Repository ID"
// @Param        body  body      models.UpdateRepositoryRequest  true  "Updated repository details"
// @Success      200   {object}  models.RepositoryResponse
// @Failure      400   {object}  models.ErrorResponse
// @Failure      401   {object}  models.ErrorResponse
// @Failure      403   {object}  models.ErrorResponse  "Access denied"
// @Failure      404   {object}  models.ErrorResponse
// @Router       /repositories/{id} [put]
func (h *RepositoryHandler) UpdateRepository(c *gin.Context) {
	id := c.Param("id")

	var req models.UpdateRepositoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: err.Error(),
		})
		return
	}

	orgID, err := utils.GetOrganizationIDFromContext(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{
			Error:            "unauthorized",
			ErrorDescription: "missing or invalid authentication",
		})
		return
	}

	resp, err := h.repoService.UpdateRepository(c.Request.Context(), id, orgID, req)
	if err != nil {
		repoErrToJSON(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// DeleteRepository deletes a repository.
// @Summary      Delete repository
// @Tags         repositories
// @Security     BearerAuth
// @Param        id  path  string  true  "Repository ID"
// @Success      204
// @Failure      401   {object}  models.ErrorResponse
// @Failure      403   {object}  models.ErrorResponse  "Access denied"
// @Failure      404   {object}  models.ErrorResponse
// @Router       /repositories/{id} [delete]
func (h *RepositoryHandler) DeleteRepository(c *gin.Context) {
	id := c.Param("id")

	orgID, err := utils.GetOrganizationIDFromContext(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{
			Error:            "unauthorized",
			ErrorDescription: "missing or invalid authentication",
		})
		return
	}

	if err := h.repoService.DeleteRepository(c.Request.Context(), id, orgID); err != nil {
		repoErrToJSON(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}

func repoErrToJSON(c *gin.Context, err error) {
	switch {
	case errors.Is(err, services.ErrRepositoryNotFound):
		c.JSON(http.StatusNotFound, models.ErrorResponse{
			Error:            "not_found",
			ErrorDescription: "repository not found",
		})
	case errors.Is(err, services.ErrForbidden):
		c.JSON(http.StatusForbidden, models.ErrorResponse{
			Error:            "forbidden",
			ErrorDescription: "you do not have access to this repository",
		})
	default:
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:            "internal_error",
			ErrorDescription: err.Error(),
		})
	}
}
