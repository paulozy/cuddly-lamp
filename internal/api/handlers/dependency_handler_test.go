package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hibiken/asynq"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
	"github.com/paulozy/idp-with-ai-backend/internal/utils"
)

type dependencyHandlerRepo struct {
	storage.Repository
	repo *models.Repository
	deps []*models.PackageDependency
}

func (r *dependencyHandlerRepo) GetRepository(ctx context.Context, id string) (*models.Repository, error) {
	return r.repo, nil
}

func (r *dependencyHandlerRepo) ListPackageDependencies(ctx context.Context, repoID string, onlyVulnerable bool) ([]*models.PackageDependency, error) {
	if !onlyVulnerable {
		return r.deps, nil
	}
	var out []*models.PackageDependency
	for _, dep := range r.deps {
		if dep.IsVulnerable {
			out = append(out, dep)
		}
	}
	return out, nil
}

type dependencyHandlerEnqueuer struct {
	err      error
	enqueued bool
}

func (e *dependencyHandlerEnqueuer) Enqueue(ctx context.Context, taskType string, payload any, opts ...asynq.Option) error {
	if e.err != nil {
		return e.err
	}
	e.enqueued = true
	return nil
}

func (e *dependencyHandlerEnqueuer) EnqueueIn(ctx context.Context, taskType string, payload any, delay time.Duration, opts ...asynq.Option) error {
	return e.Enqueue(ctx, taskType, payload, opts...)
}

func TestDependencyHandler_ScanDependencies(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &dependencyHandlerRepo{repo: &models.Repository{ID: "repo-1", OrganizationID: "org-1"}}
	enqueuer := &dependencyHandlerEnqueuer{}
	handler := NewDependencyHandler(repo, enqueuer)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodPost, "/repositories/repo-1/dependencies/scan", nil)
	req = req.WithContext(context.WithValue(req.Context(), utils.ContextKeyOrganization, "org-1"))
	c.Request = req
	c.Params = gin.Params{{Key: "id", Value: "repo-1"}}

	handler.ScanDependencies(c)

	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", w.Code, w.Body.String())
	}
	if !enqueuer.enqueued {
		t.Fatal("expected dependency scan to be enqueued")
	}
}

func TestDependencyHandler_ListDependencies(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &dependencyHandlerRepo{
		repo: &models.Repository{ID: "repo-1", OrganizationID: "org-1"},
		deps: []*models.PackageDependency{
			{Name: "safe", IsVulnerable: false},
			{Name: "vulnerable", IsVulnerable: true},
		},
	}
	handler := NewDependencyHandler(repo, &dependencyHandlerEnqueuer{})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodGet, "/repositories/repo-1/dependencies?vulnerable=true", nil)
	req = req.WithContext(context.WithValue(req.Context(), utils.ContextKeyOrganization, "org-1"))
	c.Request = req
	c.Params = gin.Params{{Key: "id", Value: "repo-1"}}

	handler.ListDependencies(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"total":1`) {
		t.Fatalf("body = %s, want total 1", w.Body.String())
	}
}
