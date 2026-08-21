package models

import (
	"database/sql/driver"
	"encoding/json"
	"time"
)

type RepositoryType string

const (
	RepositoryTypeGitHub RepositoryType = "github"
	RepositoryTypeGitLab RepositoryType = "gitlab"
	RepositoryTypeGitea  RepositoryType = "gitea"
	RepositoryTypeCustom RepositoryType = "custom"
)

type RepositoryMetadata struct {
	// GitHub/GitLab specific
	OwnerID       string `json:"owner_id,omitempty"`
	OwnerName     string `json:"owner_name,omitempty"`
	ProviderID    string `json:"provider_id,omitempty"` // GitHub repo ID, GitLab project ID
	WebhookID     string `json:"webhook_id,omitempty"`
	DefaultBranch string `json:"default_branch,omitempty"`

	// Language & framework detection
	Languages  map[string]int `json:"languages,omitempty"`  // e.g., {"Go": 50, "SQL": 30}
	Frameworks []string       `json:"frameworks,omitempty"` // e.g., ["Gin", "GORM"]
	Topics     []string       `json:"topics,omitempty"`     // GitHub topics/GitLab tags

	// Statistics
	StarCount    int `json:"star_count,omitempty"`
	ForkCount    int `json:"fork_count,omitempty"`
	IssueCount   int `json:"issue_count,omitempty"`
	PRCount      int `json:"pr_count,omitempty"`
	BranchCount  int `json:"branch_count,omitempty"`
	CommitCount  int `json:"commit_count,omitempty"`
	Contributors int `json:"contributors,omitempty"`

	// Configuration
	HasCI        bool     `json:"has_ci,omitempty"`        // Has GitHub Actions/GitLab CI
	HasTests     bool     `json:"has_tests,omitempty"`     // Has test suite
	TestCoverage *float64 `json:"test_coverage,omitempty"` // Test coverage percentage
}

func (rm *RepositoryMetadata) Scan(value interface{}) error {
	bytes, _ := value.([]byte)
	return json.Unmarshal(bytes, &rm)
}

func (rm RepositoryMetadata) Value() (driver.Value, error) {
	return json.Marshal(rm)
}

type Repository struct {
	ID string `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`

	Name        string         `gorm:"type:varchar(255);index" json:"name"`
	Description string         `gorm:"type:text" json:"description"`
	URL         string         `gorm:"type:text;uniqueIndex" json:"url"`
	Type        RepositoryType `gorm:"type:varchar(50);index" json:"type"` // github, gitlab, gitea, custom

	OrganizationID  string        `gorm:"type:uuid;index" json:"organization_id"`
	Organization    *Organization `gorm:"foreignKey:OrganizationID" json:"organization,omitempty"`
	CreatedByUserID string        `gorm:"type:uuid;index" json:"created_by_user_id,omitempty"`
	CreatedByUser   *User         `gorm:"foreignKey:CreatedByUserID" json:"created_by_user,omitempty"`
	// OwnerTeamID is the accountable team. NULL means unowned, which is a real
	// state the catalog reports rather than a gap to be filled with a default.
	OwnerTeamID *string `gorm:"type:uuid;index" json:"owner_team_id,omitempty"`
	// OwnerTeam is filled by the enriched read so callers can render the owner
	// without a second query. It is `gorm:"-"` and a plain TeamRef rather than a
	// *Team on purpose: as a belongs-to association GORM upserts the referenced
	// row on every Save, and the partial team the read produces (id/name/slug)
	// would be written back with an empty organization_id.
	OwnerTeam *TeamRef `gorm:"-" json:"owner_team,omitempty"`

	IsPublic bool `gorm:"default:false" json:"is_public"`

	Metadata RepositoryMetadata `gorm:"type:jsonb;default:'{}'" json:"metadata"`

	LastSyncedAt time.Time `json:"last_synced_at,omitempty"`
	SyncStatus   string    `gorm:"type:varchar(50);default:'idle'" json:"sync_status"` // idle, syncing, synced, error
	SyncError    string    `gorm:"type:text" json:"sync_error,omitempty"`

	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	DeletedAt *time.Time `gorm:"index" json:"deleted_at,omitempty"` // soft delete

	// Relationships
	Webhooks []Webhook `gorm:"foreignKey:RepositoryID" json:"webhooks,omitempty"`

	// EnrichedStats is populated only during ListRepositories. It is never persisted.
	EnrichedStats *EnrichedStats `gorm:"-" json:"-"`
}

func (Repository) TableName() string {
	return "repositories"
}

// EnrichedStats carries lateral-join results from the enriched list query.
// It is a transient field — never stored in the DB.
type EnrichedStats struct {
	TestCoverage   float64
	TestedLines    int
	UncoveredLines int
	CoverageStatus string // ok|partial|failed|not_configured (empty when never uploaded)
	// HasCoverage is false when the repository has no coverage upload at all,
	// which the DTO uses to tell "never measured" from "measured 0%".
	HasCoverage        bool
	CoverageUploadedAt *string // ISO-8601 or nil
	// Scorecard signals, loaded by the same enriched read.
	HasDocs    bool
	HasWebhook bool
}

func (r *Repository) IsValid() bool {
	return r.Name != "" && r.URL != "" && r.Type != "" && r.OrganizationID != ""
}

func (r *Repository) CanSync() bool {
	return r.SyncStatus != "syncing"
}

func (r *Repository) UpdateSyncStatus(status string, errMsg *string) {
	r.SyncStatus = status
	r.LastSyncedAt = time.Now()
	if errMsg != nil {
		r.SyncError = *errMsg
	} else {
		r.SyncError = ""
	}
}
