package models

import (
	"time"

	"gorm.io/datatypes"
)

type DocGenerationStatus string

const (
	DocGenerationStatusPending    DocGenerationStatus = "pending"
	DocGenerationStatusInProgress DocGenerationStatus = "in_progress"
	DocGenerationStatusCompleted  DocGenerationStatus = "completed"
	DocGenerationStatusFailed     DocGenerationStatus = "failed"
)

// DocGenerationScope distinguishes between per-repo documentation (delivered
// via GitHub PR) and org-wide documentation (stored in the backend, surfaced
// in the IDP UI without any PR).
type DocGenerationScope string

const (
	DocGenerationScopeRepo DocGenerationScope = "repo"
	DocGenerationScopeOrg  DocGenerationScope = "org"
)

// DocProgressStage gives the UI a coarse-grained progress signal between
// `pending` and `completed` so the polling loop can show "Aggregating
// context…" / "Calling Claude…" instead of a generic spinner.
type DocProgressStage string

const (
	DocProgressStageAggregatingContext DocProgressStage = "aggregating_context"
	DocProgressStageCallingClaude      DocProgressStage = "calling_claude"
	DocProgressStagePersisting         DocProgressStage = "persisting"
)

type DocGeneration struct {
	ID string `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`

	OrganizationID string        `gorm:"type:uuid;not null;index" json:"organization_id"`
	Organization   *Organization `gorm:"foreignKey:OrganizationID" json:"organization,omitempty"`

	// Scope discriminates org-level docs (no repository_id) from per-repo
	// docs (with repository_id set). Enforced by a DB CHECK constraint.
	Scope DocGenerationScope `gorm:"type:varchar(8);not null;default:'repo'" json:"scope"`

	// RepositoryID is nullable in the DB (org-level rows leave it empty).
	RepositoryID *string     `gorm:"type:uuid;index" json:"repository_id,omitempty"`
	Repository   *Repository `gorm:"foreignKey:RepositoryID" json:"repository,omitempty"`

	// TemplateID stores which template was used for ADR org-level rows
	// (e.g. "adr-tech-choice"). NULL for non-ADR docs.
	TemplateID *string `gorm:"type:varchar(64)" json:"template_id,omitempty"`

	// SupersededByID links to the row that replaced this one when the user
	// edits the doc manually. The UI lists only the "head" of each chain
	// (rows where superseded_by_id IS NULL).
	SupersededByID *string `gorm:"type:uuid" json:"superseded_by_id,omitempty"`

	// ProgressStage is updated by the worker as it advances through phases.
	// NULL when the doc is already in a terminal state.
	ProgressStage *DocProgressStage `gorm:"type:varchar(48)" json:"progress_stage,omitempty"`

	Status            DocGenerationStatus                   `gorm:"type:varchar(50);not null;default:'pending';index" json:"status"`
	Types             datatypes.JSONSlice[string]           `gorm:"type:jsonb;not null;default:'[]'" json:"types"`
	Branch            string                                `gorm:"type:varchar(255)" json:"branch,omitempty"`
	GenBranch         string                                `gorm:"type:varchar(255)" json:"gen_branch,omitempty"`
	PullRequestURL    string                                `gorm:"type:text" json:"pull_request_url,omitempty"`
	PullRequestNumber int                                   `json:"pull_request_number,omitempty"`
	Content           datatypes.JSONType[map[string]string] `gorm:"type:jsonb;not null;default:'{}'" json:"content"`
	TokensUsed        int                                   `gorm:"not null;default:0" json:"tokens_used"`
	ErrorMessage      string                                `gorm:"type:text" json:"error_message,omitempty"`
	TriggeredByUserID string                                `gorm:"type:uuid" json:"triggered_by_user_id,omitempty"`

	// UserPrompt holds the free-text "topic" the user typed when generating
	// an ADR org-level doc (e.g. "Should we standardize on PostgreSQL?").
	// Empty for non-ADR and per-repo flows.
	UserPrompt string `gorm:"type:text" json:"user_prompt,omitempty"`

	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	DeletedAt *time.Time `gorm:"index" json:"deleted_at,omitempty"`
}

func (DocGeneration) TableName() string {
	return "doc_generations"
}

func (d *DocGeneration) IsValid() bool {
	if d.OrganizationID == "" || len(d.Types) == 0 {
		return false
	}
	switch d.Scope {
	case DocGenerationScopeRepo:
		return d.RepositoryID != nil && *d.RepositoryID != ""
	case DocGenerationScopeOrg:
		return d.RepositoryID == nil
	default:
		return false
	}
}
