package models

import (
	"time"

	"github.com/paulozy/idp-with-ai-backend/internal/coverage"
	"gorm.io/datatypes"
)

// CoverageUpload records a single coverage report uploaded by a CI run.
// All uploads for a given (repository_id, commit_sha) are kept; the most
// recent one wins when patching `code_analyses.metrics`.
type CoverageUpload struct {
	ID string `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`

	RepositoryID string      `gorm:"type:uuid;not null;index" json:"repository_id"`
	Repository   *Repository `gorm:"foreignKey:RepositoryID" json:"repository,omitempty"`

	CommitSHA string `gorm:"type:varchar(64);not null;index" json:"commit_sha"`
	Branch    string `gorm:"type:varchar(255)" json:"branch,omitempty"`

	Format       coverage.Format `gorm:"type:varchar(32);not null" json:"format"`
	LinesCovered int             `gorm:"not null;default:0" json:"lines_covered"`
	LinesTotal   int             `gorm:"not null;default:0" json:"lines_total"`
	Percentage   float64         `gorm:"not null;default:0" json:"percentage"`
	Status       coverage.Status `gorm:"type:varchar(32);not null" json:"status"`
	RawSizeBytes int             `gorm:"not null;default:0" json:"raw_size_bytes"`

	// Files maps source path → coverage breakdown. Used by the PR rule to
	// flag added files that arrived without test coverage.
	Files    datatypes.JSONType[map[string]coverage.FileCoverage] `gorm:"type:jsonb;not null;default:'{}'" json:"files"`
	Warnings StringArray                                          `gorm:"type:text[];not null;default:'{}'" json:"warnings"`

	UploadedByTokenID string `gorm:"type:uuid" json:"uploaded_by_token_id,omitempty"`

	CreatedAt time.Time `json:"created_at"`
}

func (CoverageUpload) TableName() string {
	return "coverage_uploads"
}
