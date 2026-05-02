package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"gorm.io/gorm"
)

func (pr *PostgresRepository) CreateCoverageUploadToken(ctx context.Context, token *models.CoverageUploadToken) error {
	if token == nil || token.RepositoryID == "" || token.TokenHash == "" || token.Name == "" {
		return errors.New("coverage upload token requires repository_id, name and token_hash")
	}
	if err := pr.db.WithContext(ctx).Create(token).Error; err != nil {
		return fmt.Errorf("create coverage upload token: %w", err)
	}
	return nil
}

func (pr *PostgresRepository) GetCoverageUploadTokenByHash(ctx context.Context, hash string) (*models.CoverageUploadToken, error) {
	var token models.CoverageUploadToken
	err := pr.db.WithContext(ctx).
		Where("token_hash = ?", hash).
		First(&token).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("get coverage upload token by hash: %w", err)
	}
	return &token, nil
}

func (pr *PostgresRepository) GetCoverageUploadToken(ctx context.Context, id string) (*models.CoverageUploadToken, error) {
	var token models.CoverageUploadToken
	err := pr.db.WithContext(ctx).First(&token, "id = ?", id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("get coverage upload token: %w", err)
	}
	return &token, nil
}

func (pr *PostgresRepository) ListCoverageUploadTokens(ctx context.Context, repoID string) ([]*models.CoverageUploadToken, error) {
	var tokens []*models.CoverageUploadToken
	err := pr.db.WithContext(ctx).
		Where("repository_id = ?", repoID).
		Order("created_at DESC").
		Find(&tokens).Error
	if err != nil {
		return nil, fmt.Errorf("list coverage upload tokens: %w", err)
	}
	return tokens, nil
}

func (pr *PostgresRepository) RevokeCoverageUploadToken(ctx context.Context, id string) error {
	now := time.Now().UTC()
	res := pr.db.WithContext(ctx).
		Model(&models.CoverageUploadToken{}).
		Where("id = ? AND revoked_at IS NULL", id).
		Update("revoked_at", &now)
	if res.Error != nil {
		return fmt.Errorf("revoke coverage upload token: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (pr *PostgresRepository) TouchCoverageUploadTokenUsage(ctx context.Context, id string) error {
	now := time.Now().UTC()
	if err := pr.db.WithContext(ctx).
		Model(&models.CoverageUploadToken{}).
		Where("id = ?", id).
		Update("last_used_at", &now).Error; err != nil {
		return fmt.Errorf("touch coverage upload token: %w", err)
	}
	return nil
}
