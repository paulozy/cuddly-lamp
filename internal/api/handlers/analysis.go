package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hibiken/asynq"
	"github.com/paulozy/idp-with-ai-backend/internal/ai"
	"github.com/paulozy/idp-with-ai-backend/internal/embeddings"
	"github.com/paulozy/idp-with-ai-backend/internal/integrations/github"
	"github.com/paulozy/idp-with-ai-backend/internal/jobs"
	"github.com/paulozy/idp-with-ai-backend/internal/jobs/tasks"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
	redisstore "github.com/paulozy/idp-with-ai-backend/internal/storage/redis"
	"github.com/paulozy/idp-with-ai-backend/internal/utils"
)

// SynthesizerFactory builds an ai.Synthesizer for a given Anthropic API key.
// The handler resolves the org-specific key at request time and constructs the
// synthesizer lazily, mirroring how analysis handlers resolve providers today.
type SynthesizerFactory func(apiKey string) ai.Synthesizer

type AnalysisHandler struct {
	repo               storage.Repository
	enqueuer           jobs.Enqueuer
	cache              redisstore.Cache
	githubFactory      func(token string) github.ClientInterface
	synthesizerFactory SynthesizerFactory
}

const defaultSemanticMinScore = 0.55

func NewAnalysisHandler(
	repo storage.Repository,
	enqueuer jobs.Enqueuer,
	cache redisstore.Cache,
	synthesizerFactory SynthesizerFactory,
) *AnalysisHandler {
	if cache == nil {
		cache = redisstore.NewNoopCache()
	}
	return &AnalysisHandler{
		repo:     repo,
		enqueuer: enqueuer,
		cache:    cache,
		githubFactory: func(token string) github.ClientInterface {
			return github.NewClient(token)
		},
		synthesizerFactory: synthesizerFactory,
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

	repo, ok := h.fetchAccessibleRepository(c, repoID)
	if !ok {
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
	cfg, err := h.repo.GetOrganizationConfig(c.Request.Context(), repo.OrganizationID)
	if err != nil || cfg == nil || cfg.AnthropicAPIKey == "" {
		c.JSON(http.StatusServiceUnavailable, models.ErrorResponse{
			Error:            "analysis_unavailable",
			ErrorDescription: "anthropic api key is not configured for this organization",
		})
		return
	}
	used, err := h.repo.SumTokensUsedSince(c.Request.Context(), repo.OrganizationID, time.Now().UTC().Add(-time.Hour))
	if err == nil && used >= int64(cfg.AnthropicTokensPerHour) {
		c.JSON(http.StatusTooManyRequests, models.ErrorResponse{
			Error:            "rate_limit_exceeded",
			ErrorDescription: fmt.Sprintf("token budget exhausted (%d/%d tokens used in last hour)", used, cfg.AnthropicTokensPerHour),
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

	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if offset < 0 {
		offset = 0
	}

	analyses, total, err := h.repo.GetAnalysesByRepository(c.Request.Context(), repoID, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:            "internal_error",
			ErrorDescription: "failed to fetch analyses",
		})
		return
	}

	c.JSON(http.StatusOK, models.AnalysisListResponse{
		Total:    total,
		Analyses: analyses,
		Limit:    limit,
		Offset:   offset,
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
	orgID, err := utils.GetOrganizationIDFromContext(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{
			Error:            "unauthorized",
			ErrorDescription: "missing or invalid organization",
		})
		return
	}
	embeddingProvider, err := h.embeddingProviderForOrganization(c.Request.Context(), orgID)
	if err != nil {
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
	taskID := fmt.Sprintf("embeddings:generate:%s:%s:%s:%d", repoID, req.Branch, embeddingProvider.Model(), embeddingProvider.Dimension())
	err = h.enqueuer.Enqueue(c.Request.Context(), tasks.TypeGenerateEmbeddings, payload,
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
// @Description  Returns code chunks ranked by embedding similarity. When `synthesize=true` the response upgrades to Server-Sent Events (Content-Type: text/event-stream) and streams: a single `results` event with the SemanticSearchResponse JSON, then either a single `synthesis` event (cache hit) or a sequence of `token_delta` events (cache miss), then a terminal `done` event with token usage. Token-budget limits still apply (HTTP 429 is returned as plain JSON without an SSE upgrade).
// @Tags         search
// @Produce      json
// @Security     BearerAuth
// @Param        id          path      string true  "Repository ID"
// @Param        q           query     string true  "Search query"
// @Param        limit       query     int    false "Result limit"
// @Param        branch      query     string false "Branch"
// @Param        min_score   query     number false "Minimum similarity score (0-1, default 0.55)"
// @Param        synthesize  query     bool   false "Stream an AI synthesis of the results via SSE (default false)"
// @Success      200      {object}  models.SemanticSearchResponse
// @Failure      400      {object}  models.ErrorResponse
// @Failure      401      {object}  models.ErrorResponse
// @Failure      404      {object}  models.ErrorResponse
// @Failure      429      {object}  models.ErrorResponse
// @Failure      503      {object}  models.ErrorResponse
// @Router       /repositories/{id}/search [get]
func (h *AnalysisHandler) SemanticSearch(c *gin.Context) {
	orgID, err := utils.GetOrganizationIDFromContext(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{
			Error:            "unauthorized",
			ErrorDescription: "missing or invalid organization",
		})
		return
	}
	embeddingProvider, err := h.embeddingProviderForOrganization(c.Request.Context(), orgID)
	if err != nil {
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
	synthesize := c.Query("synthesize") == "true"

	// When synthesizing, the budget check must run before any response bytes
	// are written so that 429 stays plain JSON for both SSE and non-SSE clients.
	var orgConfig *models.OrganizationConfig
	if synthesize {
		orgConfig, err = h.repo.GetOrganizationConfig(c.Request.Context(), repository.OrganizationID)
		if err != nil {
			utils.Error("semantic search: load org config failed", "repo_id", repoID, "error", err)
			c.JSON(http.StatusInternalServerError, models.ErrorResponse{
				Error:            "internal_error",
				ErrorDescription: "failed to load organization config",
			})
			return
		}
		if orgConfig != nil && orgConfig.AnthropicAPIKey != "" {
			used, err := h.repo.SumTokensUsedSince(c.Request.Context(), repository.OrganizationID, time.Now().UTC().Add(-time.Hour))
			if err == nil && used >= int64(orgConfig.AnthropicTokensPerHour) {
				c.JSON(http.StatusTooManyRequests, models.ErrorResponse{
					Error:            "rate_limit_exceeded",
					ErrorDescription: fmt.Sprintf("token budget exhausted (%d/%d tokens used in last hour)", used, orgConfig.AnthropicTokensPerHour),
				})
				return
			}
		}
	}

	embedding, err := embeddingProvider.Embed(c.Request.Context(), []string{query}, embeddings.InputTypeQuery)
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
		Provider:     embeddingProvider.Provider(),
		Model:        embeddingProvider.Model(),
		Dimension:    embeddingProvider.Dimension(),
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

	response := models.SemanticSearchResponse{
		Query:   query,
		Total:   len(results),
		Results: results,
	}

	if !synthesize {
		c.JSON(http.StatusOK, response)
		return
	}

	h.streamSearchSynthesis(c, repository, query, response, orgConfig)
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

func (h *AnalysisHandler) embeddingProviderForOrganization(ctx context.Context, orgID string) (embeddings.Provider, error) {
	cfg, err := h.repo.GetOrganizationConfig(ctx, orgID)
	if err != nil {
		return nil, err
	}
	if cfg == nil || cfg.VoyageAPIKey == "" || cfg.EmbeddingsProvider != embeddings.ProviderVoyage {
		return nil, fmt.Errorf("embedding provider is not configured")
	}
	return embeddings.NewVoyageClient(cfg.VoyageAPIKey, cfg.EmbeddingsModel, cfg.EmbeddingsDimensions), nil
}
