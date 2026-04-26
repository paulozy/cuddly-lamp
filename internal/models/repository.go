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

	OwnerUserID string `gorm:"type:uuid;index" json:"owner_user_id"`
	OwnerUser   *User  `gorm:"foreignKey:OwnerUserID" json:"owner_user,omitempty"`

	IsPublic bool `gorm:"default:false" json:"is_public"`

	Metadata RepositoryMetadata `gorm:"type:jsonb;default:'{}'" json:"metadata"`

	LastAnalyzedAt time.Time `json:"last_analyzed_at,omitempty"`
	AnalysisStatus string    `gorm:"type:varchar(50);default:'pending'" json:"analysis_status"` // pending, in_progress, completed, failed
	AnalysisError  string    `gorm:"type:text" json:"analysis_error,omitempty"`

	LastReviewedAt time.Time `json:"last_reviewed_at,omitempty"`
	ReviewsCount   int       `gorm:"default:0" json:"reviews_count"`

	LastSyncedAt time.Time `json:"last_synced_at,omitempty"`
	SyncStatus   string    `gorm:"type:varchar(50);default:'idle'" json:"sync_status"` // idle, syncing, error
	SyncError    string    `gorm:"type:text" json:"sync_error,omitempty"`

	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	DeletedAt *time.Time `gorm:"index" json:"deleted_at,omitempty"` // soft delete

	// Relationships
	Analyses     []CodeAnalysis         `gorm:"foreignKey:RepositoryID" json:"analyses,omitempty"`
	Webhooks     []Webhook              `gorm:"foreignKey:RepositoryID" json:"webhooks,omitempty"`
	Members      []User                 `gorm:"many2many:user_repositories;" json:"members,omitempty"`
	Dependencies []RepositoryDependency `gorm:"foreignKey:RepositoryID" json:"dependencies,omitempty"`
}

func (Repository) TableName() string {
	return "repositories"
}

func (r *Repository) IsValid() bool {
	return r.Name != "" && r.URL != "" && r.Type != "" && r.OwnerUserID != ""
}

func (r *Repository) NeedsAnalysis() bool {
	return r.LastAnalyzedAt.IsZero() ||
		time.Since(r.LastAnalyzedAt) > 24*time.Hour
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

type RepositoryDependency struct {
	ID           string `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	RepositoryID string `gorm:"type:uuid;index" json:"repository_id"`
	DependsOnID  string `gorm:"type:uuid;index" json:"depends_on_id"`

	Type       string `gorm:"type:varchar(50)" json:"type"` // import, library, service, etc
	IsOptional bool   `gorm:"default:false" json:"is_optional"`
	Version    string `gorm:"type:varchar(255)" json:"version,omitempty"`

	Repository Repository `gorm:"foreignKey:RepositoryID" json:"repository,omitempty"`
	DependsOn  Repository `gorm:"foreignKey:DependsOnID" json:"depends_on,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (RepositoryDependency) TableName() string {
	return "repository_dependencies"
}
