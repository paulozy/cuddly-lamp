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

	resp, err := h.repoService.CreateRepository(c.Request.Context(), userID, req)
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

func (h *RepositoryHandler) GetRepository(c *gin.Context) {
	id := c.Param("id")

	userID, err := utils.GetUserIDFromContext(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{
			Error:            "unauthorized",
			ErrorDescription: "missing or invalid authentication",
		})
		return
	}

	resp, err := h.repoService.GetRepository(c.Request.Context(), id, userID)
	if err != nil {
		repoErrToJSON(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

func (h *RepositoryHandler) ListRepositories(c *gin.Context) {
	userID, err := utils.GetUserIDFromContext(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{
			Error:            "unauthorized",
			ErrorDescription: "missing or invalid authentication",
		})
		return
	}

	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))

	resp, err := h.repoService.ListRepositories(c.Request.Context(), userID, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:            "list_failed",
			ErrorDescription: err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, resp)
}

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

	userID, err := utils.GetUserIDFromContext(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{
			Error:            "unauthorized",
			ErrorDescription: "missing or invalid authentication",
		})
		return
	}

	resp, err := h.repoService.UpdateRepository(c.Request.Context(), id, userID, req)
	if err != nil {
		repoErrToJSON(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

func (h *RepositoryHandler) DeleteRepository(c *gin.Context) {
	id := c.Param("id")

	userID, err := utils.GetUserIDFromContext(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{
			Error:            "unauthorized",
			ErrorDescription: "missing or invalid authentication",
		})
		return
	}

	if err := h.repoService.DeleteRepository(c.Request.Context(), id, userID); err != nil {
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
