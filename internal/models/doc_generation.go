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

type DocGeneration struct {
	ID string `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`

	RepositoryID string      `gorm:"type:uuid;not null;index" json:"repository_id"`
	Repository   *Repository `gorm:"foreignKey:RepositoryID" json:"repository,omitempty"`

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

	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	DeletedAt *time.Time `gorm:"index" json:"deleted_at,omitempty"`
}

func (DocGeneration) TableName() string {
	return "doc_generations"
}

func (d *DocGeneration) IsValid() bool {
	return d.RepositoryID != "" && len(d.Types) > 0
}
