package handlers

import (
	"context"
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
	*scmResolver
}

func NewPullRequestHandler(repo storage.Repository, hosts scm.Credentials) *PullRequestHandler {
	return &PullRequestHandler{scmResolver: newSCMResolver(repo, hosts)}
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
	resolved, ok := h.resolveRepository(c, repoID)
	if !ok {
		return nil, false
	}
	return &pullRequestContext{client: resolved.provider, ref: resolved.ref}, true
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

// ReviewPullRequestRequest carries the optional message attached to a verdict.
type ReviewPullRequestRequest struct {
	Body string `json:"body"`
}

// ApprovePullRequest records an approval on the repository's host.
// @Summary      Approve a pull request
// @Tags         pull-requests
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id         path      string                    true   "Repository ID"
// @Param        pr_number  path      int                       true   "Pull request number"
// @Param        body       body      ReviewPullRequestRequest  false  "Review message"
// @Success      204        "approved"
// @Failure      400        {object}  models.ErrorResponse
// @Failure      401        {object}  models.ErrorResponse
// @Failure      403        {object}  models.ErrorResponse
// @Failure      501        {object}  models.ErrorResponse
// @Failure      503        {object}  models.ErrorResponse
// @Router       /repositories/{id}/pull-requests/{pr_number}/approve [post]
func (h *PullRequestHandler) ApprovePullRequest(c *gin.Context) {
	h.submitReview(c, func(ctx context.Context, resolved *resolvedRepo, number int64, body string) error {
		return resolved.provider.ApproveChangeRequest(ctx, resolved.ref, number, body)
	}, "approved")
}

// RequestPullRequestChanges asks the author for changes.
// @Summary      Request changes on a pull request
// @Tags         pull-requests
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id         path      string                    true   "Repository ID"
// @Param        pr_number  path      int                       true   "Pull request number"
// @Param        body       body      ReviewPullRequestRequest  false  "Review message"
// @Success      204        "changes requested"
// @Failure      400        {object}  models.ErrorResponse
// @Failure      401        {object}  models.ErrorResponse
// @Failure      403        {object}  models.ErrorResponse
// @Failure      501        {object}  models.ErrorResponse  "provider has no equivalent"
// @Failure      503        {object}  models.ErrorResponse
// @Router       /repositories/{id}/pull-requests/{pr_number}/request-changes [post]
func (h *PullRequestHandler) RequestPullRequestChanges(c *gin.Context) {
	h.submitReview(c, func(ctx context.Context, resolved *resolvedRepo, number int64, body string) error {
		return resolved.provider.RequestChanges(ctx, resolved.ref, number, body)
	}, "requested changes on")
}

// submitReview is the shared path for both verdicts: parse, authorize, attach
// attribution, dispatch, and translate the provider's answer.
func (h *PullRequestHandler) submitReview(
	c *gin.Context,
	action func(context.Context, *resolvedRepo, int64, string) error,
	verb string,
) {
	prNumber, ok := parsePullRequestNumber(c)
	if !ok {
		return
	}

	var req ReviewPullRequestRequest
	// A body is optional; only malformed JSON is an error.
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, models.ErrorResponse{
				Error:            "invalid_request",
				ErrorDescription: err.Error(),
			})
			return
		}
	}

	resolved, ok := h.resolveRepository(c, c.Param("id"))
	if !ok {
		return
	}
	if !h.requireWriteAccess(c, resolved.repository) {
		return
	}

	err := action(c.Request.Context(), resolved, int64(prNumber), reviewBody(c, req.Body, verb))
	switch {
	case err == nil:
		c.Status(http.StatusNoContent)
	case errors.Is(err, scm.ErrUnsupportedCapability):
		// Not a failure — this host genuinely has no equivalent action. 501 says
		// "the server does not implement this", which is exactly the situation.
		c.JSON(http.StatusNotImplemented, models.ErrorResponse{
			Error:            "unsupported_capability",
			ErrorDescription: "this repository's host does not support that review action",
		})
	default:
		h.providerError(c, err)
	}
}

// reviewBody stamps the acting user onto the review message.
//
// This matters more than it looks. The call is authenticated with the
// *organization's* token, so the host records the verdict under whoever owns
// that token — not the person who clicked. On GitHub an approval under the
// wrong name can also satisfy branch protection. The platform cannot change
// whose credentials are used without per-user write scopes, so the least it can
// do is make the real actor part of the permanent record.
func reviewBody(c *gin.Context, body, verb string) string {
	actor := "Someone"
	if claims, ok := c.Request.Context().Value(utils.ContextKeyClaims).(*models.TokenClaims); ok {
		if claims.FullName != "" {
			actor = claims.FullName
		} else if claims.Email != "" {
			actor = claims.Email
		}
	}
	attribution := actor + " " + verb + " this via the IDP."
	if body == "" {
		return attribution
	}
	return body + "\n\n— " + attribution
}
