package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
	"github.com/paulozy/idp-with-ai-backend/internal/utils"
)

type templateHandlerRepo struct {
	storage.Repository
	template        *models.CodeTemplate
	deletedID       string
	deleteErr       error
	deleteCallCount int
}

func (r *templateHandlerRepo) GetCodeTemplate(ctx context.Context, id string) (*models.CodeTemplate, error) {
	if r.template == nil {
		return nil, nil
	}
	if r.template.ID != id {
		return nil, nil
	}
	return r.template, nil
}

func (r *templateHandlerRepo) DeleteCodeTemplate(ctx context.Context, id string) error {
	r.deleteCallCount++
	if r.deleteErr != nil {
		return r.deleteErr
	}
	r.deletedID = id
	return nil
}

// buildDeleteTemplateRouter wires a minimal gin engine with the DELETE route
// and a middleware that injects the (optional) organization id into context.
// Using a real router (vs gin.CreateTestContext) ensures the response status is
// flushed correctly for 204 No Content.
func buildDeleteTemplateRouter(handler *TemplateHandler, orgID string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if orgID != "" {
			c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), utils.ContextKeyOrganization, orgID))
		}
		c.Next()
	})
	r.DELETE("/templates/:id", handler.DeleteTemplate)
	return r
}

func performDeleteTemplate(t *testing.T, repo *templateHandlerRepo, templateID, orgID string) *httptest.ResponseRecorder {
	t.Helper()
	handler := NewTemplateHandler(repo, nil)
	r := buildDeleteTemplateRouter(handler, orgID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/templates/"+templateID, nil))
	return w
}

func TestDeleteTemplate_Failed_Returns204AndSoftDeletes(t *testing.T) {
	repo := &templateHandlerRepo{
		template: &models.CodeTemplate{
			ID:             "tmpl-1",
			OrganizationID: "org-1",
			Status:         models.TemplateStatusFailed,
		},
	}

	w := performDeleteTemplate(t, repo, "tmpl-1", "org-1")

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body=%s", w.Code, w.Body.String())
	}
	if repo.deletedID != "tmpl-1" {
		t.Fatalf("DeleteCodeTemplate not called with tmpl-1; got %q", repo.deletedID)
	}
}

func TestDeleteTemplate_Completed_Returns204(t *testing.T) {
	repo := &templateHandlerRepo{
		template: &models.CodeTemplate{
			ID:             "tmpl-2",
			OrganizationID: "org-1",
			Status:         models.TemplateStatusCompleted,
		},
	}

	w := performDeleteTemplate(t, repo, "tmpl-2", "org-1")

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body=%s", w.Code, w.Body.String())
	}
}

func TestDeleteTemplate_Completed_Pinned_Returns204(t *testing.T) {
	repo := &templateHandlerRepo{
		template: &models.CodeTemplate{
			ID:             "tmpl-3",
			OrganizationID: "org-1",
			Status:         models.TemplateStatusCompleted,
			IsPinned:       true,
		},
	}

	w := performDeleteTemplate(t, repo, "tmpl-3", "org-1")

	if w.Code != http.StatusNoContent {
		t.Fatalf("pinned templates should also be deletable; status = %d, body=%s", w.Code, w.Body.String())
	}
}

func TestDeleteTemplate_Pending_Returns409(t *testing.T) {
	repo := &templateHandlerRepo{
		template: &models.CodeTemplate{
			ID:             "tmpl-4",
			OrganizationID: "org-1",
			Status:         models.TemplateStatusPending,
		},
	}

	w := performDeleteTemplate(t, repo, "tmpl-4", "org-1")

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", w.Code, w.Body.String())
	}
	if repo.deleteCallCount != 0 {
		t.Fatalf("DeleteCodeTemplate should not have been called; got %d calls", repo.deleteCallCount)
	}
}

func TestDeleteTemplate_Generating_Returns409(t *testing.T) {
	repo := &templateHandlerRepo{
		template: &models.CodeTemplate{
			ID:             "tmpl-5",
			OrganizationID: "org-1",
			Status:         models.TemplateStatusGenerating,
		},
	}

	w := performDeleteTemplate(t, repo, "tmpl-5", "org-1")

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", w.Code, w.Body.String())
	}
}

func TestDeleteTemplate_OtherOrg_Returns403(t *testing.T) {
	repo := &templateHandlerRepo{
		template: &models.CodeTemplate{
			ID:             "tmpl-6",
			OrganizationID: "org-other",
			Status:         models.TemplateStatusFailed,
		},
	}

	w := performDeleteTemplate(t, repo, "tmpl-6", "org-1")

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body=%s", w.Code, w.Body.String())
	}
}

func TestDeleteTemplate_NotFound_Returns404(t *testing.T) {
	repo := &templateHandlerRepo{template: nil}

	w := performDeleteTemplate(t, repo, "missing", "org-1")

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", w.Code, w.Body.String())
	}
}

func TestDeleteTemplate_Unauthenticated_Returns401(t *testing.T) {
	repo := &templateHandlerRepo{
		template: &models.CodeTemplate{
			ID:             "tmpl-7",
			OrganizationID: "org-1",
			Status:         models.TemplateStatusFailed,
		},
	}

	// no organization in context
	w := performDeleteTemplate(t, repo, "tmpl-7", "")

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body=%s", w.Code, w.Body.String())
	}
}

func TestDeleteTemplate_StorageError_Returns500(t *testing.T) {
	repo := &templateHandlerRepo{
		template: &models.CodeTemplate{
			ID:             "tmpl-8",
			OrganizationID: "org-1",
			Status:         models.TemplateStatusFailed,
		},
		deleteErr: errors.New("db unavailable"),
	}

	w := performDeleteTemplate(t, repo, "tmpl-8", "org-1")

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}
