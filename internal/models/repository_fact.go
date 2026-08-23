// Package models — repository_fact.go holds the seam between the two passes of
// architecture derivation.
//
// Extraction is per repository and does network I/O. Reconciliation is per
// organization and is a pure function over the facts every repository left
// behind. This type is what the first pass writes and the second pass reads,
// which is what makes re-deriving cheap: the second pass performs no provider
// I/O at all.
package models

import (
	"time"

	"gorm.io/datatypes"
)

// RepositoryFactKind is the closed set of things an extractor can record.
type RepositoryFactKind string

const (
	// FactKindPackages holds the package names a repository publishes and the
	// dependencies it declares — the input to the library-edge index.
	FactKindPackages RepositoryFactKind = "packages"
	// FactKindAPIs holds the API specs discovered in the tree.
	FactKindAPIs RepositoryFactKind = "apis"
	// FactKindResources holds runtime infrastructure found in config.
	FactKindResources RepositoryFactKind = "resources"
	// FactKindHosts holds hostnames a repository declares (Kubernetes Service
	// names, ingress hosts) and the ones it consumes.
	FactKindHosts RepositoryFactKind = "hosts"
)

func IsValidRepositoryFactKind(kind RepositoryFactKind) bool {
	switch kind {
	case FactKindPackages, FactKindAPIs, FactKindResources, FactKindHosts:
		return true
	default:
		return false
	}
}

// RepositoryFact is one extractor's output for one repository.
type RepositoryFact struct {
	ID string `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`

	OrganizationID string `gorm:"type:uuid;index;not null" json:"organization_id"`
	RepositoryID   string `gorm:"type:uuid;index;not null" json:"repository_id"`

	FactKind RepositoryFactKind `gorm:"type:varchar(50);not null" json:"fact_kind"`

	// Payload is shaped by the extractor and versioned by ExtractorVersion, not
	// by the schema. See migration 032 for why this is not columns.
	Payload datatypes.JSON `gorm:"type:jsonb;default:'{}'" json:"payload"`

	// TreeSHA is the guard against unnecessary work: same tree, same facts, so
	// the extraction is skipped entirely.
	TreeSHA string `gorm:"type:varchar(64)" json:"tree_sha,omitempty"`

	// Complete is false when the tree came back truncated, when a file read
	// failed with anything other than "not found", or when the extractor
	// aborted. The reconciler never sweeps from an incomplete fact, because a
	// 429 is indistinguishable from a dependency that was removed.
	Complete bool `gorm:"not null;default:false" json:"complete"`

	ExtractorVersion int       `gorm:"not null;default:1" json:"extractor_version"`
	ExtractedAt      time.Time `json:"extracted_at"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (RepositoryFact) TableName() string {
	return "repository_facts"
}

// DerivationSuppression is a person's "no, that edge is wrong" made durable.
//
// A soft delete cannot express it: the next derivation run recomputes the same
// fingerprint and revives the row. Scoping the tombstone by
// (derivation_key, fingerprint) means the deriver can consult it and skip,
// which is the only thing that survives re-derivation.
type DerivationSuppression struct {
	ID string `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`

	OrganizationID        string `gorm:"type:uuid;index;not null" json:"organization_id"`
	DerivationKey         string `gorm:"not null" json:"derivation_key"`
	DerivationFingerprint string `gorm:"not null" json:"derivation_fingerprint"`

	Reason          string `json:"reason,omitempty"`
	CreatedByUserID string `gorm:"type:uuid" json:"created_by_user_id,omitempty"`

	CreatedAt time.Time `json:"created_at"`
}

func (DerivationSuppression) TableName() string {
	return "derivation_suppressions"
}
