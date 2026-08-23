package models

import (
	"time"

	"gorm.io/datatypes"
)

// Resource is runtime infrastructure a repository depends on: a database, a
// queue, a cache, a bucket.
//
// The locator mirrors OpenTelemetry's database semantic conventions rather than
// inventing an identity — Engine is `db.system.name`, Host is `server.address`,
// Port is `server.port`, Namespace is `db.namespace`. The payoff is
// forward-looking: when observability integration arrives, a resource derived
// from committed config and one observed in a trace unify with no translation.
//
// ScopedRepositoryID is the honest half of the model. See migration 035: static
// files cannot prove two repositories share a production database, so a resource
// whose only evidence is a compose file or a Helm subchart belongs to one
// repository and is never unified with another's.
type Resource struct {
	ID string `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`

	OrganizationID string `gorm:"type:uuid;index;not null" json:"organization_id"`

	Engine string `gorm:"type:varchar(50);not null" json:"engine"`
	// Host, Port and Namespace are the locator. All three are optional because a
	// compose image proves the engine and nothing about which instance.
	Host      string `gorm:"type:varchar(255)" json:"host,omitempty"`
	Port      *int   `json:"port,omitempty"`
	Namespace string `gorm:"type:varchar(255)" json:"namespace,omitempty"`

	// ScopedRepositoryID is nil for a shared resource (the host is known, so two
	// repositories naming it are naming the same thing) and set when the evidence
	// was local-only. A pointer because NULL is the meaningful value here — the
	// partial unique indexes of migration 035 are written against it.
	ScopedRepositoryID *string `gorm:"type:uuid" json:"scoped_repository_id,omitempty"`

	DisplayName string `gorm:"type:varchar(255)" json:"display_name,omitempty"`

	DerivationKey         *string    `json:"derivation_key,omitempty"`
	DerivationFingerprint *string    `json:"derivation_fingerprint,omitempty"`
	LastSeenAt            *time.Time `json:"last_seen_at,omitempty"`

	Metadata datatypes.JSONMap `gorm:"type:jsonb;default:'{}'" json:"metadata,omitempty"`

	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	DeletedAt *time.Time `gorm:"index" json:"deleted_at,omitempty"`
}

func (Resource) TableName() string { return "resources" }

func (r *Resource) IsValid() bool {
	return r.OrganizationID != "" && r.Engine != ""
}

func (r *Resource) IsDerived() bool {
	return r.DerivationKey != nil && *r.DerivationKey != ""
}

// IsScoped reports whether this resource belongs to a single repository because
// the evidence could not identify an instance.
func (r *Resource) IsScoped() bool {
	return r.ScopedRepositoryID != nil && *r.ScopedRepositoryID != ""
}

// RepositoryResource joins a repository to a resource it uses, with the evidence
// that says so.
//
// It is a separate table because a shared resource has N consumers, and it is
// this join that answers "do these three services depend on the same Postgres?".
type RepositoryResource struct {
	ID string `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`

	OrganizationID string `gorm:"type:uuid;index;not null" json:"organization_id"`
	RepositoryID   string `gorm:"type:uuid;index;not null" json:"repository_id"`
	ResourceID     string `gorm:"type:uuid;index;not null" json:"resource_id"`

	Confidence float64 `gorm:"type:numeric(5,4);not null;default:1.0" json:"confidence"`

	DerivationKey         *string    `json:"derivation_key,omitempty"`
	DerivationFingerprint *string    `json:"derivation_fingerprint,omitempty"`
	LastSeenAt            *time.Time `json:"last_seen_at,omitempty"`

	Metadata datatypes.JSONMap `gorm:"type:jsonb;default:'{}'" json:"metadata,omitempty"`

	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	DeletedAt *time.Time `gorm:"index" json:"deleted_at,omitempty"`
}

func (RepositoryResource) TableName() string { return "repository_resources" }

func (r *RepositoryResource) IsValid() bool {
	return r.OrganizationID != "" && r.RepositoryID != "" && r.ResourceID != "" &&
		r.Confidence >= 0 && r.Confidence <= 1
}

func (r *RepositoryResource) IsDerived() bool {
	return r.DerivationKey != nil && *r.DerivationKey != ""
}
