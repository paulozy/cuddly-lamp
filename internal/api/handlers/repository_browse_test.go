package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/paulozy/idp-with-ai-backend/internal/integrations/scm"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
	"github.com/paulozy/idp-with-ai-backend/internal/utils"
)

// browseStore serves the two lookups scmResolver performs. Everything else
// comes from the embedded nil interface and would panic if reached.
type browseStore struct {
	storage.Repository
	repo *models.Repository
}

func (s *browseStore) GetRepository(_ context.Context, id string) (*models.Repository, error) {
	if s.repo == nil || s.repo.ID != id {
		return nil, nil
	}
	return s.repo, nil
}

func (s *browseStore) GetOrganizationConfig(_ context.Context, _ string) (*models.OrganizationConfig, error) {
	return &models.OrganizationConfig{}, nil
}

// browseProvider stubs the provider so no test reaches the network. Only the
// three methods the handler calls are meaningful.
type browseProvider struct {
	scm.Provider
	issues       []scm.Issue
	contributors []scm.Contributor
	changeReqs   []scm.ChangeRequest
	commits      []scm.Commit
	issuesErr    error
	commitsErr   error
	changeErr    error
}

func (p *browseProvider) ListIssues(_ context.Context, _ scm.RepoRef) ([]scm.Issue, error) {
	return p.issues, p.issuesErr
}

func (p *browseProvider) ListContributors(_ context.Context, _ scm.RepoRef) ([]scm.Contributor, error) {
	return p.contributors, nil
}

func (p *browseProvider) ListChangeRequests(_ context.Context, _ scm.RepoRef) ([]scm.ChangeRequest, error) {
	return p.changeReqs, p.changeErr
}

func (p *browseProvider) ListCommits(_ context.Context, _ scm.RepoRef, _ string, _ int) ([]scm.Commit, error) {
	return p.commits, p.commitsErr
}

const browseOrgID = "org-1"

func newBrowseHandler(provider scm.Provider) *RepositoryBrowseHandler {
	store := &browseStore{repo: &models.Repository{
		ID:             "repo-1",
		OrganizationID: browseOrgID,
		URL:            "https://github.com/owner/repo",
		Metadata:       models.RepositoryMetadata{DefaultBranch: "main"},
	}}
	h := NewRepositoryBrowseHandler(store, scm.Credentials{})
	h.resolve = func(_ models.RepositoryType, _ scm.Credentials) (scm.Provider, error) {
		return provider, nil
	}
	return h
}

// browseRequest drives one handler call and returns the recorder.
func browseRequest(h func(*gin.Context), repoID, orgID string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Params = gin.Params{{Key: "id", Value: repoID}}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if orgID != "" {
		req = req.WithContext(context.WithValue(req.Context(), utils.ContextKeyOrganization, orgID))
	}
	c.Request = req
	h(c)
	return rec
}

func TestListIssues_ReturnsOpenIssues(t *testing.T) {
	h := newBrowseHandler(&browseProvider{issues: []scm.Issue{{
		Number:        88,
		Title:         "Sidebar does not collapse",
		State:         scm.IssueStateOpen,
		AuthorLogin:   "julia.r",
		Labels:        []string{"bug", "ui"},
		CommentsCount: 3,
	}}})

	rec := browseRequest(h.ListIssues, "repo-1", browseOrgID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body)
	}

	var got models.IssueListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Total != 1 || len(got.Items) != 1 {
		t.Fatalf("got %d items, want 1", len(got.Items))
	}
	if got.Items[0].Number != 88 || got.Items[0].CommentsCount != 3 {
		t.Errorf("item = %+v, want issue 88 with 3 comments", got.Items[0])
	}
}

// A repository with no issues must serialize as [], not null: the client's
// schema types this as a list and null would fail it.
func TestListIssues_EmptySerializesAsArray(t *testing.T) {
	h := newBrowseHandler(&browseProvider{issues: nil})

	rec := browseRequest(h.ListIssues, "repo-1", browseOrgID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var raw map[string]json.RawMessage
	json.Unmarshal(rec.Body.Bytes(), &raw)
	if string(raw["items"]) != "[]" {
		t.Errorf("items = %s, want []", raw["items"])
	}
}

func TestListIssues_RefusesAnotherOrganizationsRepository(t *testing.T) {
	h := newBrowseHandler(&browseProvider{})

	rec := browseRequest(h.ListIssues, "repo-1", "org-2")
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}

func TestListIssues_ProviderFailureIsUnavailable(t *testing.T) {
	h := newBrowseHandler(&browseProvider{issuesErr: scm.ErrRateLimited})

	rec := browseRequest(h.ListIssues, "repo-1", browseOrgID)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestListContributors_DerivesChangeRequestsAndLastCommit(t *testing.T) {
	h := newBrowseHandler(&browseProvider{
		contributors: []scm.Contributor{{Login: "paulozy", Commits: 240}},
		changeReqs: []scm.ChangeRequest{
			{Number: 1, AuthorLogin: "paulozy"},
			{Number: 2, AuthorLogin: "paulozy"},
			{Number: 3, AuthorLogin: "ana-m"},
		},
		commits: []scm.Commit{
			{AuthorName: "paulozy", Date: time.Date(2026, 8, 21, 10, 0, 0, 0, time.UTC)},
			{AuthorName: "paulozy", Date: time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)},
		},
	})

	rec := browseRequest(h.ListContributors, "repo-1", browseOrgID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body)
	}

	var got models.ContributorListResponse
	json.Unmarshal(rec.Body.Bytes(), &got)
	if len(got.Items) != 1 {
		t.Fatalf("got %d contributors, want 1", len(got.Items))
	}
	item := got.Items[0]
	if item.OpenChangeRequests == nil || *item.OpenChangeRequests != 2 {
		t.Errorf("open_change_requests = %v, want 2", item.OpenChangeRequests)
	}
	// Commits arrive newest first, so the first sighting is the last activity.
	if item.LastCommitAt == nil || *item.LastCommitAt != "2026-08-21T10:00:00Z" {
		t.Errorf("last_commit_at = %v, want 2026-08-21T10:00:00Z", item.LastCommitAt)
	}
}

// GitLab reports a contributor's display name and no username, so a lookup
// keyed on login has to fall through to the name.
func TestListContributors_MatchesOnNameWhenThereIsNoLogin(t *testing.T) {
	h := newBrowseHandler(&browseProvider{
		contributors: []scm.Contributor{{Name: "Paulo Abreu", Commits: 240}},
		commits: []scm.Commit{
			{AuthorName: "paulo abreu", Date: time.Date(2026, 8, 22, 9, 0, 0, 0, time.UTC)},
		},
	})

	rec := browseRequest(h.ListContributors, "repo-1", browseOrgID)
	var got models.ContributorListResponse
	json.Unmarshal(rec.Body.Bytes(), &got)

	if len(got.Items) != 1 {
		t.Fatalf("got %d contributors, want 1", len(got.Items))
	}
	if got.Items[0].LastCommitAt == nil {
		t.Fatal("last_commit_at = nil, want a date matched on the display name")
	}
	if *got.Items[0].LastCommitAt != "2026-08-22T09:00:00Z" {
		t.Errorf("last_commit_at = %q, want 2026-08-22T09:00:00Z", *got.Items[0].LastCommitAt)
	}
}

// The distinction the pointers exist for: when the enrichment call itself
// fails we know nothing, and the payload must say null rather than 0.
func TestListContributors_UnavailableEnrichmentIsNullNotZero(t *testing.T) {
	h := newBrowseHandler(&browseProvider{
		contributors: []scm.Contributor{{Login: "paulozy", Commits: 240}},
		changeErr:    errors.New("boom"),
		commitsErr:   errors.New("boom"),
	})

	rec := browseRequest(h.ListContributors, "repo-1", browseOrgID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 — an enrichment failure must not fail the list", rec.Code)
	}

	var got models.ContributorListResponse
	json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Items[0].OpenChangeRequests != nil {
		t.Errorf("open_change_requests = %v, want null", *got.Items[0].OpenChangeRequests)
	}
	if got.Items[0].LastCommitAt != nil {
		t.Errorf("last_commit_at = %v, want null", *got.Items[0].LastCommitAt)
	}
	// Commits come from the contributors endpoint itself, so they survive.
	if got.Items[0].Commits != 240 {
		t.Errorf("commits = %d, want 240", got.Items[0].Commits)
	}
}

// A contributor with no open change requests, on a repository whose list we
// did read, is a real zero — not an unknown.
func TestListContributors_NoChangeRequestsIsZeroNotNull(t *testing.T) {
	h := newBrowseHandler(&browseProvider{
		contributors: []scm.Contributor{{Login: "quiet-dev", Commits: 4}},
		changeReqs:   []scm.ChangeRequest{{Number: 1, AuthorLogin: "someone-else"}},
	})

	rec := browseRequest(h.ListContributors, "repo-1", browseOrgID)
	var got models.ContributorListResponse
	json.Unmarshal(rec.Body.Bytes(), &got)

	if got.Items[0].OpenChangeRequests == nil {
		t.Fatal("open_change_requests = null, want 0 — the list was read successfully")
	}
	if *got.Items[0].OpenChangeRequests != 0 {
		t.Errorf("open_change_requests = %d, want 0", *got.Items[0].OpenChangeRequests)
	}
}

// ── write actions ─────────────────────────────────────────────────────────────

// browseWriteRequest drives a write handler as a user with the given role.
func browseWriteRequest(h func(*gin.Context), params gin.Params, role models.UserRole, body string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Params = params

	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(http.MethodPost, "/", reader)
	req.Header.Set("Content-Type", "application/json")

	ctx := context.WithValue(req.Context(), utils.ContextKeyOrganization, browseOrgID)
	ctx = context.WithValue(ctx, utils.ContextKeyClaims, &models.TokenClaims{
		UserID:           "user-1",
		FullName:         "Paulo Abreu",
		OrganizationRole: role,
	})
	c.Request = req.WithContext(ctx)
	h(c)
	// gin defers the status write to the end of its handler chain, which a
	// direct call bypasses; without this a 204 reaches the recorder as 200.
	c.Writer.WriteHeaderNow()
	return rec
}

func TestCloseIssue_MaintainerSucceeds(t *testing.T) {
	provider := &writeProvider{}
	h := newBrowseHandler(provider)

	rec := browseWriteRequest(h.CloseIssue,
		gin.Params{{Key: "id", Value: "repo-1"}, {Key: "number", Value: "88"}},
		models.RoleMaintainer, "")

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 (body: %s)", rec.Code, rec.Body)
	}
	if provider.closedIssue != 88 {
		t.Errorf("closed issue = %d, want 88", provider.closedIssue)
	}
}

// A viewer is below the ownership floor, so ownership cannot grant the action.
func TestCloseIssue_ViewerIsForbidden(t *testing.T) {
	provider := &writeProvider{}
	h := newBrowseHandler(provider)

	rec := browseWriteRequest(h.CloseIssue,
		gin.Params{{Key: "id", Value: "repo-1"}, {Key: "number", Value: "88"}},
		models.RoleViewer, "")

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if provider.closedIssue != 0 {
		t.Error("the provider was called despite the caller being refused")
	}
}

func TestCloseIssue_RejectsNonNumericIssue(t *testing.T) {
	h := newBrowseHandler(&writeProvider{})

	rec := browseWriteRequest(h.CloseIssue,
		gin.Params{{Key: "id", Value: "repo-1"}, {Key: "number", Value: "abc"}},
		models.RoleMaintainer, "")

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// writeProvider records the mutations it was asked to perform.
type writeProvider struct {
	browseProvider
	closedIssue  int64
	approved     int64
	approvedBody string
	requestErr   error
	approveErr   error
}

func (p *writeProvider) CloseIssue(_ context.Context, _ scm.RepoRef, number int64) error {
	p.closedIssue = number
	return nil
}

func (p *writeProvider) ApproveChangeRequest(_ context.Context, _ scm.RepoRef, number int64, body string) error {
	if p.approveErr != nil {
		return p.approveErr
	}
	p.approved = number
	p.approvedBody = body
	return nil
}

// CurrentUser is reached when a self-review rejection is being explained. This
// double reports nothing, which exercises the degraded path: the 409 still goes
// out, just without naming the token's owner.
func (p *writeProvider) CurrentUser(_ context.Context) (*scm.Identity, error) {
	return nil, nil
}

func (p *writeProvider) RequestChanges(_ context.Context, _ scm.RepoRef, _ int64, _ string) error {
	if p.requestErr != nil {
		return p.requestErr
	}
	return nil
}
