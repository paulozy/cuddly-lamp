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
	// Provides is repo → api and Uses is repo → resource. They exist so a
	// typed graph can say what an edge means; `other` cannot.
	RepositoryRelationshipKindProvides RepositoryRelationshipKind = "provides"
	RepositoryRelationshipKindUses     RepositoryRelationshipKind = "uses"
)

type RepositoryRelationshipSource string

const (
	RepositoryRelationshipSourceManual           RepositoryRelationshipSource = "manual"
	RepositoryRelationshipSourceAnalysis         RepositoryRelationshipSource = "analysis"
	RepositoryRelationshipSourceManifest         RepositoryRelationshipSource = "manifest"
	RepositoryRelationshipSourceImport           RepositoryRelationshipSource = "import"
	RepositoryRelationshipSourceWebhook          RepositoryRelationshipSource = "webhook"
	RepositoryRelationshipSourceLegacyDependency RepositoryRelationshipSource = "legacy_dependency"
	// Config marks an edge derived from configuration committed in the
	// repository — a compose file, a Kubernetes manifest, a Helm values file.
	RepositoryRelationshipSourceConfig RepositoryRelationshipSource = "config"
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

	// DerivationKey is empty for a row a human declared, and
	// `<deriver>:<version>:<scope>` for a derived one. Every sweep is scoped by
	// it, which is what makes deleting a human row structurally impossible.
	// Promoting a derived row to human is done by clearing it — Backstage's
	// "existing entity has no location key" rule, used on purpose.
	// It is a pointer because NULL and '' are not the same fact here, and the
	// difference is the safety guarantee: the partial unique index and every
	// sweep predicate are written against `derivation_key IS NOT NULL`, so an
	// empty string would enrol human rows in both. A non-pointer string cannot
	// be written as NULL by GORM, which is exactly how that bug would arrive.
	DerivationKey *string `json:"derivation_key,omitempty"`
	// DerivationFingerprint is the identity of the fact behind the edge. The
	// declared version is deliberately not part of it: a version bump must not
	// sweep and recreate the row, which would change its id.
	DerivationFingerprint *string    `json:"derivation_fingerprint,omitempty"`
	LastSeenAt            *time.Time `json:"last_seen_at,omitempty"`

	CreatedByUserID string `gorm:"type:uuid;index" json:"created_by_user_id,omitempty"`
	CreatedByUser   *User  `gorm:"foreignKey:CreatedByUserID" json:"created_by_user,omitempty"`

	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	DeletedAt *time.Time `gorm:"index" json:"deleted_at,omitempty"`
}

// IsDerived reports whether this row was produced by a deriver rather than
// declared by a person.
func (r *RepositoryRelationship) IsDerived() bool {
	return r.DerivationKey != nil && *r.DerivationKey != ""
}

// Promote turns a derived row into a human-declared one by dropping its
// derivation identity.
//
// This is Backstage's "if the existing entity has no location key, the new
// entity wins" rule used on purpose: once a person edits a derived edge, the
// sweep must abandon it rather than fight them for it. The alternative —
// duplicating the row — leaves two competing truths on the graph.
func (r *RepositoryRelationship) Promote() {
	r.DerivationKey = nil
	r.DerivationFingerprint = nil
	r.LastSeenAt = nil
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
		RepositoryRelationshipKindOther,
		RepositoryRelationshipKindProvides,
		RepositoryRelationshipKindUses:
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
		RepositoryRelationshipSourceLegacyDependency,
		RepositoryRelationshipSourceConfig:
		return true
	default:
		return false
	}
}
