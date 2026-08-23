package models

import (
	"time"

	"gorm.io/datatypes"
)

// APIKind is the closed set of contract formats discovery recognizes.
type APIKind string

const (
	APIKindOpenAPI  APIKind = "openapi"
	APIKindAsyncAPI APIKind = "asyncapi"
	APIKindGraphQL  APIKind = "graphql"
	APIKindGRPC     APIKind = "grpc"
)

func IsValidAPIKind(kind APIKind) bool {
	switch kind {
	case APIKindOpenAPI, APIKindAsyncAPI, APIKindGraphQL, APIKindGRPC:
		return true
	default:
		return false
	}
}

// API is a contract a repository exposes.
//
// Identity is (RepositoryID, SpecPath): the location, which is Backstage's
// `locationKey` applied to a file. Title and Version are display attributes and
// are deliberately outside the key — see migration 034 for why putting the
// version in it would manufacture a version history that never happened.
type API struct {
	ID string `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`

	OrganizationID string `gorm:"type:uuid;index;not null" json:"organization_id"`
	RepositoryID   string `gorm:"type:uuid;index;not null" json:"repository_id"`

	SpecPath string  `gorm:"type:varchar(1024);not null" json:"spec_path"`
	Kind     APIKind `gorm:"type:varchar(30);not null" json:"kind"`

	Title   string `gorm:"type:varchar(500)" json:"title,omitempty"`
	Version string `gorm:"type:varchar(100)" json:"version,omitempty"`
	// OperationCount is nil when `$ref` to an external file made the count
	// unreliable. nil and 0 are different facts and the UI must not collapse
	// them — a dash is honest, a zero is a lie.
	OperationCount *int `json:"operation_count,omitempty"`

	// See models.RepositoryRelationship for why these are pointers: NULL means
	// "a human owns this row" and '' would silently enrol it in every sweep.
	DerivationKey         *string    `json:"derivation_key,omitempty"`
	DerivationFingerprint *string    `json:"derivation_fingerprint,omitempty"`
	LastSeenAt            *time.Time `json:"last_seen_at,omitempty"`

	Metadata datatypes.JSONMap `gorm:"type:jsonb;default:'{}'" json:"metadata,omitempty"`

	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	DeletedAt *time.Time `gorm:"index" json:"deleted_at,omitempty"`
}

func (API) TableName() string { return "apis" }

func (a *API) IsValid() bool {
	return a.OrganizationID != "" &&
		a.RepositoryID != "" &&
		a.SpecPath != "" &&
		IsValidAPIKind(a.Kind)
}

func (a *API) IsDerived() bool {
	return a.DerivationKey != nil && *a.DerivationKey != ""
}
