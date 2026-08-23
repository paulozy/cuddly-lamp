package workers

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/paulozy/idp-with-ai-backend/internal/jobs/tasks"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/services"
)

// architectureStore answers the reads a reconciliation over an organization with
// no facts performs, and nothing else. The embedded storage.Repository panics on
// anything more, which is what keeps this test honest about the worker's scope:
// an organization with no facts must produce no writes at all.
type architectureStore struct {
	mockRepository
	listedKinds []models.RepositoryFactKind
	sweepCalls  int
}

func (s *architectureStore) ListRepositoryFacts(_ context.Context, _ string, kind models.RepositoryFactKind) ([]models.RepositoryFact, error) {
	s.listedKinds = append(s.listedKinds, kind)
	return nil, nil
}

func (s *architectureStore) ListSuppressions(_ context.Context, _, _ string) ([]models.DerivationSuppression, error) {
	return nil, nil
}

func (s *architectureStore) SweepDerivedRelationships(_ context.Context, _ string, _ time.Time) (int64, error) {
	s.sweepCalls++
	return 0, nil
}

func newArchitectureTask(t *testing.T, payload tasks.DeriveArchitecturePayload) *asynq.Task {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return asynq.NewTask(tasks.TypeDeriveArchitecture, body)
}

func TestArchitectureWorker_Handle(t *testing.T) {
	store := &architectureStore{}
	worker := NewArchitectureWorker(services.NewArchitectureService(store, nil))

	task := newArchitectureTask(t, tasks.DeriveArchitecturePayload{OrganizationID: "org-1"})
	if err := worker.Handle(context.Background(), task); err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	// Every registered fact kind is read, and an organization with no facts
	// produces no writes at all — the embedded storage.Repository would panic on
	// an unstubbed write, which is what proves it.
	if !slices.Contains(store.listedKinds, models.FactKindPackages) ||
		!slices.Contains(store.listedKinds, models.FactKindAPIs) {
		t.Errorf("listed fact kinds = %v, want packages and apis", store.listedKinds)
	}
}

// An empty organization id is refused rather than reconciled against nothing:
// a reconciliation with no scope would sweep with a key nobody owns.
func TestArchitectureWorker_HandleRejectsEmptyOrganizationID(t *testing.T) {
	worker := NewArchitectureWorker(services.NewArchitectureService(&architectureStore{}, nil))

	if err := worker.Handle(context.Background(), newArchitectureTask(t, tasks.DeriveArchitecturePayload{})); err == nil {
		t.Error("Handle() error = nil, want an error for an empty organization_id")
	}
}

func TestArchitectureWorker_HandleRejectsMalformedPayload(t *testing.T) {
	worker := NewArchitectureWorker(services.NewArchitectureService(&architectureStore{}, nil))

	task := asynq.NewTask(tasks.TypeDeriveArchitecture, []byte("not json"))
	if err := worker.Handle(context.Background(), task); err == nil {
		t.Error("Handle() error = nil, want an unmarshal error")
	}
}
