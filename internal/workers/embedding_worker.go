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
	provider    embeddings.Provider
	repo        storage.Repository
	githubToken string
}

func NewEmbeddingWorker(provider embeddings.Provider, repo storage.Repository, githubToken string) *EmbeddingWorker {
	return &EmbeddingWorker{
		provider:    provider,
		repo:        repo,
		githubToken: githubToken,
	}
}

func (w *EmbeddingWorker) Handle(ctx context.Context, task *asynq.Task) error {
	if w.provider == nil {
		return fmt.Errorf("embedding worker: provider not configured")
	}

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

	branch := payload.Branch
	if branch == "" {
		branch = repository.Metadata.DefaultBranch
	}
	if branch == "" {
		branch = "main"
	}

	utils.Info("embedding worker: collecting chunks", "repo_id", repository.ID, "branch", branch)
	chunks, err := embeddings.CollectRepositoryChunks(ctx, repository.URL, w.githubToken, branch)
	if err != nil {
		return fmt.Errorf("embedding worker: collect chunks: %w", err)
	}

	deleteFilter := storage.EmbeddingDeleteFilter{
		RepositoryID: repository.ID,
		Provider:     w.provider.Provider(),
		Model:        w.provider.Model(),
		Dimension:    w.provider.Dimension(),
		Branch:       branch,
	}
	if err := w.repo.DeleteEmbeddings(ctx, deleteFilter); err != nil {
		return fmt.Errorf("embedding worker: delete old embeddings: %w", err)
	}
	if len(chunks) == 0 {
		utils.Warn("embedding worker: no chunks found", "repo_id", repository.ID)
		return nil
	}

	now := time.Now().UTC()
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

		result, err := w.provider.Embed(ctx, input, embeddings.InputTypeDocument)
		if err != nil {
			return fmt.Errorf("embedding worker: embed batch: %w", err)
		}
		if len(result.Embeddings) != len(batch) {
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
				Provider:     w.provider.Provider(),
				Model:        w.provider.Model(),
				Dimension:    w.provider.Dimension(),
				Branch:       branch,
				CommitSHA:    payload.CommitSHA,
				Embedding:    pgvector.NewVector(result.Embeddings[i]),
				Tokens:       chunk.Tokens,
				CreatedAt:    now,
				UpdatedAt:    now,
			}
		}

		if err := w.repo.CreateCodeEmbeddings(ctx, records); err != nil {
			return fmt.Errorf("embedding worker: save embeddings: %w", err)
		}
	}

	utils.Info("embedding worker: completed", "repo_id", repository.ID, "chunks", len(chunks), "provider", w.provider.Provider(), "model", w.provider.Model())
	return nil
}
