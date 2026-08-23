package handlers

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/paulozy/idp-with-ai-backend/internal/integrations/scm"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
	"github.com/paulozy/idp-with-ai-backend/internal/utils"
)

type pullRequestContext struct {
	client scm.ChangeRequestReader
	// reviews reads recorded verdicts. Separate from client because it is a
	// separate capability, and because the two are used at different costs —
	// one request per change request, versus one for the whole page.
	reviews scm.ChangeRequestReviewReader
	ref     scm.RepoRef
	// kind names the host the repository lives on, so the detail endpoint can
	// look up the caller's identity there. Reads need nothing else from the
	// resolution, which is why this stays narrower than resolvedRepo.
	kind models.RepositoryType
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

	attachReviewStates(c.Request.Context(), prCtx.reviews, prCtx.ref, items)

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

	response := pullRequestToResponse(*pr)
	response.ReviewBlockedReason = h.reviewBlockedReason(c, prCtx.kind, *pr)
	attachReviewState(c.Request.Context(), prCtx.reviews, prCtx.ref, &response)

	c.JSON(http.StatusOK, models.PullRequestDetailResponse{
		PullRequest: response,
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
	return &pullRequestContext{
		client:  resolved.provider,
		reviews: resolved.provider,
		ref:     resolved.ref,
		kind:    resolved.kind,
	}, true
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
// @Failure      409        {object}  models.ErrorResponse  "the host refused: self_review or provider_rejected"
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
// @Failure      409        {object}  models.ErrorResponse  "the host refused: self_review or provider_rejected"
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
		h.providerError(c, h.nameTheActingIdentity(c, resolved, err))
	}
}

// nameTheActingIdentity fills in whose credential the host refused, for the one
// rejection where that is the missing piece.
//
// The refusal says "you cannot approve your own change request", which is
// baffling to read when you did not open it — the author it means is the
// organization's token. Naming the token's owner turns a dead end into a
// diagnosis, and it costs a request to the host, which is why it is paid here
// and not on every successful review.
//
// Failing to resolve the identity is not an error. The rejection is already
// established and still gets reported; only the extra detail is lost.
func (h *PullRequestHandler) nameTheActingIdentity(c *gin.Context, resolved *resolvedRepo, err error) error {
	var providerErr *scm.ProviderError
	if !errors.As(err, &providerErr) || providerErr.Reason != scm.ReasonSelfReview || providerErr.TokenOwner != "" {
		return err
	}

	identity, identityErr := resolved.provider.CurrentUser(c.Request.Context())
	if identityErr != nil || identity == nil || identity.Login == "" {
		utils.Warn("could not resolve the acting identity behind a self-review rejection",
			"repository", resolved.repository.ID, "error", identityErr)
		return err
	}

	// Copied rather than mutated: the error came from the adapter and may be a
	// shared value for all this function knows.
	enriched := *providerErr
	enriched.TokenOwner = identity.Login
	return &enriched
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

// Reasons a change request cannot be reviewed by the caller. These reach the
// client verbatim, which picks the wording; the backend names the situation,
// not the sentence.
const (
	// ReviewBlockedSelfAuthored means the caller opened this change request.
	ReviewBlockedSelfAuthored = "self_authored"
)

// reviewBlockedReason reports why the caller cannot review this change request,
// or nil when nothing known stops them.
//
// It exists so the interface can refuse before the click instead of surfacing a
// rejection after it. Two things it deliberately is not:
//
//   - Not a permission check. The host decides, and it knows things this cannot
//     — GitLab lets a project forbid approval by the author *or* by anyone who
//     committed, and either rule is invisible from here. A nil answer means
//     "nothing we can see", never "allowed".
//   - Not the answer to the failure people actually hit. The identity that
//     performs a review is the organization's token, not the caller, so the
//     common refusal is one this check cannot predict. That case is explained
//     after the fact, in submitReview.
//
// Costs no request to the host: it compares the caller's stored provider login
// against the change request's author.
func (h *PullRequestHandler) reviewBlockedReason(c *gin.Context, kind models.RepositoryType, pr scm.ChangeRequest) *string {
	if pr.AuthorLogin == "" {
		return nil
	}
	login := h.actingProviderLogin(c, kind)
	if login == "" || !strings.EqualFold(login, pr.AuthorLogin) {
		return nil
	}
	reason := ReviewBlockedSelfAuthored
	return &reason
}

// actingProviderLogin resolves the caller's login on a given host, or "" when
// it cannot be determined.
//
// Empty is an ordinary outcome, not a failure: a member who signed up with
// email and password has no OAuth connection at all, and connections created
// before migration 029 have no stored username until their next login. Callers
// must treat "unknown" as "no opinion".
func (h *PullRequestHandler) actingProviderLogin(c *gin.Context, kind models.RepositoryType) string {
	actor := actorFromContext(c)
	if actor.UserID == "" {
		return ""
	}
	conn, err := h.repo.GetOAuthConnectionByUser(c.Request.Context(), actor.UserID, string(kind))
	if err != nil || conn == nil {
		// Not worth failing the read over. The detail response is useful without
		// this, and the host still refuses the action if it must.
		return ""
	}
	return conn.ProviderUsername
}

// How much provider quota a single list request may spend on review state.
//
// Review state is one request per change request — neither host reports it on
// the list endpoint — so a repository with many open change requests would turn
// one page view into dozens of API calls. These two constants bound that:
// reviewStateFanOut caps how many run at once, reviewStateListCeiling caps how
// many are attempted at all. Change requests past the ceiling report unknown
// review state rather than silently reporting "not reviewed".
const (
	reviewStateFanOut      = 8
	reviewStateListCeiling = 30
)

// attachReviewState fills in the review verdict for a single change request.
//
// A failure here is not the read's failure. The change request itself loaded
// fine, and a missing badge is a far better outcome than a 503 on a page that
// otherwise works — so the error is logged and the field left null, which the
// client renders as nothing rather than as "not reviewed".
func attachReviewState(
	ctx context.Context,
	reader scm.ChangeRequestReviewReader,
	ref scm.RepoRef,
	response *models.PullRequestResponse,
) {
	state, err := reader.GetChangeRequestReviews(ctx, ref, response.Number)
	if err != nil || state == nil {
		utils.Warn("could not read review state",
			"repository", ref.FullPath(), "number", response.Number, "error", err)
		return
	}
	decision := state.Decision
	response.ReviewDecision = &decision
	response.ApprovedBy = state.ApprovedBy
	response.ChangesRequestedBy = state.ChangesRequestedBy
}

// attachReviewStates fills in review verdicts for a page of change requests,
// bounded by the two constants above.
//
// The ceiling is reported rather than applied silently: a caller reading the
// logs can see that some change requests were skipped, instead of concluding
// from a page of blank badges that nobody reviews anything.
func attachReviewStates(
	ctx context.Context,
	reader scm.ChangeRequestReviewReader,
	ref scm.RepoRef,
	items []models.PullRequestListItemResponse,
) {
	attempted := len(items)
	if attempted > reviewStateListCeiling {
		attempted = reviewStateListCeiling
		utils.Warn("review state skipped past the per-list ceiling",
			"repository", ref.FullPath(), "total", len(items), "attempted", attempted)
	}

	var wg sync.WaitGroup
	slots := make(chan struct{}, reviewStateFanOut)
	for i := 0; i < attempted; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			attachReviewState(ctx, reader, ref, &items[idx].PullRequest)
		}(i)
	}
	wg.Wait()
}
