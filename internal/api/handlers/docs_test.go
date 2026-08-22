package handlers

import (
	"context"
	"encoding/json"
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
	return NewDocsHandler(repo, nil, scm.Credentials{})
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

// --- Org docs handler tests ---

// orgDocsRepo embeds a full Repository implementation but overrides only the
// methods the org-scope endpoints exercise, so each test can configure its
// own state without re-implementing every interface method.
type orgDocsRepo struct {
	storage.Repository
	orgConfig   *models.OrganizationConfig
	tokensUsed  int64
	createdDocs []*models.DocGeneration
	updatedDocs []*models.DocGeneration
	listOrgDocs []models.DocGeneration
	docByID     map[string]*models.DocGeneration
	createErr   error
}

func (r *orgDocsRepo) GetOrganizationConfig(_ context.Context, orgID string) (*models.OrganizationConfig, error) {
	return r.orgConfig, nil
}

func (r *orgDocsRepo) SumTokensUsedSince(_ context.Context, _ string, _ time.Time) (int64, error) {
	return r.tokensUsed, nil
}

func (r *orgDocsRepo) CreateDocGeneration(_ context.Context, doc *models.DocGeneration) error {
	if r.createErr != nil {
		return r.createErr
	}
	r.createdDocs = append(r.createdDocs, doc)
	if r.docByID == nil {
		r.docByID = map[string]*models.DocGeneration{}
	}
	r.docByID[doc.ID] = doc
	return nil
}

func (r *orgDocsRepo) UpdateDocGeneration(_ context.Context, doc *models.DocGeneration) error {
	r.updatedDocs = append(r.updatedDocs, doc)
	return nil
}

func (r *orgDocsRepo) GetDocGeneration(_ context.Context, id string) (*models.DocGeneration, error) {
	if r.docByID == nil {
		return nil, nil
	}
	return r.docByID[id], nil
}

func (r *orgDocsRepo) ListOrgDocGenerations(_ context.Context, _ string) ([]models.DocGeneration, error) {
	return r.listOrgDocs, nil
}

type stubEnqueuer struct {
	enqueueErr error
	calls      []string // task type per call
}

func (e *stubEnqueuer) Enqueue(_ context.Context, taskType string, _ any, _ ...interface{ apply(any) }) error {
	// Real enqueuer uses asynq.Option — but the docs handler reaches into the
	// jobs.Enqueuer interface, which accepts asynq.Option directly. The test
	// just needs to record the call and surface a configured error.
	return e.enqueueErr
}

// withClaims attaches admin JWT claims to the request context. Used by the
// org-admin endpoints, which gate themselves on `OrganizationRole == admin`.
func withClaims(req *http.Request, orgRole models.UserRole) *http.Request {
	ctx := context.WithValue(req.Context(), utils.ContextKeyOrganization, "org-1")
	ctx = context.WithValue(ctx, utils.ContextKeyUser, "user-1")
	ctx = context.WithValue(ctx, utils.ContextKeyClaims, &models.TokenClaims{
		Role:             "admin",
		OrganizationRole: orgRole,
	})
	return req.WithContext(ctx)
}

func TestDocsHandler_GenerateOrgDocs_RequiresAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &orgDocsRepo{
		orgConfig: &models.OrganizationConfig{AnthropicAPIKey: "sk-test", AnthropicTokensPerHour: 20000},
	}
	handler := NewDocsHandler(repo, nil, scm.Credentials{})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body := `{"types":["architecture"]}`
	req := httptest.NewRequest(http.MethodPost, "/organizations/docs/generate", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withClaims(req, "developer") // not admin
	c.Request = req

	handler.GenerateOrgDocs(c)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body=%s", w.Code, w.Body.String())
	}
}

func TestDocsHandler_GenerateOrgDocs_RejectsRepoOnlyTypes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &orgDocsRepo{
		orgConfig: &models.OrganizationConfig{AnthropicAPIKey: "sk-test", AnthropicTokensPerHour: 20000},
	}
	handler := NewDocsHandler(repo, nil, scm.Credentials{})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body := `{"types":["service_doc"]}`
	req := httptest.NewRequest(http.MethodPost, "/organizations/docs/generate", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withClaims(req, "admin")
	c.Request = req

	handler.GenerateOrgDocs(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
}

func TestDocsHandler_GenerateOrgDocs_ADRRequiresTemplateID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &orgDocsRepo{
		orgConfig: &models.OrganizationConfig{AnthropicAPIKey: "sk-test", AnthropicTokensPerHour: 20000},
	}
	handler := NewDocsHandler(repo, nil, scm.Credentials{})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body := `{"types":["adr"]}` // missing template_id
	req := httptest.NewRequest(http.MethodPost, "/organizations/docs/generate", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withClaims(req, "admin")
	c.Request = req

	handler.GenerateOrgDocs(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
}

func TestDocsHandler_ListOrgDocs_FiltersByOrg(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Now().UTC()
	repo := &orgDocsRepo{
		listOrgDocs: []models.DocGeneration{
			{ID: "doc-org", OrganizationID: "org-1", Scope: models.DocGenerationScopeOrg, Status: models.DocGenerationStatusCompleted, CreatedAt: now, UpdatedAt: now},
		},
	}
	handler := NewDocsHandler(repo, nil, scm.Credentials{})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodGet, "/organizations/docs", nil)
	req = withClaims(req, "developer") // listing isn't admin-only
	c.Request = req

	handler.ListOrgDocs(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var resp models.DocGenerationListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Total != 1 || resp.Items[0].ID != "doc-org" {
		t.Fatalf("unexpected listing: %+v", resp)
	}
}

func TestDocsHandler_ListDocTemplates_ReturnsRegistry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewDocsHandler(&orgDocsRepo{}, nil, scm.Credentials{})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodGet, "/docs/templates", nil)
	req = withClaims(req, "developer")
	c.Request = req

	handler.ListDocTemplates(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if !contains(w.Body.String(), "adr-tech-choice") {
		t.Fatalf("templates response missing tech-choice id: %s", w.Body.String())
	}
}

func TestDocsHandler_UpdateDocContent_CreatesSupersedingVersion(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Now().UTC()
	previous := &models.DocGeneration{
		ID:             "doc-1",
		OrganizationID: "org-1",
		Scope:          models.DocGenerationScopeOrg,
		Status:         models.DocGenerationStatusCompleted,
		Types:          datatypes.JSONSlice[string]{"adr"},
		Content:        datatypes.NewJSONType(map[string]string{"adr": "# Original"}),
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	repo := &orgDocsRepo{
		docByID: map[string]*models.DocGeneration{"doc-1": previous},
	}
	handler := NewDocsHandler(repo, nil, scm.Credentials{})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body := `{"content":{"adr":"# Edited"}}`
	req := httptest.NewRequest(http.MethodPatch, "/docs/doc-1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withClaims(req, "admin")
	c.Request = req
	c.Params = gin.Params{{Key: "id", Value: "doc-1"}}

	handler.UpdateDocContent(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if len(repo.createdDocs) != 1 {
		t.Fatalf("expected 1 new row, got %d", len(repo.createdDocs))
	}
	newRow := repo.createdDocs[0]
	if got := newRow.Content.Data()["adr"]; got != "# Edited" {
		t.Errorf("new content = %q, want %q", got, "# Edited")
	}
	if previous.SupersededByID == nil || *previous.SupersededByID != newRow.ID {
		t.Errorf("previous row should be marked superseded by new id; got %+v", previous.SupersededByID)
	}
}
