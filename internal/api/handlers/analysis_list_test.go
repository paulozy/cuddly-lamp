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
	repo       *models.Repository
	analyses   []models.CodeAnalysis
	total      int64
	lastLimit  int
	lastOffset int
}

func (m *analysisListRepo) GetRepository(ctx context.Context, id string) (*models.Repository, error) {
	return m.repo, nil
}

func (m *analysisListRepo) GetAnalysesByRepository(ctx context.Context, repoID string, limit, offset int) ([]models.CodeAnalysis, int64, error) {
	m.lastLimit = limit
	m.lastOffset = offset
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

func containsString(s, substr string) bool {
	return strings.Contains(s, substr)
}
