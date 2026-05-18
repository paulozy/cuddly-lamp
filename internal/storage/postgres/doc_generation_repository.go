package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"gorm.io/gorm"
)

func (pr *PostgresRepository) CreateDocGeneration(ctx context.Context, doc *models.DocGeneration) error {
	if !doc.IsValid() {
		return errors.New("invalid doc generation data")
	}
	if err := pr.db.WithContext(ctx).Create(doc).Error; err != nil {
		return fmt.Errorf("create doc generation: %w", err)
	}
	return nil
}

func (pr *PostgresRepository) UpdateDocGeneration(ctx context.Context, doc *models.DocGeneration) error {
	if !doc.IsValid() {
		return errors.New("invalid doc generation data")
	}
	if err := pr.db.WithContext(ctx).Save(doc).Error; err != nil {
		return fmt.Errorf("update doc generation: %w", err)
	}
	return nil
}

func (pr *PostgresRepository) GetDocGeneration(ctx context.Context, id string) (*models.DocGeneration, error) {
	var doc models.DocGeneration
	if err := pr.db.WithContext(ctx).First(&doc, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("get doc generation: %w", err)
	}
	return &doc, nil
}

func (pr *PostgresRepository) GetLatestDocGenerationForRepo(ctx context.Context, repoID string) (*models.DocGeneration, error) {
	var doc models.DocGeneration
	if err := pr.db.WithContext(ctx).
		Where("repository_id = ? AND status = ?", repoID, models.DocGenerationStatusCompleted).
		Order("created_at DESC").
		First(&doc).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("get latest doc generation: %w", err)
	}
	return &doc, nil
}

func (pr *PostgresRepository) ListDocGenerationsForRepo(ctx context.Context, repoID string) ([]models.DocGeneration, error) {
	var docs []models.DocGeneration
	if err := pr.db.WithContext(ctx).
		Where("repository_id = ?", repoID).
		Order("created_at DESC").
		Find(&docs).Error; err != nil {
		return nil, fmt.Errorf("list doc generations: %w", err)
	}
	return docs, nil
}

// ListOrgDocGenerations returns org-level docs for the head of each
// supersession chain. Used by the UI listing — older versions are still
// reachable individually via GET /docs/:id.
func (pr *PostgresRepository) ListOrgDocGenerations(ctx context.Context, orgID string) ([]models.DocGeneration, error) {
	var docs []models.DocGeneration
	if err := pr.db.WithContext(ctx).
		Where("organization_id = ? AND scope = ? AND superseded_by_id IS NULL", orgID, models.DocGenerationScopeOrg).
		Order("created_at DESC").
		Find(&docs).Error; err != nil {
		return nil, fmt.Errorf("list org doc generations: %w", err)
	}
	return docs, nil
}

// GetLatestOrgDocs returns the most recent COMPLETED head row per requested
// doc `type` (ADR, architecture, guidelines). Used by the AnalysisWorker to
// inject org-wide standards into every per-repo analysis prompt.
//
// We use the JSONB `@>` operator (contains) — `types @> ?::jsonb` — because
// GORM treats `?` as a placeholder, making the JSONB existence operator (`?`)
// awkward to use directly.
func (pr *PostgresRepository) GetLatestOrgDocs(ctx context.Context, orgID string, types []string) ([]models.DocGeneration, error) {
	out := make([]models.DocGeneration, 0, len(types))
	for _, t := range types {
		var doc models.DocGeneration
		contains := fmt.Sprintf(`["%s"]`, t)
		err := pr.db.WithContext(ctx).
			Where("organization_id = ? AND scope = ? AND status = ? AND superseded_by_id IS NULL AND types @> ?::jsonb",
				orgID, models.DocGenerationScopeOrg, models.DocGenerationStatusCompleted, contains).
			Order("created_at DESC").
			First(&doc).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			return nil, fmt.Errorf("get latest org doc (%s): %w", t, err)
		}
		out = append(out, doc)
	}
	return out, nil
}
