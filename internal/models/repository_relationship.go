package models

import (
	"time"

	"gorm.io/datatypes"
)

type RepositoryRelationshipKind string

const (
	RepositoryRelationshipKindHTTP    RepositoryRelationshipKind = "http"
	RepositoryRelationshipKindAsync   RepositoryRelationshipKind = "async"
	RepositoryRelationshipKindLibrary RepositoryRelationshipKind = "library"
	RepositoryRelationshipKindData    RepositoryRelationshipKind = "data"
	RepositoryRelationshipKindInfra   RepositoryRelationshipKind = "infra"
	RepositoryRelationshipKindManual  RepositoryRelationshipKind = "manual"
	RepositoryRelationshipKindOther   RepositoryRelationshipKind = "other"
)

type RepositoryRelationshipSource string

const (
	RepositoryRelationshipSourceManual           RepositoryRelationshipSource = "manual"
	RepositoryRelationshipSourceAnalysis         RepositoryRelationshipSource = "analysis"
	RepositoryRelationshipSourceManifest         RepositoryRelationshipSource = "manifest"
	RepositoryRelationshipSourceImport           RepositoryRelationshipSource = "import"
	RepositoryRelationshipSourceWebhook          RepositoryRelationshipSource = "webhook"
	RepositoryRelationshipSourceLegacyDependency RepositoryRelationshipSource = "legacy_dependency"
)

type RepositoryRelationship struct {
	ID string `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`

	OrganizationID     string        `gorm:"type:uuid;index;not null" json:"organization_id"`
	Organization       *Organization `gorm:"foreignKey:OrganizationID" json:"organization,omitempty"`
	SourceRepositoryID string        `gorm:"type:uuid;index;not null" json:"source_repository_id"`
	SourceRepository   *Repository   `gorm:"foreignKey:SourceRepositoryID" json:"source_repository,omitempty"`
	TargetRepositoryID string        `gorm:"type:uuid;index;not null" json:"target_repository_id"`
	TargetRepository   *Repository   `gorm:"foreignKey:TargetRepositoryID" json:"target_repository,omitempty"`

	Kind        RepositoryRelationshipKind   `gorm:"type:varchar(50);index;not null" json:"kind"`
	Label       string                       `gorm:"type:varchar(255)" json:"label,omitempty"`
	Description string                       `gorm:"type:text" json:"description,omitempty"`
	Source      RepositoryRelationshipSource `gorm:"type:varchar(50);index;not null;default:'manual'" json:"source"`
	Confidence  float64                      `gorm:"type:numeric(5,4);not null;default:1.0" json:"confidence"`
	Metadata    datatypes.JSONMap            `gorm:"type:jsonb;default:'{}'" json:"metadata,omitempty"`

	CreatedByUserID string `gorm:"type:uuid;index" json:"created_by_user_id,omitempty"`
	CreatedByUser   *User  `gorm:"foreignKey:CreatedByUserID" json:"created_by_user,omitempty"`

	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	DeletedAt *time.Time `gorm:"index" json:"deleted_at,omitempty"`
}

func (RepositoryRelationship) TableName() string {
	return "repository_relationships"
}

func (r *RepositoryRelationship) IsValid() bool {
	return r.OrganizationID != "" &&
		r.SourceRepositoryID != "" &&
		r.TargetRepositoryID != "" &&
		r.SourceRepositoryID != r.TargetRepositoryID &&
		IsValidRepositoryRelationshipKind(r.Kind) &&
		IsValidRepositoryRelationshipSource(r.Source) &&
		r.Confidence >= 0 &&
		r.Confidence <= 1
}

func IsValidRepositoryRelationshipKind(kind RepositoryRelationshipKind) bool {
	switch kind {
	case RepositoryRelationshipKindHTTP,
		RepositoryRelationshipKindAsync,
		RepositoryRelationshipKindLibrary,
		RepositoryRelationshipKindData,
		RepositoryRelationshipKindInfra,
		RepositoryRelationshipKindManual,
		RepositoryRelationshipKindOther:
		return true
	default:
		return false
	}
}

func IsValidRepositoryRelationshipSource(source RepositoryRelationshipSource) bool {
	switch source {
	case RepositoryRelationshipSourceManual,
		RepositoryRelationshipSourceAnalysis,
		RepositoryRelationshipSourceManifest,
		RepositoryRelationshipSourceImport,
		RepositoryRelationshipSourceWebhook,
		RepositoryRelationshipSourceLegacyDependency:
		return true
	default:
		return false
	}
}
