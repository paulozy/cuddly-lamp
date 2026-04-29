package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hibiken/asynq"
	"github.com/paulozy/idp-with-ai-backend/internal/embeddings"
	"github.com/paulozy/idp-with-ai-backend/internal/jobs"
	"github.com/paulozy/idp-with-ai-backend/internal/jobs/tasks"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
	"github.com/paulozy/idp-with-ai-backend/internal/utils"
)

type AnalysisHandler struct {
	repo              storage.Repository
	enqueuer          jobs.Enqueuer
	tokenHourlyLimit  int64
	embeddingProvider embeddings.Provider
}

const defaultSemanticMinScore = 0.55

func NewAnalysisHandler(repo storage.Repository, enqueuer jobs.Enqueuer, tokenLimit int64, embeddingProvider embeddings.Provider) *AnalysisHandler {
	return &AnalysisHandler{
		repo:              repo,
		enqueuer:          enqueuer,
		tokenHourlyLimit:  tokenLimit,
		embeddingProvider: embeddingProvider,
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

	// Validate analysis type
	allowedTypes := map[string]bool{
		"code_review": true, "security": true, "architecture": true,
	}
	if !allowedTypes[req.Type] {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:            "invalid_analysis_type",
			ErrorDescription: "type must be one of: code_review, security, architecture",
		})
		return
	}

	// Check token budget
	used, err := h.repo.SumTokensUsedSince(c.Request.Context(), time.Now().UTC().Add(-time.Hour))
	if err == nil && used >= h.tokenHourlyLimit {
		c.JSON(http.StatusTooManyRequests, models.ErrorResponse{
			Error:            "rate_limit_exceeded",
			ErrorDescription: fmt.Sprintf("token budget exhausted (%d/%d tokens used in last hour)", used, h.tokenHourlyLimit),
		})
		return
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

// GenerateEmbeddings queues semantic index generation for a repository.
// @Summary      Generate repository embeddings
// @Tags         search
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id       path      string true  "Repository ID"
// @Param        body     body      models.GenerateEmbeddingsRequest false "Embedding options"
// @Success      202      {object}  models.JobResponse
// @Failure      400      {object}  models.ErrorResponse
// @Failure      401      {object}  models.ErrorResponse
// @Failure      404      {object}  models.ErrorResponse
// @Failure      503      {object}  models.ErrorResponse
// @Router       /repositories/{id}/embeddings [post]
func (h *AnalysisHandler) GenerateEmbeddings(c *gin.Context) {
	if h.embeddingProvider == nil {
		c.JSON(http.StatusServiceUnavailable, models.ErrorResponse{
			Error:            "embeddings_unavailable",
			ErrorDescription: "embedding provider is not configured",
		})
		return
	}

	repoID := c.Param("id")
	repository, ok := h.fetchAccessibleRepository(c, repoID)
	if !ok {
		return
	}

	var req models.GenerateEmbeddingsRequest
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, models.ErrorResponse{
				Error:            "invalid_request",
				ErrorDescription: err.Error(),
			})
			return
		}
	}
	if req.Branch == "" {
		req.Branch = repository.Metadata.DefaultBranch
	}
	if req.Branch == "" {
		req.Branch = "main"
	}

	payload := tasks.GenerateEmbeddingsPayload{
		RepositoryID: repoID,
		Branch:       req.Branch,
		CommitSHA:    req.CommitSHA,
	}
	taskID := fmt.Sprintf("embeddings:generate:%s:%s:%s:%d", repoID, req.Branch, h.embeddingProvider.Model(), h.embeddingProvider.Dimension())
	err := h.enqueuer.Enqueue(c.Request.Context(), tasks.TypeGenerateEmbeddings, payload,
		asynq.TaskID(taskID),
		asynq.Retention(10*time.Minute),
	)
	if err != nil {
		if errors.Is(err, asynq.ErrTaskIDConflict) {
			c.JSON(http.StatusConflict, models.ErrorResponse{
				Error:            "embeddings_in_progress",
				ErrorDescription: "embedding generation for this repository is already queued or running",
			})
			return
		}
		utils.Error("embeddings handler: enqueue failed", "repo_id", repoID, "error", err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:            "queue_error",
			ErrorDescription: "failed to enqueue embeddings job",
		})
		return
	}

	c.JSON(http.StatusAccepted, models.JobResponse{
		Status: "queued",
		Type:   tasks.TypeGenerateEmbeddings,
		Target: repoID,
	})
}

// SemanticSearch searches repository code embeddings.
// @Summary      Semantic repository search
// @Tags         search
// @Produce      json
// @Security     BearerAuth
// @Param        id       path      string true  "Repository ID"
// @Param        q        query     string true  "Search query"
// @Param        limit    query     int    false "Result limit"
// @Param        branch   query     string false "Branch"
// @Success      200      {object}  models.SemanticSearchResponse
// @Failure      400      {object}  models.ErrorResponse
// @Failure      401      {object}  models.ErrorResponse
// @Failure      404      {object}  models.ErrorResponse
// @Failure      503      {object}  models.ErrorResponse
// @Router       /repositories/{id}/search [get]
func (h *AnalysisHandler) SemanticSearch(c *gin.Context) {
	if h.embeddingProvider == nil {
		c.JSON(http.StatusServiceUnavailable, models.ErrorResponse{
			Error:            "embeddings_unavailable",
			ErrorDescription: "embedding provider is not configured",
		})
		return
	}

	repoID := c.Param("id")
	repository, ok := h.fetchAccessibleRepository(c, repoID)
	if !ok {
		return
	}

	query := strings.TrimSpace(c.Query("q"))
	if query == "" {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: "query parameter q is required",
		})
		return
	}

	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	minScore := parseSemanticMinScore(c.Query("min_score"))
	branch := c.Query("branch")
	if branch == "" {
		branch = repository.Metadata.DefaultBranch
	}
	if branch == "" {
		branch = "main"
	}

	embedding, err := h.embeddingProvider.Embed(c.Request.Context(), []string{query}, embeddings.InputTypeQuery)
	if err != nil {
		utils.Error("semantic search: embed query failed", "repo_id", repoID, "error", err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:            "embedding_error",
			ErrorDescription: "failed to embed query",
		})
		return
	}
	if len(embedding.Embeddings) != 1 {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:            "embedding_error",
			ErrorDescription: "embedding provider returned an invalid response",
		})
		return
	}

	matches, err := h.repo.SearchEmbeddings(c.Request.Context(), storage.EmbeddingSearchFilter{
		RepositoryID: repoID,
		Query:        query,
		Vector:       embedding.Embeddings[0],
		Provider:     h.embeddingProvider.Provider(),
		Model:        h.embeddingProvider.Model(),
		Dimension:    h.embeddingProvider.Dimension(),
		Branch:       branch,
		Limit:        limit,
		MinScore:     minScore,
	})
	if err != nil {
		utils.Error("semantic search: storage search failed", "repo_id", repoID, "error", err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:            "search_error",
			ErrorDescription: "failed to search embeddings",
		})
		return
	}

	results := make([]models.SemanticSearchResult, len(matches))
	for i, match := range matches {
		results[i] = models.SemanticSearchResult{
			FilePath:  match.FilePath,
			Content:   match.Content,
			Language:  match.Language,
			StartLine: match.StartLine,
			EndLine:   match.EndLine,
			Score:     match.Score,
			Provider:  match.Provider,
			Model:     match.Model,
			Branch:    match.Branch,
		}
	}

	c.JSON(http.StatusOK, models.SemanticSearchResponse{
		Query:   query,
		Total:   len(results),
		Results: results,
	})
}

func parseSemanticMinScore(raw string) float64 {
	if raw == "" {
		return defaultSemanticMinScore
	}
	score, err := strconv.ParseFloat(raw, 64)
	if err != nil || score < 0 || score > 1 {
		return defaultSemanticMinScore
	}
	return score
}

func (h *AnalysisHandler) fetchAccessibleRepository(c *gin.Context, repoID string) (*models.Repository, bool) {
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

	userID, err := utils.GetUserIDFromContext(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{
			Error:            "unauthorized",
			ErrorDescription: "missing or invalid authentication",
		})
		return nil, false
	}
	if repository.OwnerUserID != userID {
		c.JSON(http.StatusForbidden, models.ErrorResponse{
			Error:            "forbidden",
			ErrorDescription: "you do not have access to this repository",
		})
		return nil, false
	}

	return repository, true
}
