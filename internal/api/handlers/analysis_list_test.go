package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
)

type analysisListRepo struct {
	storage.Repository
	repo         *models.Repository
	analyses     []models.CodeAnalysis
	total        int64
	lastLimit    int
	lastOffset   int
	lastType     string
	lastStatus   string
}

func (m *analysisListRepo) GetRepository(ctx context.Context, id string) (*models.Repository, error) {
	return m.repo, nil
}

func (m *analysisListRepo) GetAnalysesByRepository(ctx context.Context, repoID string, analysisType, status string, limit, offset int) ([]models.CodeAnalysis, int64, error) {
	m.lastLimit = limit
	m.lastOffset = offset
	m.lastType = analysisType
	m.lastStatus = status
	return m.analyses, m.total, nil
}

func TestAnalysisHandler_ListAnalyses_ReturnsStoredAnalyses(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &analysisListRepo{
		repo: &models.Repository{ID: "repo-1", OrganizationID: "org-1"},
		analyses: []models.CodeAnalysis{
			{ID: "analysis-1", RepositoryID: "repo-1", Type: models.AnalysisTypeCodeReview},
			{ID: "analysis-2", RepositoryID: "repo-1", Type: models.AnalysisTypeSecurity},
		},
		total: 2,
	}
	handler := NewAnalysisHandler(repo, nil, nil, nil)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodGet, "/repositories/repo-1/analyses?limit=10&offset=5", nil)
	c.Request = req
	c.Params = gin.Params{{Key: "id", Value: "repo-1"}}

	handler.ListAnalyses(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if repo.lastLimit != 10 || repo.lastOffset != 5 {
		t.Fatalf("limit/offset = %d/%d, want 10/5", repo.lastLimit, repo.lastOffset)
	}
	if want := `"total":2`; !containsString(w.Body.String(), want) {
		t.Fatalf("body = %s, want %s", w.Body.String(), want)
	}
	if want := `"id":"analysis-1"`; !containsString(w.Body.String(), want) {
		t.Fatalf("body = %s, want %s", w.Body.String(), want)
	}
}

func TestAnalysisHandler_ListAnalyses_FiltersByTypeAndStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &analysisListRepo{
		repo: &models.Repository{ID: "repo-1", OrganizationID: "org-1"},
		analyses: []models.CodeAnalysis{
			{ID: "analysis-1", RepositoryID: "repo-1", Type: models.AnalysisTypeCodeReview, Status: models.AnalysisStatusCompleted},
		},
		total: 1,
	}
	handler := NewAnalysisHandler(repo, nil, nil, nil)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodGet, "/repositories/repo-1/analyses?type=code_review&status=completed&limit=5", nil)
	c.Request = req
	c.Params = gin.Params{{Key: "id", Value: "repo-1"}}

	handler.ListAnalyses(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if repo.lastType != "code_review" {
		t.Fatalf("lastType = %q, want code_review", repo.lastType)
	}
	if repo.lastStatus != "completed" {
		t.Fatalf("lastStatus = %q, want completed", repo.lastStatus)
	}
	if repo.lastLimit != 5 {
		t.Fatalf("lastLimit = %d, want 5", repo.lastLimit)
	}
}

func TestAnalysisHandler_ListAnalyses_InvalidType_Returns400(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &analysisListRepo{
		repo: &models.Repository{ID: "repo-1", OrganizationID: "org-1"},
	}
	handler := NewAnalysisHandler(repo, nil, nil, nil)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodGet, "/repositories/repo-1/analyses?type=bogus", nil)
	c.Request = req
	c.Params = gin.Params{{Key: "id", Value: "repo-1"}}

	handler.ListAnalyses(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
	if !containsString(w.Body.String(), "type must be one of") {
		t.Fatalf("body = %s, want validation message", w.Body.String())
	}
}

func TestAnalysisHandler_ListAnalyses_InvalidStatus_Returns400(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &analysisListRepo{
		repo: &models.Repository{ID: "repo-1", OrganizationID: "org-1"},
	}
	handler := NewAnalysisHandler(repo, nil, nil, nil)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodGet, "/repositories/repo-1/analyses?status=bogus", nil)
	c.Request = req
	c.Params = gin.Params{{Key: "id", Value: "repo-1"}}

	handler.ListAnalyses(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
}

func containsString(s, substr string) bool {
	return strings.Contains(s, substr)
}
