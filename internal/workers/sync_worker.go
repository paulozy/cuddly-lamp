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

type SyncWorker struct {
	syncService *services.SyncService
}

func NewSyncWorker(svc *services.SyncService) *SyncWorker {
	return &SyncWorker{syncService: svc}
}

func (w *SyncWorker) Handle(ctx context.Context, task *asynq.Task) error {
	var payload tasks.SyncRepoPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return fmt.Errorf("sync worker: unmarshal payload: %w", err)
	}
	if payload.RepositoryID == "" {
		return fmt.Errorf("sync worker: empty repository_id")
	}

	utils.Info("sync worker: processing", "repo_id", payload.RepositoryID)

	if err := w.syncService.SyncRepository(ctx, payload.RepositoryID); err != nil {
		utils.Error("sync worker: sync failed", "repo_id", payload.RepositoryID, "error", err)
		return err
	}

	return nil
}
