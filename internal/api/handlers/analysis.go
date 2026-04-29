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

type AnalysisHandler struct {
	repo     storage.Repository
	enqueuer jobs.Enqueuer
}

func NewAnalysisHandler(repo storage.Repository, enqueuer jobs.Enqueuer) *AnalysisHandler {
	return &AnalysisHandler{
		repo:     repo,
		enqueuer: enqueuer,
	}
}

// AnalyzeRepository triggers code analysis for a repository
// @Summary      Analyze repository
// @Tags         analysis
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id       path      string                          true   "Repository ID"
// @Param        body     body      models.AnalyzeRepositoryRequest false  "Analysis options"
// @Success      202      {object}  models.JobResponse
// @Failure      400      {object}  models.ErrorResponse
// @Failure      401      {object}  models.ErrorResponse
// @Failure      404      {object}  models.ErrorResponse
// @Router       /repositories/{id}/analyze [post]
func (h *AnalysisHandler) AnalyzeRepository(c *gin.Context) {
	repoID := c.Param("id")
	if repoID == "" {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: "repository id is required",
		})
		return
	}

	// Verify repository exists
	repo, err := h.repo.GetRepository(c.Request.Context(), repoID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:            "internal_error",
			ErrorDescription: "failed to fetch repository",
		})
		return
	}

	if repo == nil {
		c.JSON(http.StatusNotFound, models.ErrorResponse{
			Error:            "not_found",
			ErrorDescription: "repository not found",
		})
		return
	}

	// Parse optional request body
	var req models.AnalyzeRepositoryRequest
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, models.ErrorResponse{
				Error:            "invalid_request",
				ErrorDescription: err.Error(),
			})
			return
		}
	}

	// Set defaults
	if req.Type == "" {
		req.Type = "code_review"
	}

	// Enqueue analysis job with deduplication: manual triggers get a deterministic TaskID
	payload := tasks.AnalyzeRepoPayload{
		RepositoryID: repoID,
		Branch:       req.Branch,
		CommitSHA:    req.CommitSHA,
		Type:         req.Type,
		TriggeredBy:  "user",
	}

	// Use TaskID to prevent duplicate manual triggers (one per repo at a time)
	taskID := fmt.Sprintf("analyze:manual:%s", repoID)
	err = h.enqueuer.Enqueue(c.Request.Context(), tasks.TypeAnalyzeRepo, payload,
		asynq.TaskID(taskID),
		asynq.Retention(10*time.Minute), // keep record for 10m after completion so ID stays locked
	)
	if err != nil {
		if errors.Is(err, asynq.ErrTaskIDConflict) {
			c.JSON(http.StatusConflict, models.ErrorResponse{
				Error:            "analysis_in_progress",
				ErrorDescription: "an analysis for this repository is already queued or running",
			})
			return
		}
		utils.Error("analysis handler: enqueue failed", "repo_id", repoID, "error", err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:            "queue_error",
			ErrorDescription: "failed to enqueue analysis job",
		})
		return
	}

	c.JSON(http.StatusAccepted, models.JobResponse{
		Status: "queued",
		Type:   tasks.TypeAnalyzeRepo,
		Target: repoID,
	})
}

// ListAnalyses retrieves all analyses for a repository
// @Summary      List repository analyses
// @Tags         analysis
// @Produce      json
// @Security     BearerAuth
// @Param        id       path      string false "Repository ID"
// @Param        limit    query     int    false "Result limit (default 20)"
// @Param        offset   query     int    false "Result offset (default 0)"
// @Success      200      {object}  models.AnalysisListResponse
// @Failure      401      {object}  models.ErrorResponse
// @Failure      404      {object}  models.ErrorResponse
// @Router       /repositories/{id}/analyses [get]
func (h *AnalysisHandler) ListAnalyses(c *gin.Context) {
	repoID := c.Param("id")
	if repoID == "" {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: "repository id is required",
		})
		return
	}

	// Verify repository exists
	repo, err := h.repo.GetRepository(c.Request.Context(), repoID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:            "internal_error",
			ErrorDescription: "failed to fetch repository",
		})
		return
	}

	if repo == nil {
		c.JSON(http.StatusNotFound, models.ErrorResponse{
			Error:            "not_found",
			ErrorDescription: "repository not found",
		})
		return
	}

	// Fetch analyses (TODO: implement GetAnalysesByRepository in storage)
	// For now, return empty list
	c.JSON(http.StatusOK, models.AnalysisListResponse{
		Total:    0,
		Analyses: []models.CodeAnalysis{},
	})
}
