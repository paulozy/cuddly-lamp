package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/paulozy/idp-with-ai-backend/internal/integrations/scm"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/utils"
)

func newReviewHandler(provider scm.Provider) *PullRequestHandler {
	store := &browseStore{repo: &models.Repository{
		ID:             "repo-1",
		OrganizationID: browseOrgID,
		URL:            "https://github.com/owner/repo",
	}}
	h := NewPullRequestHandler(store, scm.Credentials{})
	h.resolve = func(_ models.RepositoryType, _ scm.Credentials) (scm.Provider, error) {
		return provider, nil
	}
	return h
}

func reviewRequest(h func(*gin.Context), role models.UserRole, body string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Params = gin.Params{{Key: "id", Value: "repo-1"}, {Key: "pr_number", Value: "412"}}

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx := context.WithValue(req.Context(), utils.ContextKeyOrganization, browseOrgID)
	ctx = context.WithValue(ctx, utils.ContextKeyClaims, &models.TokenClaims{
		UserID:           "user-1",
		FullName:         "Paulo Abreu",
		OrganizationRole: role,
	})
	c.Request = req.WithContext(ctx)
	h(c)
	c.Writer.WriteHeaderNow()
	return rec
}

func TestApprovePullRequest_StampsTheRealActorOntoTheReview(t *testing.T) {
	provider := &writeProvider{}
	h := newReviewHandler(provider)

	rec := reviewRequest(h.ApprovePullRequest, models.RoleMaintainer, `{"body":"Looks good."}`)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 (body: %s)", rec.Code, rec.Body)
	}
	if provider.approved != 412 {
		t.Errorf("approved = %d, want 412", provider.approved)
	}
	// The API call runs as the organization's token, so the person who actually
	// clicked has to survive in the message or the record is misleading.
	if !strings.Contains(provider.approvedBody, "Paulo Abreu") {
		t.Errorf("review body = %q, want it to name the acting user", provider.approvedBody)
	}
	if !strings.Contains(provider.approvedBody, "Looks good.") {
		t.Errorf("review body = %q, want it to keep the author's message", provider.approvedBody)
	}
}

func TestApprovePullRequest_AttributesEvenWithoutAMessage(t *testing.T) {
	provider := &writeProvider{}
	h := newReviewHandler(provider)

	if rec := reviewRequest(h.ApprovePullRequest, models.RoleMaintainer, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if !strings.Contains(provider.approvedBody, "Paulo Abreu") {
		t.Errorf("review body = %q, want it to name the acting user", provider.approvedBody)
	}
}

func TestApprovePullRequest_ViewerIsForbidden(t *testing.T) {
	provider := &writeProvider{}
	h := newReviewHandler(provider)

	rec := reviewRequest(h.ApprovePullRequest, models.RoleViewer, "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if provider.approved != 0 {
		t.Error("the provider was called despite the caller being refused")
	}
}

// GitLab has no portable equivalent of REQUEST_CHANGES. That must surface as
// "this host cannot do that" (501), not as a generic provider failure — the UI
// hides the affordance on the strength of this distinction.
func TestRequestChanges_UnsupportedProviderReportsNotImplemented(t *testing.T) {
	h := newReviewHandler(&writeProvider{requestErr: scm.ErrUnsupportedCapability})

	rec := reviewRequest(h.RequestPullRequestChanges, models.RoleMaintainer, "")
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501 (body: %s)", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "unsupported_capability") {
		t.Errorf("body = %s, want the unsupported_capability code", rec.Body)
	}
}

func TestRequestChanges_SupportedProviderSucceeds(t *testing.T) {
	h := newReviewHandler(&writeProvider{})

	if rec := reviewRequest(h.RequestPullRequestChanges, models.RoleMaintainer, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
}

// The bug this whole change exists for. GitHub refuses a self-approval with a
// 422; the platform used to report that as 503 "provider unavailable", which
// says "retry later" about something that can never succeed, and blamed the
// organization's token, which is perfectly valid.
func TestApprovePullRequest_SelfReviewIsAConflictNotAnOutage(t *testing.T) {
	h := newReviewHandler(&writeProvider{approveErr: &scm.ProviderError{
		Provider: "github",
		Status:   http.StatusUnprocessableEntity,
		Reason:   scm.ReasonSelfReview,
		Message:  "Review Can not approve your own pull request",
	}})

	rec := reviewRequest(h.ApprovePullRequest, models.RoleMaintainer, "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body: %s)", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "self_review") {
		t.Errorf("body = %s, want the self_review code", rec.Body)
	}
	// The description has to explain the platform's situation, not repeat the
	// host's wording — the author is usually the org token, not the clicker.
	if !strings.Contains(rec.Body.String(), "token") {
		t.Errorf("body = %s, want it to explain whose identity acted", rec.Body)
	}
}

// A named token owner is the whole point of enriching the failure: it turns a
// dead end into something the reader can act on.
func TestApprovePullRequest_SelfReviewNamesTheTokenOwner(t *testing.T) {
	h := newReviewHandler(&writeProvider{approveErr: &scm.ProviderError{
		Provider:   "github",
		Status:     http.StatusUnprocessableEntity,
		Reason:     scm.ReasonSelfReview,
		Message:    "Review Can not approve your own pull request",
		TokenOwner: "paulozy",
	}})

	rec := reviewRequest(h.ApprovePullRequest, models.RoleMaintainer, "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "@paulozy") {
		t.Errorf("body = %s, want the token owner named", rec.Body)
	}
}

// Every other refusal the host spelled out: pass its words through, still 409.
func TestApprovePullRequest_OtherRefusalsKeepTheHostsWords(t *testing.T) {
	h := newReviewHandler(&writeProvider{approveErr: &scm.ProviderError{
		Provider: "github",
		Status:   http.StatusUnprocessableEntity,
		Message:  "No commits between main and feature",
	}})

	rec := reviewRequest(h.ApprovePullRequest, models.RoleMaintainer, "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body: %s)", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "provider_rejected") {
		t.Errorf("body = %s, want the provider_rejected code", rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "No commits between") {
		t.Errorf("body = %s, want the host's explanation preserved", rec.Body)
	}
}

// The other half of the distinction: a real outage must stay a 503, or the fix
// has just moved the lie somewhere else.
func TestApprovePullRequest_RealOutageStaysUnavailable(t *testing.T) {
	h := newReviewHandler(&writeProvider{approveErr: scm.ErrUnauthorized})

	rec := reviewRequest(h.ApprovePullRequest, models.RoleMaintainer, "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (body: %s)", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "provider_unavailable") {
		t.Errorf("body = %s, want the provider_unavailable code", rec.Body)
	}
}

// An unclassified failure must not put the provider's raw payload in the
// response. It is the case where that text is least likely to mean anything and
// most likely to leak.
func TestApprovePullRequest_UnclassifiedFailureDoesNotLeakRawText(t *testing.T) {
	h := newReviewHandler(&writeProvider{approveErr: errors.New("dial tcp 10.0.0.5:443: connect: connection refused")})

	rec := reviewRequest(h.ApprovePullRequest, models.RoleMaintainer, "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "10.0.0.5") {
		t.Errorf("body = %s, want no raw provider/transport detail", rec.Body)
	}
}
