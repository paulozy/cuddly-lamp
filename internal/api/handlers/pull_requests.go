package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/paulozy/idp-with-ai-backend/internal/integrations/github"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
	"github.com/paulozy/idp-with-ai-backend/internal/utils"
)

type pullRequestGitHubContext struct {
	repository *models.Repository
	config     *models.OrganizationConfig
	client     github.ClientInterface
	owner      string
	repoName   string
}

// PullRequestHandler serves read-only GitHub pull request data: listing,
// detail, and changed files. It holds no queue or cache because nothing here
// is asynchronous — every response is a straight pass-through of the GitHub
// API for the repository's organization token.
type PullRequestHandler struct {
	repo          storage.Repository
	githubFactory func(token string) github.ClientInterface
}

func NewPullRequestHandler(repo storage.Repository) *PullRequestHandler {
	return &PullRequestHandler{
		repo: repo,
		githubFactory: func(token string) github.ClientInterface {
			return github.NewClient(token)
		},
	}
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
func (h *PullRequestHandler) ListPullRequests(c *gin.Context) {
	ghCtx, ok := h.resolveGitHubContext(c, c.Param("id"))
	if !ok {
		return
	}

	prs, err := ghCtx.client.ListPullRequests(c.Request.Context(), ghCtx.owner, ghCtx.repoName)
	if err != nil {
		h.githubError(c, err)
		return
	}

	items := make([]models.PullRequestListItemResponse, 0, len(prs))
	for _, pr := range prs {
		items = append(items, models.PullRequestListItemResponse{
			PullRequest: pullRequestToResponse(pr),
		})
	}

	c.JSON(http.StatusOK, models.PullRequestListResponse{
		Items: items,
		Total: len(items),
	})
}

// GetPullRequest returns PR metadata and changed files.
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
func (h *PullRequestHandler) GetPullRequest(c *gin.Context) {
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

	c.JSON(http.StatusOK, models.PullRequestDetailResponse{
		PullRequest: pullRequestToResponse(*pr),
		Files:       pullRequestFilesToResponse(files),
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
func (h *PullRequestHandler) GetPullRequestFiles(c *gin.Context) {
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

func (h *PullRequestHandler) resolvePullRequestContext(c *gin.Context) (*pullRequestGitHubContext, int, bool) {
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

func (h *PullRequestHandler) resolveGitHubContext(c *gin.Context, repoID string) (*pullRequestGitHubContext, bool) {
	repository, ok := h.fetchAccessibleRepository(c, repoID)
	if !ok {
		return nil, false
	}
	if repository.Type != "" && repository.Type != models.RepositoryTypeGitHub {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:            "unsupported_repository_type",
			ErrorDescription: "pull requests are supported only for github repositories",
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

	return &pullRequestGitHubContext{
		repository: repository,
		config:     cfg,
		client:     h.githubFactory(cfg.GithubToken),
		owner:      parts[0],
		repoName:   parts[1],
	}, true
}

func (h *PullRequestHandler) fetchAccessibleRepository(c *gin.Context, repoID string) (*models.Repository, bool) {
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

func (h *PullRequestHandler) githubError(c *gin.Context, err error) {
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
