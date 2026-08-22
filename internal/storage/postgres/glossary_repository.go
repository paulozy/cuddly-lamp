package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"gorm.io/gorm"
)

func (pr *PostgresRepository) CreateGlossaryTerm(ctx context.Context, term *models.GlossaryTerm) error {
	if err := pr.db.WithContext(ctx).Create(term).Error; err != nil {
		return fmt.Errorf("create glossary term: %w", err)
	}
	return nil
}

func (pr *PostgresRepository) GetGlossaryTerm(ctx context.Context, id string) (*models.GlossaryTerm, error) {
	var term models.GlossaryTerm
	if err := pr.db.WithContext(ctx).First(&term, "id = ? AND deleted_at IS NULL", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("get glossary term: %w", err)
	}
	return &term, nil
}

// ListGlossaryTerms orders case-insensitively, so `SLO` and `slo` sit together
// rather than in separate ASCII neighbourhoods.
func (pr *PostgresRepository) ListGlossaryTerms(ctx context.Context, orgID string) ([]models.GlossaryTerm, error) {
	var terms []models.GlossaryTerm
	if err := pr.db.WithContext(ctx).
		Where("organization_id = ? AND deleted_at IS NULL", orgID).
		Order("lower(term) ASC").
		Find(&terms).Error; err != nil {
		return nil, fmt.Errorf("list glossary terms: %w", err)
	}
	return terms, nil
}

func (pr *PostgresRepository) UpdateGlossaryTerm(ctx context.Context, term *models.GlossaryTerm) error {
	term.UpdatedAt = time.Now().UTC()
	err := pr.db.WithContext(ctx).
		Model(&models.GlossaryTerm{}).
		Where("id = ? AND deleted_at IS NULL", term.ID).
		Updates(map[string]interface{}{
			"term":       term.Term,
			"definition": term.Definition,
			"updated_at": term.UpdatedAt,
		}).Error
	if err != nil {
		return fmt.Errorf("update glossary term: %w", err)
	}
	return nil
}

// DeleteGlossaryTerm soft-deletes, so a step that referenced the term renders
// the reference as gone rather than breaking.
func (pr *PostgresRepository) DeleteGlossaryTerm(ctx context.Context, id string) error {
	now := time.Now().UTC()
	err := pr.db.WithContext(ctx).
		Model(&models.GlossaryTerm{}).
		Where("id = ? AND deleted_at IS NULL", id).
		Updates(map[string]interface{}{"deleted_at": now, "updated_at": now}).Error
	if err != nil {
		return fmt.Errorf("delete glossary term: %w", err)
	}
	return nil
}
