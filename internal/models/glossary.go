package models

import (
	"fmt"
	"strings"
	"time"
)

// GlossaryTerm is one piece of internal vocabulary — usually an acronym — that
// a newcomer has no way to look up.
//
// Terms live at organization scope rather than inside an onboarding flow: they
// are useful outside the onboarding too, and two flows referencing the same
// acronym should not need two copies of it.
type GlossaryTerm struct {
	ID             string `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	OrganizationID string `gorm:"type:uuid;not null;index" json:"organization_id"`

	Term       string `gorm:"type:varchar(120);not null" json:"term"`
	Definition string `gorm:"type:text;not null" json:"definition"`

	CreatedByUserID *string `gorm:"type:uuid" json:"created_by_user_id,omitempty"`

	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	DeletedAt *time.Time `gorm:"index" json:"deleted_at,omitempty"`
}

func (GlossaryTerm) TableName() string {
	return "organization_glossary_terms"
}

// Validate checks the term is worth storing. Uniqueness is case-insensitive and
// enforced by a partial unique index, not here.
func (t *GlossaryTerm) Validate() error {
	if strings.TrimSpace(t.Term) == "" {
		return fmt.Errorf("term is required")
	}
	if strings.TrimSpace(t.Definition) == "" {
		return fmt.Errorf("term %q needs a definition", t.Term)
	}
	return nil
}
