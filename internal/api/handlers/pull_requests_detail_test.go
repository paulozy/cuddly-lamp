package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/paulozy/idp-with-ai-backend/internal/integrations/scm"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
	"github.com/paulozy/idp-with-ai-backend/internal/utils"
)

// detailStore adds the OAuth lookup the pre-flight check performs. The zero
// value answers "no connection", which is the common case for a member who
// signed up with a password.
type detailStore struct {
	storage.Repository
	conn    *models.OAuthConnection
	connErr error
}

func (s *detailStore) GetRepository(_ context.Context, id string) (*models.Repository, error) {
	return &models.Repository{ID: id, OrganizationID: browseOrgID, URL: "https://github.com/owner/repo"}, nil
}

func (s *detailStore) GetOrganizationConfig(_ context.Context, _ string) (*models.OrganizationConfig, error) {
	return &models.OrganizationConfig{}, nil
}

func (s *detailStore) GetOAuthConnectionByUser(_ context.Context, _, _ string) (*models.OAuthConnection, error) {
	return s.conn, s.connErr
}

// detailProvider serves one change request and, optionally, the token identity.
type detailProvider struct {
	scm.Provider
	pr           scm.ChangeRequest
	identity     *scm.Identity
	identityErr  error
	identityHits int
}

func (p *detailProvider) GetChangeRequest(_ context.Context, _ scm.RepoRef, _ int64) (*scm.ChangeRequest, error) {
	pr := p.pr
	return &pr, nil
}

func (p *detailProvider) GetChangeRequestFiles(_ context.Context, _ scm.RepoRef, _ int64) ([]scm.ChangeRequestFile, error) {
	return nil, nil
}

func (p *detailProvider) CurrentUser(_ context.Context) (*scm.Identity, error) {
	p.identityHits++
	return p.identity, p.identityErr
}

// This double reports no review state, which exercises the "host could not be
// asked" path: the read still succeeds and the verdict comes back null. Tests
// that care about the verdict use reviewStateProvider instead.
func (p *detailProvider) GetChangeRequestReviews(_ context.Context, _ scm.RepoRef, _ int64) (*scm.ReviewState, error) {
	return nil, nil
}

func newDetailHandler(store storage.Repository, provider scm.Provider) *PullRequestHandler {
	h := NewPullRequestHandler(store, scm.Credentials{})
	h.resolve = func(_ models.RepositoryType, _ scm.Credentials) (scm.Provider, error) {
		return provider, nil
	}
	return h
}

func detailRequest(h *PullRequestHandler) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Params = gin.Params{{Key: "id", Value: "repo-1"}, {Key: "pr_number", Value: "412"}}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := context.WithValue(req.Context(), utils.ContextKeyOrganization, browseOrgID)
	ctx = context.WithValue(ctx, utils.ContextKeyUser, "user-1")
	ctx = context.WithValue(ctx, utils.ContextKeyClaims, &models.TokenClaims{
		UserID:           "user-1",
		FullName:         "Paulo Abreu",
		OrganizationRole: models.RoleMaintainer,
	})
	c.Request = req.WithContext(ctx)
	h.GetPullRequest(c)
	c.Writer.WriteHeaderNow()
	return rec
}

func decodeDetail(t *testing.T, rec *httptest.ResponseRecorder) models.PullRequestDetailResponse {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body)
	}
	var got models.PullRequestDetailResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return got
}

func TestGetPullRequest_FlagsAChangeRequestTheCallerAuthored(t *testing.T) {
	store := &detailStore{conn: &models.OAuthConnection{ProviderUsername: "paulozy"}}
	h := newDetailHandler(store, &detailProvider{pr: scm.ChangeRequest{Number: 412, AuthorLogin: "paulozy"}})

	got := decodeDetail(t, detailRequest(h))
	if got.PullRequest.ReviewBlockedReason == nil {
		t.Fatal("ReviewBlockedReason is nil, want the caller's own change request flagged")
	}
	if *got.PullRequest.ReviewBlockedReason != ReviewBlockedSelfAuthored {
		t.Errorf("reason = %q, want %q", *got.PullRequest.ReviewBlockedReason, ReviewBlockedSelfAuthored)
	}
}

// Provider logins are not case-sensitive on either host, so neither is this.
func TestGetPullRequest_MatchesTheAuthorLoginCaseInsensitively(t *testing.T) {
	store := &detailStore{conn: &models.OAuthConnection{ProviderUsername: "PauloZy"}}
	h := newDetailHandler(store, &detailProvider{pr: scm.ChangeRequest{Number: 412, AuthorLogin: "paulozy"}})

	got := decodeDetail(t, detailRequest(h))
	if got.PullRequest.ReviewBlockedReason == nil {
		t.Fatal("ReviewBlockedReason is nil, want a case-insensitive match")
	}
}

func TestGetPullRequest_LeavesSomeoneElsesChangeRequestReviewable(t *testing.T) {
	store := &detailStore{conn: &models.OAuthConnection{ProviderUsername: "paulozy"}}
	h := newDetailHandler(store, &detailProvider{pr: scm.ChangeRequest{Number: 412, AuthorLogin: "julia.r"}})

	got := decodeDetail(t, detailRequest(h))
	if got.PullRequest.ReviewBlockedReason != nil {
		t.Errorf("ReviewBlockedReason = %q, want nil", *got.PullRequest.ReviewBlockedReason)
	}
}

// The gaps are ordinary: password signups have no OAuth connection, and
// connections predating migration 029 have no username until the next login.
// Neither is an error, and neither may block the read.
func TestGetPullRequest_UnknownCallerIdentityIsNotABlock(t *testing.T) {
	tests := []struct {
		name  string
		store *detailStore
	}{
		{name: "no oauth connection", store: &detailStore{}},
		{name: "connection without a username", store: &detailStore{conn: &models.OAuthConnection{}}},
		{name: "lookup failed", store: &detailStore{connErr: errors.New("database is down")}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newDetailHandler(tt.store, &detailProvider{pr: scm.ChangeRequest{Number: 412, AuthorLogin: "paulozy"}})

			got := decodeDetail(t, detailRequest(h))
			if got.PullRequest.ReviewBlockedReason != nil {
				t.Errorf("ReviewBlockedReason = %q, want nil when the identity is unknown", *got.PullRequest.ReviewBlockedReason)
			}
		})
	}
}

// The field must be present as null rather than absent: a client treating the
// key as required would read "missing" as "reviewable".
func TestGetPullRequest_SerializesTheReasonAsNullNotAbsent(t *testing.T) {
	h := newDetailHandler(&detailStore{}, &detailProvider{pr: scm.ChangeRequest{Number: 412, AuthorLogin: "julia.r"}})

	rec := detailRequest(h)
	if !strings.Contains(rec.Body.String(), `"review_blocked_reason":null`) {
		t.Errorf("body = %s, want review_blocked_reason present and null", rec.Body)
	}
}

// The detail read must not spend a request asking who the token belongs to.
// That cost is only justified once a rejection has actually happened.
func TestGetPullRequest_DoesNotAskTheHostWhoTheTokenIs(t *testing.T) {
	provider := &detailProvider{pr: scm.ChangeRequest{Number: 412, AuthorLogin: "paulozy"}}
	h := newDetailHandler(&detailStore{conn: &models.OAuthConnection{ProviderUsername: "paulozy"}}, provider)

	detailRequest(h)
	if provider.identityHits != 0 {
		t.Errorf("CurrentUser was called %d times on the happy path, want 0", provider.identityHits)
	}
}

// reviewWriteProvider rejects an approval and can report the token's identity,
// which is how the 409 gets the token owner's name.
type reviewWriteProvider struct {
	scm.Provider
	approveErr   error
	identity     *scm.Identity
	identityErr  error
	identityHits int
}

func (p *reviewWriteProvider) ApproveChangeRequest(_ context.Context, _ scm.RepoRef, _ int64, _ string) error {
	return p.approveErr
}

func (p *reviewWriteProvider) CurrentUser(_ context.Context) (*scm.Identity, error) {
	p.identityHits++
	return p.identity, p.identityErr
}

func newSelfReviewHandler(provider scm.Provider) *PullRequestHandler {
	h := NewPullRequestHandler(&detailStore{}, scm.Credentials{})
	h.resolve = func(_ models.RepositoryType, _ scm.Credentials) (scm.Provider, error) {
		return provider, nil
	}
	return h
}

func selfReviewRejection() *scm.ProviderError {
	return &scm.ProviderError{
		Provider: "github",
		Status:   http.StatusUnprocessableEntity,
		Reason:   scm.ReasonSelfReview,
		Message:  "Review Can not approve your own pull request",
	}
}

// The case the user actually hit: the organization's token opened the change
// request, so the host refuses an approval the caller had every right to make.
// Naming the token's owner is the difference between a dead end and a fix.
func TestApprovePullRequest_ResolvesTheTokenOwnerAfterASelfReviewRejection(t *testing.T) {
	provider := &reviewWriteProvider{
		approveErr: selfReviewRejection(),
		identity:   &scm.Identity{Login: "paulozy"},
	}
	h := newSelfReviewHandler(provider)

	rec := reviewRequest(h.ApprovePullRequest, models.RoleMaintainer, "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body: %s)", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "@paulozy") {
		t.Errorf("body = %s, want the token owner named", rec.Body)
	}
	if provider.identityHits != 1 {
		t.Errorf("CurrentUser called %d times, want exactly 1", provider.identityHits)
	}
}

// Losing the identity lookup costs detail, never the response. The rejection is
// already established by the time it runs.
func TestApprovePullRequest_DegradesWhenTheIdentityCannotBeResolved(t *testing.T) {
	h := newSelfReviewHandler(&reviewWriteProvider{
		approveErr:  selfReviewRejection(),
		identityErr: errors.New("token lacks read:user"),
	})

	rec := reviewRequest(h.ApprovePullRequest, models.RoleMaintainer, "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body: %s)", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "self_review") {
		t.Errorf("body = %s, want the self_review code even without the owner's name", rec.Body)
	}
}

// Only a self-review rejection justifies the extra request. Everything else —
// other refusals, outages — must go straight through.
func TestApprovePullRequest_DoesNotResolveIdentityForOtherFailures(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{name: "another provider refusal", err: &scm.ProviderError{Provider: "github", Status: http.StatusConflict, Message: "already merged"}},
		{name: "an outage", err: scm.ErrUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := &reviewWriteProvider{approveErr: tt.err, identity: &scm.Identity{Login: "paulozy"}}
			h := newSelfReviewHandler(provider)

			reviewRequest(h.ApprovePullRequest, models.RoleMaintainer, "")
			if provider.identityHits != 0 {
				t.Errorf("CurrentUser was called %d times, want 0", provider.identityHits)
			}
		})
	}
}

// reviewStateProvider serves a change-request list plus per-PR review state,
// counting the calls so the fan-out's bounds can be asserted.
type reviewStateProvider struct {
	scm.Provider
	prs        []scm.ChangeRequest
	state      *scm.ReviewState
	stateErr   error
	mu         sync.Mutex
	stateCalls int
}

func (p *reviewStateProvider) ListChangeRequests(_ context.Context, _ scm.RepoRef) ([]scm.ChangeRequest, error) {
	return p.prs, nil
}

func (p *reviewStateProvider) GetChangeRequest(_ context.Context, _ scm.RepoRef, n int64) (*scm.ChangeRequest, error) {
	return &scm.ChangeRequest{Number: n, AuthorLogin: "julia.r"}, nil
}

func (p *reviewStateProvider) GetChangeRequestFiles(_ context.Context, _ scm.RepoRef, _ int64) ([]scm.ChangeRequestFile, error) {
	return nil, nil
}

func (p *reviewStateProvider) GetChangeRequestReviews(_ context.Context, _ scm.RepoRef, _ int64) (*scm.ReviewState, error) {
	p.mu.Lock()
	p.stateCalls++
	p.mu.Unlock()
	return p.state, p.stateErr
}

func (p *reviewStateProvider) calls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.stateCalls
}

func listRequest(h *PullRequestHandler) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Params = gin.Params{{Key: "id", Value: "repo-1"}}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	c.Request = req.WithContext(context.WithValue(req.Context(), utils.ContextKeyOrganization, browseOrgID))
	h.ListPullRequests(c)
	c.Writer.WriteHeaderNow()
	return rec
}

// Approving used to leave no trace on screen. The verdict now travels with the
// change request, which is what lets the UI show a state instead of a toast.
func TestGetPullRequest_CarriesTheReviewVerdict(t *testing.T) {
	h := newDetailHandler(&detailStore{}, &reviewStateProvider{
		state: &scm.ReviewState{Decision: scm.ReviewDecisionApproved, ApprovedBy: []string{"julia.r"}},
	})

	got := decodeDetail(t, detailRequest(h))
	if got.PullRequest.ReviewDecision == nil {
		t.Fatal("ReviewDecision is nil, want the recorded verdict")
	}
	if *got.PullRequest.ReviewDecision != scm.ReviewDecisionApproved {
		t.Errorf("decision = %q, want approved", *got.PullRequest.ReviewDecision)
	}
	if len(got.PullRequest.ApprovedBy) != 1 || got.PullRequest.ApprovedBy[0] != "julia.r" {
		t.Errorf("ApprovedBy = %v, want [julia.r]", got.PullRequest.ApprovedBy)
	}
}

// "Nobody reviewed" is a measured fact and must not look like "we could not
// ask" — the client renders the two differently.
func TestGetPullRequest_DistinguishesUnreviewedFromUnknown(t *testing.T) {
	unreviewed := newDetailHandler(&detailStore{}, &reviewStateProvider{state: &scm.ReviewState{}})
	got := decodeDetail(t, detailRequest(unreviewed))
	if got.PullRequest.ReviewDecision == nil {
		t.Fatal("ReviewDecision is nil, want an empty-string verdict for a measured zero")
	}
	if *got.PullRequest.ReviewDecision != "" {
		t.Errorf("decision = %q, want empty", *got.PullRequest.ReviewDecision)
	}

	unknown := newDetailHandler(&detailStore{}, &reviewStateProvider{stateErr: errors.New("provider down")})
	got = decodeDetail(t, detailRequest(unknown))
	if got.PullRequest.ReviewDecision != nil {
		t.Errorf("ReviewDecision = %q, want nil when the host could not be asked", *got.PullRequest.ReviewDecision)
	}
}

// A failed review-state read must not fail the page. The change request loaded
// fine; a missing badge beats a 503.
func TestGetPullRequest_ReviewStateFailureDoesNotFailTheRead(t *testing.T) {
	h := newDetailHandler(&detailStore{}, &reviewStateProvider{stateErr: errors.New("provider down")})

	if rec := detailRequest(h); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 despite the review-state failure (body: %s)", rec.Code, rec.Body)
	}
}

func TestListPullRequests_CarriesTheVerdictForEachItem(t *testing.T) {
	provider := &reviewStateProvider{
		prs:   []scm.ChangeRequest{{Number: 1}, {Number: 2}, {Number: 3}},
		state: &scm.ReviewState{Decision: scm.ReviewDecisionChangesRequested, ChangesRequestedBy: []string{"caio"}},
	}
	h := newDetailHandler(&detailStore{}, provider)

	rec := listRequest(h)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body)
	}
	var got models.PullRequestListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Items) != 3 {
		t.Fatalf("got %d items, want 3", len(got.Items))
	}
	for _, item := range got.Items {
		if item.PullRequest.ReviewDecision == nil || *item.PullRequest.ReviewDecision != scm.ReviewDecisionChangesRequested {
			t.Errorf("PR #%d decision = %v, want changes_requested", item.PullRequest.Number, item.PullRequest.ReviewDecision)
		}
	}
	if provider.calls() != 3 {
		t.Errorf("review state was read %d times, want one per listed change request", provider.calls())
	}
}

// Neither host reports review state on its list endpoint, so this costs one
// request per change request. The ceiling is what keeps a busy repository from
// turning one page view into a hundred API calls.
func TestListPullRequests_StopsReadingReviewStateAtTheCeiling(t *testing.T) {
	prs := make([]scm.ChangeRequest, reviewStateListCeiling+12)
	for i := range prs {
		prs[i] = scm.ChangeRequest{Number: int64(i + 1)}
	}
	provider := &reviewStateProvider{prs: prs, state: &scm.ReviewState{Decision: scm.ReviewDecisionApproved}}
	h := newDetailHandler(&detailStore{}, provider)

	rec := listRequest(h)
	var got models.PullRequestListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Every change request is still listed — only the extra verdicts are skipped.
	if len(got.Items) != len(prs) {
		t.Fatalf("got %d items, want all %d listed", len(got.Items), len(prs))
	}
	if provider.calls() != reviewStateListCeiling {
		t.Errorf("review state was read %d times, want the ceiling of %d", provider.calls(), reviewStateListCeiling)
	}
	// The ones past the ceiling report unknown, not "not reviewed".
	if got.Items[len(prs)-1].PullRequest.ReviewDecision != nil {
		t.Error("a change request past the ceiling must report an unknown verdict, not a measured one")
	}
}
