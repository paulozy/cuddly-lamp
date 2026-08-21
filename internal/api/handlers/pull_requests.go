package handlers

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/paulozy/idp-with-ai-backend/internal/integrations/scm"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
	"github.com/paulozy/idp-with-ai-backend/internal/utils"
)

type pullRequestContext struct {
	client scm.ChangeRequestReader
	ref    scm.RepoRef
}

// PullRequestHandler serves read-only pull/merge request data: listing,
// detail, and changed files. It holds no queue or cache because nothing here
// is asynchronous — every response is a straight pass-through of the host's
// API using the repository organization's token.
type PullRequestHandler struct {
	repo storage.Repository
	// hosts carries the deployment's provider API roots, with no tokens — see
	// scm.HostsOnly for why the two are treated differently.
	hosts   scm.Credentials
	resolve scm.ResolverFunc
}

func NewPullRequestHandler(repo storage.Repository, hosts scm.Credentials) *PullRequestHandler {
	return &PullRequestHandler{repo: repo, hosts: hosts, resolve: scm.For}
}

// ListPullRequests lists open pull/merge requests for a repository.
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
	prCtx, ok := h.resolveContext(c, c.Param("id"))
	if !ok {
		return
	}

	prs, err := prCtx.client.ListChangeRequests(c.Request.Context(), prCtx.ref)
	if err != nil {
		h.providerError(c, err)
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
	prCtx, prNumber, ok := h.resolvePullRequestContext(c)
	if !ok {
		return
	}

	pr, err := prCtx.client.GetChangeRequest(c.Request.Context(), prCtx.ref, int64(prNumber))
	if err != nil {
		h.providerError(c, err)
		return
	}
	files, err := prCtx.client.GetChangeRequestFiles(c.Request.Context(), prCtx.ref, int64(prNumber))
	if err != nil {
		h.providerError(c, err)
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
	prCtx, prNumber, ok := h.resolvePullRequestContext(c)
	if !ok {
		return
	}

	files, err := prCtx.client.GetChangeRequestFiles(c.Request.Context(), prCtx.ref, int64(prNumber))
	if err != nil {
		h.providerError(c, err)
		return
	}
	items := pullRequestFilesToResponse(files)
	c.JSON(http.StatusOK, models.PullRequestFilesResponse{
		Items: items,
		Total: len(items),
	})
}

func (h *PullRequestHandler) resolvePullRequestContext(c *gin.Context) (*pullRequestContext, int, bool) {
	prNumber, ok := parsePullRequestNumber(c)
	if !ok {
		return nil, 0, false
	}
	prCtx, ok := h.resolveContext(c, c.Param("id"))
	if !ok {
		return nil, 0, false
	}
	return prCtx, prNumber, true
}

func (h *PullRequestHandler) resolveContext(c *gin.Context, repoID string) (*pullRequestContext, bool) {
	repository, ok := h.fetchAccessibleRepository(c, repoID)
	if !ok {
		return nil, false
	}

	// The host in the URL decides which provider is queried, not the stored
	// type — the URL is what the user gave us and it is never empty.
	projectPath, provider, err := utils.ParseRepositoryURL(repository.URL)
	if err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:            "invalid_repository_url",
			ErrorDescription: err.Error(),
		})
		return nil, false
	}
	ref, err := scm.ParseRepoRef(projectPath)
	if err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:            "invalid_repository_url",
			ErrorDescription: "repository URL must identify a namespace and a repository",
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

	// The organization's own token, never the platform's — browsing another
	// organization's code with the deployment's credentials is not something a
	// read endpoint should silently do. The host does fall back, because
	// talking to the wrong GitLab is worse than talking to none.
	client, err := h.resolve(provider, scm.CredentialsFromConfig(cfg, h.hosts))
	if err != nil {
		h.providerError(c, err)
		return nil, false
	}

	return &pullRequestContext{client: client, ref: ref}, true
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

func (h *PullRequestHandler) providerError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, scm.ErrUnsupportedProvider):
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:            "unsupported_repository_type",
			ErrorDescription: err.Error(),
		})
	case errors.Is(err, scm.ErrMissingCredentials):
		c.JSON(http.StatusServiceUnavailable, models.ErrorResponse{
			Error:            "provider_unavailable",
			ErrorDescription: "no access token is configured for this repository's provider",
		})
	case errors.Is(err, scm.ErrNotFound):
		c.JSON(http.StatusNotFound, models.ErrorResponse{
			Error:            "not_found",
			ErrorDescription: "provider resource not found",
		})
	case errors.Is(err, scm.ErrUnauthorized):
		c.JSON(http.StatusServiceUnavailable, models.ErrorResponse{
			Error:            "provider_unavailable",
			ErrorDescription: "the configured provider token is invalid or unauthorized",
		})
	case errors.Is(err, scm.ErrRateLimited):
		c.JSON(http.StatusServiceUnavailable, models.ErrorResponse{
			Error:            "provider_rate_limited",
			ErrorDescription: "provider API rate limit exceeded",
		})
	default:
		c.JSON(http.StatusServiceUnavailable, models.ErrorResponse{
			Error:            "provider_unavailable",
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

func pullRequestToResponse(pr scm.ChangeRequest) models.PullRequestResponse {
	return models.PullRequestResponse{
		ID:             pr.ID,
		Number:         pr.Number,
		Title:          pr.Title,
		Body:           pr.Body,
		State:          pr.State,
		AuthorLogin:    pr.AuthorLogin,
		HeadBranch:     pr.HeadRef,
		HeadSHA:        pr.HeadSHA,
		BaseBranch:     pr.BaseRef,
		BaseSHA:        pr.BaseSHA,
		Draft:          pr.Draft,
		CommitsCount:   pr.CommitsCount,
		ChangedFiles:   pr.ChangedFiles,
		AdditionsCount: pr.Additions,
		DeletionsCount: pr.Deletions,
		HTMLURL:        pr.WebURL,
		CreatedAt:      pr.CreatedAt,
		UpdatedAt:      pr.UpdatedAt,
		MergedAt:       pr.MergedAt,
	}
}

func pullRequestFilesToResponse(files []scm.ChangeRequestFile) []models.PullRequestFileResponse {
	out := make([]models.PullRequestFileResponse, 0, len(files))
	for _, file := range files {
		out = append(out, models.PullRequestFileResponse{
			SHA:       file.SHA,
			Filename:  file.Path,
			Status:    file.Status,
			Additions: file.Additions,
			Deletions: file.Deletions,
			Changes:   file.Changes,
			Patch:     file.Patch,
		})
	}
	return out
}
