package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"gorm.io/gorm"
)

func (pr *PostgresRepository) CreateCoverageUpload(ctx context.Context, upload *models.CoverageUpload) error {
	if upload == nil || upload.RepositoryID == "" || upload.CommitSHA == "" {
		return errors.New("coverage upload requires repository_id and commit_sha")
	}
	if err := pr.db.WithContext(ctx).Create(upload).Error; err != nil {
		return fmt.Errorf("create coverage upload: %w", err)
	}
	return nil
}

func (pr *PostgresRepository) GetLatestCoverageUpload(ctx context.Context, repoID, sha string) (*models.CoverageUpload, error) {
	var upload models.CoverageUpload
	err := pr.db.WithContext(ctx).
		Where("repository_id = ? AND commit_sha = ?", repoID, sha).
		Order("created_at DESC").
		First(&upload).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("get latest coverage upload: %w", err)
	}
	return &upload, nil
}

func (pr *PostgresRepository) ListCoverageUploadsForCommit(ctx context.Context, repoID, sha string) ([]*models.CoverageUpload, error) {
	var uploads []*models.CoverageUpload
	err := pr.db.WithContext(ctx).
		Where("repository_id = ? AND commit_sha = ?", repoID, sha).
		Order("created_at DESC").
		Find(&uploads).Error
	if err != nil {
		return nil, fmt.Errorf("list coverage uploads: %w", err)
	}
	return uploads, nil
}

// PatchCodeAnalysisCoverage updates the most recent completed code_analysis row
// for (repoID, sha) with the supplied coverage metrics. Returns the number of
// rows affected — zero is normal when no analysis exists yet for this SHA.
func (pr *PostgresRepository) PatchCodeAnalysisCoverage(ctx context.Context, repoID, sha string, covered, total int, percentage float64, status string) (int64, error) {
	// Build the JSONB merge expression: keep existing keys, overwrite the
	// coverage-specific ones. `||` does shallow object merge in Postgres.
	const sql = `
        UPDATE code_analyses
           SET metrics = COALESCE(metrics, '{}'::jsonb) || jsonb_build_object(
                   'test_coverage', ?::float,
                   'tested_lines', ?::int,
                   'uncovered_lines', ?::int,
                   'coverage_status', ?::text
               ),
               updated_at = ?
         WHERE id = (
             SELECT id FROM code_analyses
              WHERE repository_id = ?
                AND commit_sha    = ?
                AND status        = 'completed'
                AND deleted_at IS NULL
              ORDER BY created_at DESC
              LIMIT 1
         )`
	uncovered := total - covered
	if uncovered < 0 {
		uncovered = 0
	}
	res := pr.db.WithContext(ctx).Exec(
		sql,
		percentage, covered, uncovered, status, time.Now().UTC(),
		repoID, sha,
	)
	if res.Error != nil {
		return 0, fmt.Errorf("patch code analysis coverage: %w", res.Error)
	}
	return res.RowsAffected, nil
}
