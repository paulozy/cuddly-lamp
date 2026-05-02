package models

import "time"

// CoverageUploadToken authorizes a specific repository's CI to upload coverage
// reports via POST /api/v1/repositories/:id/coverage. Tokens are presented
// once on creation; only the SHA-256 hash is persisted, so they are not
// recoverable. Revocation flips `RevokedAt` (no row delete) so audit trails
// survive.
type CoverageUploadToken struct {
	ID string `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`

	RepositoryID string      `gorm:"type:uuid;not null;index" json:"repository_id"`
	Repository   *Repository `gorm:"foreignKey:RepositoryID" json:"repository,omitempty"`

	Name      string `gorm:"type:varchar(255);not null" json:"name"`
	TokenHash string `gorm:"type:varchar(64);not null;uniqueIndex" json:"-"`

	CreatedByUserID string `gorm:"type:uuid" json:"created_by_user_id,omitempty"`
	CreatedByUser   *User  `gorm:"foreignKey:CreatedByUserID" json:"-"`

	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`

	CreatedAt time.Time `json:"created_at"`
}

func (CoverageUploadToken) TableName() string {
	return "coverage_upload_tokens"
}

// IsActive reports whether the token can still authenticate uploads.
func (t *CoverageUploadToken) IsActive(now time.Time) bool {
	if t.RevokedAt != nil {
		return false
	}
	if t.ExpiresAt != nil && !t.ExpiresAt.After(now) {
		return false
	}
	return true
}
