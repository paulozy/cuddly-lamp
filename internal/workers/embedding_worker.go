package workers

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/hibiken/asynq"
	"github.com/paulozy/idp-with-ai-backend/internal/embeddings"
	"github.com/paulozy/idp-with-ai-backend/internal/jobs/tasks"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
	"github.com/paulozy/idp-with-ai-backend/internal/utils"
	"github.com/pgvector/pgvector-go"
)

const embeddingBatchSize = 64

type EmbeddingWorker struct {
	repo storage.Repository
}

func NewEmbeddingWorker(repo storage.Repository) *EmbeddingWorker {
	return &EmbeddingWorker{
		repo: repo,
	}
}

func (w *EmbeddingWorker) Handle(ctx context.Context, task *asynq.Task) error {
	var payload tasks.GenerateEmbeddingsPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return fmt.Errorf("embedding worker: unmarshal payload: %w", err)
	}
	if payload.RepositoryID == "" {
		return fmt.Errorf("embedding worker: empty repository_id")
	}

	repository, err := w.repo.GetRepository(ctx, payload.RepositoryID)
	if err != nil {
		return fmt.Errorf("embedding worker: get repository: %w", err)
	}
	if repository == nil {
		return fmt.Errorf("embedding worker: repository not found: %s", payload.RepositoryID)
	}
	cfg, err := w.repo.GetOrganizationConfig(ctx, repository.OrganizationID)
	if err != nil {
		return fmt.Errorf("embedding worker: get organization config: %w", err)
	}
	if cfg == nil || cfg.VoyageAPIKey == "" || cfg.EmbeddingsProvider != embeddings.ProviderVoyage {
		w.failEmbeddings(ctx, repository, "provider not configured for organization")
		return fmt.Errorf("embedding worker: provider not configured for organization")
	}
	provider := embeddings.NewVoyageClient(cfg.VoyageAPIKey, cfg.EmbeddingsModel, embeddings.DefaultDimension)

	branch := payload.Branch
	if branch == "" {
		branch = repository.Metadata.DefaultBranch
	}
	if branch == "" {
		branch = "main"
	}

	// Mark indexing immediately so the UI polling shows progress instead of
	// the previous `pending` state.
	w.markEmbeddingsStatus(ctx, repository, models.EmbeddingsStatusIndexing, 0, "")

	utils.Info("embedding worker: collecting chunks", "repo_id", repository.ID, "branch", branch)
	chunks, err := embeddings.CollectRepositoryChunks(ctx, repository.URL, cfg.GithubToken, branch)
	if err != nil {
		w.failEmbeddings(ctx, repository, fmt.Sprintf("collect chunks: %v", err))
		return fmt.Errorf("embedding worker: collect chunks: %w", err)
	}

	deleteFilter := storage.EmbeddingDeleteFilter{
		RepositoryID: repository.ID,
		Provider:     provider.Provider(),
		Model:        provider.Model(),
		Dimension:    provider.Dimension(),
		Branch:       branch,
	}
	if err := w.repo.DeleteEmbeddings(ctx, deleteFilter); err != nil {
		w.failEmbeddings(ctx, repository, fmt.Sprintf("delete old embeddings: %v", err))
		return fmt.Errorf("embedding worker: delete old embeddings: %w", err)
	}
	if len(chunks) == 0 {
		utils.Warn("embedding worker: no chunks found", "repo_id", repository.ID)
		// Treat "no indexable code" as `indexed` with count 0 — it's a valid
		// terminal state; the UI then shows "Indexado · 0" instead of leaving
		// the user in "Indexando…" forever.
		w.markEmbeddingsIndexed(ctx, repository, 0)
		return nil
	}

	now := time.Now().UTC()
	indexedCount := 0
	for start := 0; start < len(chunks); start += embeddingBatchSize {
		end := start + embeddingBatchSize
		if end > len(chunks) {
			end = len(chunks)
		}

		batch := chunks[start:end]
		input := make([]string, len(batch))
		for i := range batch {
			input[i] = batch[i].Content
		}

		result, err := provider.Embed(ctx, input, embeddings.InputTypeDocument)
		if err != nil {
			w.failEmbeddings(ctx, repository, fmt.Sprintf("embed batch: %v", err))
			return fmt.Errorf("embedding worker: embed batch: %w", err)
		}
		if len(result.Embeddings) != len(batch) {
			w.failEmbeddings(ctx, repository, "embedding count mismatch")
			return fmt.Errorf("embedding worker: embedding count mismatch: got %d, want %d", len(result.Embeddings), len(batch))
		}

		records := make([]models.CodeEmbedding, len(batch))
		for i, chunk := range batch {
			records[i] = models.CodeEmbedding{
				RepositoryID: repository.ID,
				FilePath:     chunk.FilePath,
				Content:      chunk.Content,
				ContentHash:  chunk.ContentHash,
				Language:     chunk.Language,
				StartLine:    chunk.StartLine,
				EndLine:      chunk.EndLine,
				Provider:     provider.Provider(),
				Model:        provider.Model(),
				Dimension:    provider.Dimension(),
				Branch:       branch,
				CommitSHA:    payload.CommitSHA,
				Embedding:    pgvector.NewVector(result.Embeddings[i]),
				Tokens:       chunk.Tokens,
				CreatedAt:    now,
				UpdatedAt:    now,
			}
		}

		if err := w.repo.CreateCodeEmbeddings(ctx, records); err != nil {
			w.failEmbeddings(ctx, repository, fmt.Sprintf("save embeddings: %v", err))
			return fmt.Errorf("embedding worker: save embeddings: %w", err)
		}
		indexedCount += len(records)
		// Surface partial progress between batches — the UI polls every 5s
		// and shows this count so a long index doesn't look stuck.
		w.markEmbeddingsStatus(ctx, repository, models.EmbeddingsStatusIndexing, indexedCount, "")
	}

	w.markEmbeddingsIndexed(ctx, repository, indexedCount)
	utils.Info("embedding worker: completed", "repo_id", repository.ID, "chunks", len(chunks), "provider", provider.Provider(), "model", provider.Model())
	return nil
}

// markEmbeddingsStatus persists the current pipeline status + count without
// touching `indexed_at`. Errors are only warned — never failing the job for
// a status-write hiccup.
func (w *EmbeddingWorker) markEmbeddingsStatus(ctx context.Context, repo *models.Repository, status string, count int, errMsg string) {
	repo.EmbeddingsStatus = status
	repo.EmbeddingsCount = count
	repo.EmbeddingsError = errMsg
	if err := w.repo.UpdateRepository(ctx, repo); err != nil {
		utils.Warn("embedding worker: status update failed", "repo_id", repo.ID, "status", status, "error", err)
	}
}

// markEmbeddingsIndexed is the terminal happy-path: status=indexed, error
// cleared, indexed_at=now.
func (w *EmbeddingWorker) markEmbeddingsIndexed(ctx context.Context, repo *models.Repository, count int) {
	now := time.Now().UTC()
	repo.EmbeddingsStatus = models.EmbeddingsStatusIndexed
	repo.EmbeddingsCount = count
	repo.EmbeddingsIndexedAt = &now
	repo.EmbeddingsError = ""
	if err := w.repo.UpdateRepository(ctx, repo); err != nil {
		utils.Warn("embedding worker: mark indexed failed", "repo_id", repo.ID, "error", err)
	}
}

// failEmbeddings is the terminal sad path: status=failed + error message.
func (w *EmbeddingWorker) failEmbeddings(ctx context.Context, repo *models.Repository, msg string) {
	utils.Error("embedding worker: failing", "repo_id", repo.ID, "error", msg)
	repo.EmbeddingsStatus = models.EmbeddingsStatusFailed
	repo.EmbeddingsError = msg
	if err := w.repo.UpdateRepository(ctx, repo); err != nil {
		utils.Warn("embedding worker: persist failure status failed", "repo_id", repo.ID, "error", err)
	}
}
