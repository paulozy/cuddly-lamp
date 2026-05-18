package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
	"github.com/paulozy/idp-with-ai-backend/internal/utils"
	"gorm.io/datatypes"
)

type docsHandlerRepo struct {
	storage.Repository
	repo *models.Repository
	docs []models.DocGeneration
}

func (r *docsHandlerRepo) GetRepository(ctx context.Context, id string) (*models.Repository, error) {
	if r.repo == nil || r.repo.ID != id {
		return nil, nil
	}
	return r.repo, nil
}

func (r *docsHandlerRepo) ListDocGenerationsForRepo(ctx context.Context, repoID string) ([]models.DocGeneration, error) {
	out := make([]models.DocGeneration, 0, len(r.docs))
	for _, doc := range r.docs {
		if doc.RepositoryID != nil && *doc.RepositoryID == repoID {
			out = append(out, doc)
		}
	}
	return out, nil
}

func (r *docsHandlerRepo) GetDocGeneration(ctx context.Context, id string) (*models.DocGeneration, error) {
	for i := range r.docs {
		if r.docs[i].ID == id {
			doc := r.docs[i]
			return &doc, nil
		}
	}
	return nil, nil
}

func newDocsHandler(repo *docsHandlerRepo) *DocsHandler {
	return NewDocsHandler(repo, nil)
}

// repoIDPtr is a small helper so test fixtures can keep the concise
// `RepositoryID: repoIDPtr("repo-1")` style now that the model uses *string.
func repoIDPtr(id string) *string {
	return &id
}

func TestDocsHandler_ListRepositoryDocs_ReturnsSummaries(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Now().UTC()
	repo := &docsHandlerRepo{
		repo: &models.Repository{ID: "repo-1", OrganizationID: "org-1"},
		docs: []models.DocGeneration{
			{
				ID:                "doc-1",
				RepositoryID:      repoIDPtr("repo-1"),
				Status:            models.DocGenerationStatusCompleted,
				Types:             datatypes.JSONSlice[string]([]string{"adr", "architecture"}),
				PullRequestURL:    "https://github.com/org/repo/pull/42",
				PullRequestNumber: 42,
				TokensUsed:        1200,
				CreatedAt:         now,
				UpdatedAt:         now,
				Content:           datatypes.NewJSONType(map[string]string{"adr": "# ADR\nbig content"}),
			},
			{
				ID:           "doc-2",
				RepositoryID: repoIDPtr("repo-1"),
				Status:       models.DocGenerationStatusFailed,
				Types:        datatypes.JSONSlice[string]([]string{"guidelines"}),
				ErrorMessage: "anthropic timeout",
				CreatedAt:    now,
				UpdatedAt:    now,
			},
		},
	}
	handler := newDocsHandler(repo)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodGet, "/repositories/repo-1/docs", nil)
	req = req.WithContext(context.WithValue(req.Context(), utils.ContextKeyOrganization, "org-1"))
	c.Request = req
	c.Params = gin.Params{{Key: "id", Value: "repo-1"}}

	handler.ListRepositoryDocs(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var resp models.DocGenerationListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, w.Body.String())
	}
	if resp.Total != 2 {
		t.Fatalf("total = %d, want 2", resp.Total)
	}
	if len(resp.Items) != 2 {
		t.Fatalf("items len = %d, want 2", len(resp.Items))
	}
	// The summary projection MUST NOT leak the JSONB content blob.
	if got := string(w.Body.Bytes()); contains(got, "big content") {
		t.Fatalf("list response leaked content blob; body=%s", got)
	}
	if resp.Items[0].PullRequestNumber != 42 {
		t.Fatalf("pull request number = %d, want 42", resp.Items[0].PullRequestNumber)
	}
}

func TestDocsHandler_ListRepositoryDocs_FilterByStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Now().UTC()
	repo := &docsHandlerRepo{
		repo: &models.Repository{ID: "repo-1", OrganizationID: "org-1"},
		docs: []models.DocGeneration{
			{ID: "doc-1", RepositoryID: repoIDPtr("repo-1"), Status: models.DocGenerationStatusCompleted, CreatedAt: now, UpdatedAt: now},
			{ID: "doc-2", RepositoryID: repoIDPtr("repo-1"), Status: models.DocGenerationStatusFailed, CreatedAt: now, UpdatedAt: now},
		},
	}
	handler := newDocsHandler(repo)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodGet, "/repositories/repo-1/docs?status=completed", nil)
	req = req.WithContext(context.WithValue(req.Context(), utils.ContextKeyOrganization, "org-1"))
	c.Request = req
	c.Params = gin.Params{{Key: "id", Value: "repo-1"}}

	handler.ListRepositoryDocs(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var resp models.DocGenerationListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Total != 1 || resp.Items[0].ID != "doc-1" {
		t.Fatalf("unexpected filtered response: %+v", resp)
	}
}

func TestDocsHandler_ListRepositoryDocs_InvalidStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &docsHandlerRepo{repo: &models.Repository{ID: "repo-1", OrganizationID: "org-1"}}
	handler := newDocsHandler(repo)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodGet, "/repositories/repo-1/docs?status=banana", nil)
	req = req.WithContext(context.WithValue(req.Context(), utils.ContextKeyOrganization, "org-1"))
	c.Request = req
	c.Params = gin.Params{{Key: "id", Value: "repo-1"}}

	handler.ListRepositoryDocs(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
}

func TestDocsHandler_ListRepositoryDocs_ForbidsCrossOrgRepo(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &docsHandlerRepo{repo: &models.Repository{ID: "repo-1", OrganizationID: "org-other"}}
	handler := newDocsHandler(repo)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodGet, "/repositories/repo-1/docs", nil)
	req = req.WithContext(context.WithValue(req.Context(), utils.ContextKeyOrganization, "org-1"))
	c.Request = req
	c.Params = gin.Params{{Key: "id", Value: "repo-1"}}

	handler.ListRepositoryDocs(c)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body=%s", w.Code, w.Body.String())
	}
}

func TestDocsHandler_GetDocGeneration_ReturnsFullContent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Now().UTC()
	doc := models.DocGeneration{
		ID:           "doc-1",
		RepositoryID: repoIDPtr("repo-1"),
		Status:       models.DocGenerationStatusCompleted,
		Types:        datatypes.JSONSlice[string]([]string{"adr"}),
		Content:      datatypes.NewJSONType(map[string]string{"adr": "# ADR 001\nuse foo"}),
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	repo := &docsHandlerRepo{
		repo: &models.Repository{ID: "repo-1", OrganizationID: "org-1"},
		docs: []models.DocGeneration{doc},
	}
	handler := newDocsHandler(repo)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodGet, "/docs/doc-1", nil)
	req = req.WithContext(context.WithValue(req.Context(), utils.ContextKeyOrganization, "org-1"))
	c.Request = req
	c.Params = gin.Params{{Key: "id", Value: "doc-1"}}

	handler.GetDocGeneration(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if !contains(w.Body.String(), "# ADR 001") {
		t.Fatalf("response missing content; body=%s", w.Body.String())
	}
}

func TestDocsHandler_GetDocGeneration_NotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &docsHandlerRepo{repo: &models.Repository{ID: "repo-1", OrganizationID: "org-1"}}
	handler := newDocsHandler(repo)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodGet, "/docs/missing", nil)
	req = req.WithContext(context.WithValue(req.Context(), utils.ContextKeyOrganization, "org-1"))
	c.Request = req
	c.Params = gin.Params{{Key: "id", Value: "missing"}}

	handler.GetDocGeneration(c)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", w.Code, w.Body.String())
	}
}

func TestDocsHandler_GetDocGeneration_CrossOrgForbidden(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Now().UTC()
	repo := &docsHandlerRepo{
		repo: &models.Repository{ID: "repo-1", OrganizationID: "org-other"},
		docs: []models.DocGeneration{
			{ID: "doc-1", RepositoryID: repoIDPtr("repo-1"), Status: models.DocGenerationStatusCompleted, CreatedAt: now, UpdatedAt: now},
		},
	}
	handler := newDocsHandler(repo)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodGet, "/docs/doc-1", nil)
	req = req.WithContext(context.WithValue(req.Context(), utils.ContextKeyOrganization, "org-1"))
	c.Request = req
	c.Params = gin.Params{{Key: "id", Value: "doc-1"}}

	handler.GetDocGeneration(c)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body=%s", w.Code, w.Body.String())
	}
}

func contains(haystack, needle string) bool {
	return len(needle) == 0 || (len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0)
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
