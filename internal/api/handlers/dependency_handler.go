package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hibiken/asynq"
	"github.com/paulozy/idp-with-ai-backend/internal/jobs"
	"github.com/paulozy/idp-with-ai-backend/internal/jobs/tasks"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
	"github.com/paulozy/idp-with-ai-backend/internal/utils"
)

type DependencyHandler struct {
	repo     storage.Repository
	enqueuer jobs.Enqueuer
}

func NewDependencyHandler(repo storage.Repository, enqueuer jobs.Enqueuer) *DependencyHandler {
	return &DependencyHandler{repo: repo, enqueuer: enqueuer}
}

func (h *DependencyHandler) ScanDependencies(c *gin.Context) {
	repoID := c.Param("id")
	repository, ok := h.fetchAccessibleRepository(c, repoID)
	if !ok {
		return
	}

	payload := tasks.ScanDependenciesPayload{
		RepositoryID: repoID,
		Branch:       repository.Metadata.DefaultBranch,
		TriggeredBy:  "user",
	}
	if payload.Branch == "" {
		payload.Branch = "main"
	}

	taskID := fmt.Sprintf("dependency:scan:manual:%s", repoID)
	err := h.enqueuer.Enqueue(c.Request.Context(), tasks.TypeScanDependencies, payload,
		asynq.TaskID(taskID),
		asynq.Retention(10*time.Minute),
	)
	if err != nil {
		if errors.Is(err, asynq.ErrTaskIDConflict) {
			c.JSON(http.StatusConflict, models.ErrorResponse{
				Error:            "dependency_scan_in_progress",
				ErrorDescription: "a dependency scan for this repository is already queued or running",
			})
			return
		}
		utils.Error("dependency handler: enqueue failed", "repo_id", repoID, "error", err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:            "queue_error",
			ErrorDescription: "failed to enqueue dependency scan",
		})
		return
	}

	c.JSON(http.StatusAccepted, models.JobResponse{
		Status: "queued",
		Type:   tasks.TypeScanDependencies,
		Target: repoID,
	})
}

func (h *DependencyHandler) ListDependencies(c *gin.Context) {
	repoID := c.Param("id")
	if _, ok := h.fetchAccessibleRepository(c, repoID); !ok {
		return
	}

	onlyVulnerable := c.Query("vulnerable") == "true"
	deps, err := h.repo.ListPackageDependencies(c.Request.Context(), repoID, onlyVulnerable)
	if err != nil {
		utils.Error("dependency handler: list failed", "repo_id", repoID, "error", err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:            "internal_error",
			ErrorDescription: "failed to list dependencies",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"total":        len(deps),
		"dependencies": deps,
	})
}

func (h *DependencyHandler) fetchAccessibleRepository(c *gin.Context, repoID string) (*models.Repository, bool) {
	if repoID == "" {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: "repository id is required",
		})
		return nil, false
	}
	repository, err := h.repo.GetRepository(c.Request.Context(), repoID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:            "internal_error",
			ErrorDescription: "failed to fetch repository",
		})
		return nil, false
	}
	if repository == nil {
		c.JSON(http.StatusNotFound, models.ErrorResponse{
			Error:            "not_found",
			ErrorDescription: "repository not found",
		})
		return nil, false
	}
	orgID, err := utils.GetOrganizationIDFromContext(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{
			Error:            "unauthorized",
			ErrorDescription: "missing or invalid authentication",
		})
		return nil, false
	}
	if repository.OrganizationID != orgID {
		c.JSON(http.StatusForbidden, models.ErrorResponse{
			Error:            "forbidden",
			ErrorDescription: "you do not have access to this repository",
		})
		return nil, false
	}
	return repository, true
}
