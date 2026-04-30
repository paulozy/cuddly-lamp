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
