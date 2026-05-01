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
	"github.com/paulozy/idp-with-ai-backend/internal/integrations/github"
	"github.com/paulozy/idp-with-ai-backend/internal/jobs/tasks"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/utils"
)

const (
	pullRequestReviewEventComment        = "COMMENT"
	pullRequestReviewEventApprove        = "APPROVE"
	pullRequestReviewEventRequestChanges = "REQUEST_CHANGES"
)

type analysisGitHubContext struct {
	repository *models.Repository
	config     *models.OrganizationConfig
	client     github.ClientInterface
	owner      string
	repoName   string
}

// ListPullRequests lists open GitHub pull requests for a repository.
// @Summary      List repository pull requests
// @Tags         pull-requests
// @Produce      json
// @Security     BearerAuth
// @Param        id  path      string  true  "Repository ID"
// @Success      200 {object}  models.PullRequestListResponse
// @Failure      401 {object}  models.ErrorResponse
// @Failure      403 {object}  models.ErrorResponse
// @Failure      404 {object}  models.ErrorResponse
// @Failure      503 {object}  models.ErrorResponse
// @Router       /repositories/{id}/pull-requests [get]
func (h *AnalysisHandler) ListPullRequests(c *gin.Context) {
	ghCtx, ok := h.resolveGitHubContext(c, c.Param("id"))
	if !ok {
		return
	}

	prs, err := ghCtx.client.ListPullRequests(c.Request.Context(), ghCtx.owner, ghCtx.repoName)
	if err != nil {
		h.githubError(c, err)
		return
	}

	prNumbers := make([]int, 0, len(prs))
	for _, pr := range prs {
		prNumbers = append(prNumbers, int(pr.Number))
	}
	analyses, err := h.repo.ListLatestAnalysesForPullRequests(c.Request.Context(), ghCtx.repository.ID, prNumbers, models.AnalysisTypeCodeReview)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:            "internal_error",
			ErrorDescription: "failed to fetch pull request analyses",
		})
		return
	}

	items := make([]models.PullRequestListItemResponse, 0, len(prs))
	for _, pr := range prs {
		var latest *models.PullRequestReviewAnalysisResponse
		if analysis, exists := analyses[int(pr.Number)]; exists {
			var mapErr error
			latest, mapErr = codeAnalysisToPullRequestReviewAnalysisResponse(&analysis)
			if mapErr != nil {
				c.JSON(http.StatusInternalServerError, models.ErrorResponse{
					Error:            "internal_error",
					ErrorDescription: "failed to map pull request analysis",
				})
				return
			}
		}
		items = append(items, models.PullRequestListItemResponse{
			PullRequest:    pullRequestToResponse(pr),
			LatestAnalysis: latest,
		})
	}

	c.JSON(http.StatusOK, models.PullRequestListResponse{
		Items: items,
		Total: len(items),
	})
}

// GetPullRequest returns PR metadata, files, and latest AI analysis.
// @Summary      Get pull request detail
// @Tags         pull-requests
// @Produce      json
// @Security     BearerAuth
// @Param        id         path      string  true  "Repository ID"
// @Param        pr_number  path      int     true  "Pull request number"
// @Success      200        {object}  models.PullRequestDetailResponse
// @Failure      400        {object}  models.ErrorResponse
// @Failure      401        {object}  models.ErrorResponse
// @Failure      403        {object}  models.ErrorResponse
// @Failure      404        {object}  models.ErrorResponse
// @Failure      503        {object}  models.ErrorResponse
// @Router       /repositories/{id}/pull-requests/{pr_number} [get]
func (h *AnalysisHandler) GetPullRequest(c *gin.Context) {
	ghCtx, prNumber, ok := h.resolvePullRequestContext(c)
	if !ok {
		return
	}

	pr, err := ghCtx.client.GetPullRequest(c.Request.Context(), ghCtx.owner, ghCtx.repoName, int64(prNumber))
	if err != nil {
		h.githubError(c, err)
		return
	}
	files, err := ghCtx.client.GetPullRequestFiles(c.Request.Context(), ghCtx.owner, ghCtx.repoName, int64(prNumber))
	if err != nil {
		h.githubError(c, err)
		return
	}
	latest, ok := h.latestPullRequestAnalysis(c, ghCtx.repository.ID, prNumber)
	if !ok {
		return
	}

	c.JSON(http.StatusOK, models.PullRequestDetailResponse{
		PullRequest:    pullRequestToResponse(*pr),
		Files:          pullRequestFilesToResponse(files),
		LatestAnalysis: latest,
	})
}

// GetPullRequestFiles returns changed files and patches for a PR.
// @Summary      Get pull request files
// @Tags         pull-requests
// @Produce      json
// @Security     BearerAuth
// @Param        id         path      string  true  "Repository ID"
// @Param        pr_number  path      int     true  "Pull request number"
// @Success      200        {object}  models.PullRequestFilesResponse
// @Failure      400        {object}  models.ErrorResponse
// @Failure      401        {object}  models.ErrorResponse
// @Failure      403        {object}  models.ErrorResponse
// @Failure      404        {object}  models.ErrorResponse
// @Failure      503        {object}  models.ErrorResponse
// @Router       /repositories/{id}/pull-requests/{pr_number}/files [get]
func (h *AnalysisHandler) GetPullRequestFiles(c *gin.Context) {
	ghCtx, prNumber, ok := h.resolvePullRequestContext(c)
	if !ok {
		return
	}

	files, err := ghCtx.client.GetPullRequestFiles(c.Request.Context(), ghCtx.owner, ghCtx.repoName, int64(prNumber))
	if err != nil {
		h.githubError(c, err)
		return
	}
	items := pullRequestFilesToResponse(files)
	c.JSON(http.StatusOK, models.PullRequestFilesResponse{
		Items: items,
		Total: len(items),
	})
}

// AnalyzePullRequest queues a manual AI reanalysis for a PR.
// @Summary      Analyze pull request
// @Tags         pull-requests
// @Produce      json
// @Security     BearerAuth
// @Param        id         path      string  true  "Repository ID"
// @Param        pr_number  path      int     true  "Pull request number"
// @Success      202        {object}  models.JobResponse
// @Failure      400        {object}  models.ErrorResponse
// @Failure      401        {object}  models.ErrorResponse
// @Failure      403        {object}  models.ErrorResponse
// @Failure      404        {object}  models.ErrorResponse
// @Failure      409        {object}  models.ErrorResponse
// @Failure      429        {object}  models.ErrorResponse
// @Failure      503        {object}  models.ErrorResponse
// @Router       /repositories/{id}/pull-requests/{pr_number}/analyze [post]
func (h *AnalysisHandler) AnalyzePullRequest(c *gin.Context) {
	ghCtx, prNumber, ok := h.resolvePullRequestContext(c)
	if !ok {
		return
	}
	if !h.ensureAnalysisAvailable(c, ghCtx.repository, ghCtx.config) {
		return
	}

	pr, err := ghCtx.client.GetPullRequest(c.Request.Context(), ghCtx.owner, ghCtx.repoName, int64(prNumber))
	if err != nil {
		h.githubError(c, err)
		return
	}

	payload := tasks.AnalyzeRepoPayload{
		RepositoryID:  ghCtx.repository.ID,
		Branch:        pr.Head.DisplayName(),
		CommitSHA:     pr.Head.SHA,
		PullRequestID: int64(prNumber),
		Type:          string(models.AnalysisTypeCodeReview),
		TriggeredBy:   "user",
	}
	taskID := fmt.Sprintf("analyze:manual:%s:pr:%d", ghCtx.repository.ID, prNumber)
	err = h.enqueuer.Enqueue(c.Request.Context(), tasks.TypeAnalyzeRepo, payload,
		asynq.TaskID(taskID),
		asynq.Retention(10*time.Minute),
	)
	if err != nil {
		if errors.Is(err, asynq.ErrTaskIDConflict) {
			c.JSON(http.StatusConflict, models.ErrorResponse{
				Error:            "analysis_in_progress",
				ErrorDescription: "an analysis for this pull request is already queued or running",
			})
			return
		}
		utils.Error("analysis handler: enqueue PR analysis failed", "repo_id", ghCtx.repository.ID, "pr_number", prNumber, "error", err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:            "queue_error",
			ErrorDescription: "failed to enqueue pull request analysis job",
		})
		return
	}

	c.JSON(http.StatusAccepted, models.JobResponse{
		Status: "queued",
		Type:   tasks.TypeAnalyzeRepo,
		Target: fmt.Sprintf("%s#%d", ghCtx.repository.ID, prNumber),
	})
}

// CreatePullRequestReview posts a GitHub PR review.
// @Summary      Create pull request review
// @Tags         pull-requests
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id         path      string                                true  "Repository ID"
// @Param        pr_number  path      int                                   true  "Pull request number"
// @Param        body       body      models.CreatePullRequestReviewRequest true  "Review"
// @Success      200        {object}  models.CreatePullRequestReviewResponse
// @Failure      400        {object}  models.ErrorResponse
// @Failure      401        {object}  models.ErrorResponse
// @Failure      403        {object}  models.ErrorResponse
// @Failure      404        {object}  models.ErrorResponse
// @Failure      422        {object}  models.ErrorResponse
// @Failure      503        {object}  models.ErrorResponse
// @Router       /repositories/{id}/pull-requests/{pr_number}/reviews [post]
func (h *AnalysisHandler) CreatePullRequestReview(c *gin.Context) {
	ghCtx, prNumber, ok := h.resolvePullRequestContext(c)
	if !ok {
		return
	}

	var req models.CreatePullRequestReviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: err.Error(),
		})
		return
	}

	event, body, comments, valid := normalizePullRequestReviewRequest(c, req)
	if !valid {
		return
	}

	reviewID, err := ghCtx.client.CreatePullRequestReview(c.Request.Context(), ghCtx.owner, ghCtx.repoName, int64(prNumber), body, event, comments)
	if err != nil {
		h.githubError(c, err)
		return
	}

	c.JSON(http.StatusOK, models.CreatePullRequestReviewResponse{
		ReviewID: reviewID,
		Event:    event,
		Status:   "submitted",
	})
}

func (h *AnalysisHandler) resolvePullRequestContext(c *gin.Context) (*analysisGitHubContext, int, bool) {
	prNumber, ok := parsePullRequestNumber(c)
	if !ok {
		return nil, 0, false
	}
	ghCtx, ok := h.resolveGitHubContext(c, c.Param("id"))
	if !ok {
		return nil, 0, false
	}
	return ghCtx, prNumber, true
}

func (h *AnalysisHandler) resolveGitHubContext(c *gin.Context, repoID string) (*analysisGitHubContext, bool) {
	repository, ok := h.fetchAccessibleRepository(c, repoID)
	if !ok {
		return nil, false
	}
	if repository.Type != "" && repository.Type != models.RepositoryTypeGitHub {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:            "unsupported_repository_type",
			ErrorDescription: "pull request reviews are supported only for github repositories",
		})
		return nil, false
	}

	cfg, err := h.repo.GetOrganizationConfig(c.Request.Context(), repository.OrganizationID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:            "internal_error",
			ErrorDescription: "failed to fetch organization config",
		})
		return nil, false
	}
	if cfg == nil || cfg.GithubToken == "" {
		c.JSON(http.StatusServiceUnavailable, models.ErrorResponse{
			Error:            "github_unavailable",
			ErrorDescription: "github token is not configured for this organization",
		})
		return nil, false
	}

	ownerRepo, _, err := utils.ParseRepositoryURL(repository.URL)
	if err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:            "invalid_repository_url",
			ErrorDescription: err.Error(),
		})
		return nil, false
	}
	parts := strings.Split(ownerRepo, "/")
	if len(parts) != 2 {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:            "invalid_repository_url",
			ErrorDescription: "repository URL must identify owner and repository",
		})
		return nil, false
	}

	return &analysisGitHubContext{
		repository: repository,
		config:     cfg,
		client:     h.githubFactory(cfg.GithubToken),
		owner:      parts[0],
		repoName:   parts[1],
	}, true
}

func (h *AnalysisHandler) ensureAnalysisAvailable(c *gin.Context, repository *models.Repository, cfg *models.OrganizationConfig) bool {
	if cfg == nil || cfg.AnthropicAPIKey == "" {
		c.JSON(http.StatusServiceUnavailable, models.ErrorResponse{
			Error:            "analysis_unavailable",
			ErrorDescription: "anthropic api key is not configured for this organization",
		})
		return false
	}
	used, err := h.repo.SumTokensUsedSince(c.Request.Context(), repository.OrganizationID, time.Now().UTC().Add(-time.Hour))
	if err == nil && used >= int64(cfg.AnthropicTokensPerHour) {
		c.JSON(http.StatusTooManyRequests, models.ErrorResponse{
			Error:            "rate_limit_exceeded",
			ErrorDescription: fmt.Sprintf("token budget exhausted (%d/%d tokens used in last hour)", used, cfg.AnthropicTokensPerHour),
		})
		return false
	}
	return true
}

func parsePullRequestNumber(c *gin.Context) (int, bool) {
	prNumber, err := strconv.Atoi(c.Param("pr_number"))
	if err != nil || prNumber <= 0 {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: "pull request number must be a positive integer",
		})
		return 0, false
	}
	return prNumber, true
}

func (h *AnalysisHandler) latestPullRequestAnalysis(c *gin.Context, repoID string, prNumber int) (*models.PullRequestReviewAnalysisResponse, bool) {
	analysis, err := h.repo.GetLatestAnalysisForPullRequest(c.Request.Context(), repoID, prNumber, models.AnalysisTypeCodeReview)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:            "internal_error",
			ErrorDescription: "failed to fetch pull request analysis",
		})
		return nil, false
	}
	resp, err := codeAnalysisToPullRequestReviewAnalysisResponse(analysis)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:            "internal_error",
			ErrorDescription: "failed to map pull request analysis",
		})
		return nil, false
	}
	return resp, true
}

func (h *AnalysisHandler) githubError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, github.ErrNotFound):
		c.JSON(http.StatusNotFound, models.ErrorResponse{
			Error:            "not_found",
			ErrorDescription: "github resource not found",
		})
	case errors.Is(err, github.ErrUnauthorized):
		c.JSON(http.StatusServiceUnavailable, models.ErrorResponse{
			Error:            "github_unavailable",
			ErrorDescription: "github token is invalid or unauthorized",
		})
	case errors.Is(err, github.ErrRateLimited):
		c.JSON(http.StatusServiceUnavailable, models.ErrorResponse{
			Error:            "github_rate_limited",
			ErrorDescription: "github API rate limit exceeded",
		})
	default:
		c.JSON(http.StatusServiceUnavailable, models.ErrorResponse{
			Error:            "github_unavailable",
			ErrorDescription: err.Error(),
		})
	}
}

func normalizePullRequestReviewRequest(c *gin.Context, req models.CreatePullRequestReviewRequest) (string, string, []github.ReviewCommentInput, bool) {
	event := strings.ToUpper(strings.TrimSpace(req.Event))
	body := strings.TrimSpace(req.Body)

	comments := make([]github.ReviewCommentInput, 0, len(req.Comments))
	for _, comment := range req.Comments {
		path := strings.TrimSpace(comment.Path)
		commentBody := strings.TrimSpace(comment.Body)
		if path == "" || commentBody == "" || comment.Position <= 0 {
			c.JSON(http.StatusUnprocessableEntity, models.ErrorResponse{
				Error:            "invalid_review",
				ErrorDescription: "inline comments require path, position > 0, and body",
			})
			return "", "", nil, false
		}
		comments = append(comments, github.ReviewCommentInput{
			Path:     path,
			Position: comment.Position,
			Body:     commentBody,
		})
	}

	switch event {
	case pullRequestReviewEventComment:
		if body == "" && len(comments) == 0 {
			c.JSON(http.StatusUnprocessableEntity, models.ErrorResponse{
				Error:            "invalid_review",
				ErrorDescription: "comment reviews require a body or at least one inline comment",
			})
			return "", "", nil, false
		}
	case pullRequestReviewEventApprove:
	case pullRequestReviewEventRequestChanges:
		if body == "" {
			c.JSON(http.StatusUnprocessableEntity, models.ErrorResponse{
				Error:            "invalid_review",
				ErrorDescription: "request changes reviews require a body",
			})
			return "", "", nil, false
		}
	default:
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: "event must be one of: COMMENT, APPROVE, REQUEST_CHANGES",
		})
		return "", "", nil, false
	}

	return event, body, comments, true
}

func pullRequestToResponse(pr github.PullRequest) models.PullRequestResponse {
	return models.PullRequestResponse{
		ID:             pr.ID,
		Number:         pr.Number,
		Title:          pr.Title,
		Body:           pr.Body,
		State:          pr.State,
		AuthorLogin:    pr.User.Login,
		HeadBranch:     pr.Head.DisplayName(),
		HeadSHA:        pr.Head.SHA,
		BaseBranch:     pr.Base.DisplayName(),
		BaseSHA:        pr.Base.SHA,
		Draft:          pr.Draft,
		CommitsCount:   pr.CommitsCount,
		ChangedFiles:   pr.ChangedFiles,
		AdditionsCount: pr.AdditionsCount,
		DeletionsCount: pr.DeletionsCount,
		HTMLURL:        pr.HTMLURL,
		CreatedAt:      pr.CreatedAt,
		UpdatedAt:      pr.UpdatedAt,
		MergedAt:       pr.MergedAt,
	}
}

func pullRequestFilesToResponse(files []github.PRFile) []models.PullRequestFileResponse {
	out := make([]models.PullRequestFileResponse, 0, len(files))
	for _, file := range files {
		out = append(out, models.PullRequestFileResponse{
			SHA:       file.SHA,
			Filename:  file.Filename,
			Status:    file.Status,
			Additions: file.Additions,
			Deletions: file.Deletions,
			Changes:   file.Changes,
			Patch:     file.Patch,
		})
	}
	return out
}

func codeAnalysisToPullRequestReviewAnalysisResponse(analysis *models.CodeAnalysis) (*models.PullRequestReviewAnalysisResponse, error) {
	if analysis == nil {
		return nil, nil
	}

	pullRequestID := 0
	if analysis.PullRequestID != nil {
		pullRequestID = *analysis.PullRequestID
	}

	return &models.PullRequestReviewAnalysisResponse{
		ID:            analysis.ID,
		RepositoryID:  analysis.RepositoryID,
		PullRequestID: pullRequestID,
		Type:          analysis.Type,
		Status:        analysis.Status,
		SummaryText:   analysis.SummaryText,
		Issues:        append([]models.CodeIssue(nil), analysis.Issues.Data()...),
		IssueCount:    analysis.IssueCount,
		CriticalCount: analysis.CriticalCount,
		ErrorCount:    analysis.ErrorCount,
		WarningCount:  analysis.WarningCount,
		InfoCount:     analysis.InfoCount,
		AIModel:       analysis.AIModel,
		TokensUsed:    analysis.TokensUsed,
		ProcessingMs:  analysis.ProcessingMs,
		ErrorMessage:  analysis.ErrorMessage,
		CreatedAt:     analysis.CreatedAt,
		UpdatedAt:     analysis.UpdatedAt,
	}, nil
}
