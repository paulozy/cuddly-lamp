package workers

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hibiken/asynq"
	"github.com/paulozy/idp-with-ai-backend/internal/jobs/tasks"
	"github.com/paulozy/idp-with-ai-backend/internal/services"
	"github.com/paulozy/idp-with-ai-backend/internal/utils"
)

// ArchitectureWorker runs pass two of architecture derivation: reconciling one
// organization's derived graph from the facts its syncs left behind.
//
// It is org-scoped because an internal edge is inherently org-wide, and it is a
// separate task from the sync because reconciling is cheap and repeatable —
// pass two does no provider I/O at all.
type ArchitectureWorker struct {
	architectureService *services.ArchitectureService
}

func NewArchitectureWorker(svc *services.ArchitectureService) *ArchitectureWorker {
	return &ArchitectureWorker{architectureService: svc}
}

func (w *ArchitectureWorker) Handle(ctx context.Context, task *asynq.Task) error {
	var payload tasks.DeriveArchitecturePayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return fmt.Errorf("architecture worker: unmarshal payload: %w", err)
	}
	if payload.OrganizationID == "" {
		return fmt.Errorf("architecture worker: empty organization_id")
	}

	utils.Info("architecture worker: processing", "organization_id", payload.OrganizationID)

	if err := w.architectureService.Reconcile(ctx, payload.OrganizationID); err != nil {
		utils.Error("architecture worker: derivation failed", "organization_id", payload.OrganizationID, "error", err)
		// Returned so asynq retries: reconciliation is idempotent and reading
		// facts again is cheap, so a transient database failure is worth a retry.
		return err
	}
	return nil
}
