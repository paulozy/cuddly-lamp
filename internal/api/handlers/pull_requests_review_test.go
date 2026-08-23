package handlers

import (
	"context"
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
