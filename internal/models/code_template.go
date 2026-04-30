package models

import (
	"time"

	"github.com/paulozy/idp-with-ai-backend/internal/ai"
	"gorm.io/datatypes"
)

type TemplateStatus string

const (
	TemplateStatusPending    TemplateStatus = "pending"
	TemplateStatusGenerating TemplateStatus = "generating"
	TemplateStatusCompleted  TemplateStatus = "completed"
	TemplateStatusFailed     TemplateStatus = "failed"
)

type CodeTemplate struct {
	ID string `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`

	OrganizationID  string                                 `gorm:"type:uuid;index;not null" json:"organization_id"`
	Organization    *Organization                          `gorm:"foreignKey:OrganizationID" json:"organization,omitempty"`
	RepositoryID    *string                                `gorm:"type:uuid;index" json:"repository_id,omitempty"`
	Repository      *Repository                            `gorm:"foreignKey:RepositoryID" json:"repository,omitempty"`
	CreatedByUserID *string                                `gorm:"type:uuid" json:"created_by_user_id,omitempty"`
	CreatedByUser   *User                                  `gorm:"foreignKey:CreatedByUserID" json:"created_by_user,omitempty"`
	Prompt          string                                 `gorm:"type:text;not null" json:"prompt"`
	StackHint       string                                 `gorm:"type:text" json:"stack_hint,omitempty"`
	StackSnapshot   datatypes.JSONType[ai.StackProfile]    `gorm:"type:jsonb" json:"stack_snapshot"`
	Status          TemplateStatus                         `gorm:"type:varchar(50);index" json:"status"`
	Summary         string                                 `gorm:"type:text" json:"summary,omitempty"`
	Files           datatypes.JSONType[[]ai.GeneratedFile] `gorm:"type:jsonb" json:"files"`
	AIModel         string                                 `gorm:"type:varchar(100)" json:"ai_model,omitempty"`
	TokensUsed      int                                    `json:"tokens_used,omitempty"`
	ProcessingMs    int64                                  `json:"processing_ms,omitempty"`
	ErrorMessage    string                                 `gorm:"type:text" json:"error_message,omitempty"`
	IsPinned        bool                                   `gorm:"default:false;index" json:"is_pinned"`
	PinnedByUserID  *string                                `gorm:"type:uuid" json:"pinned_by_user_id,omitempty"`
	PinnedByUser    *User                                  `gorm:"foreignKey:PinnedByUserID" json:"pinned_by_user,omitempty"`
	PinnedAt        *time.Time                             `json:"pinned_at,omitempty"`
	Name            string                                 `gorm:"type:varchar(255)" json:"name,omitempty"`
	CreatedAt       time.Time                              `json:"created_at"`
	UpdatedAt       time.Time                              `json:"updated_at"`
	DeletedAt       *time.Time                             `gorm:"index" json:"deleted_at,omitempty"`
}

func (CodeTemplate) TableName() string {
	return "code_templates"
}

func (ct *CodeTemplate) IsValid() bool {
	return ct.OrganizationID != "" && ct.Prompt != ""
}
