package models

import "time"

// TeamRole is scoped to the team itself and is independent of the organization
// role. A lead may curate the team's membership; it confers nothing elsewhere.
type TeamRole string

const (
	TeamRoleMember TeamRole = "member"
	TeamRoleLead   TeamRole = "lead"
)

// TeamSource records where a team came from. Only `local` is produced today;
// `github` is reserved for the import path, which will treat imported membership
// as read-only so a sync pass cannot silently revert someone's edit.
type TeamSource string

const (
	TeamSourceLocal  TeamSource = "local"
	TeamSourceGitHub TeamSource = "github"
)

// Team is the unit of ownership. A repository points at exactly one, or at none.
type Team struct {
	ID             string        `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	OrganizationID string        `gorm:"type:uuid;not null;index" json:"organization_id"`
	Organization   *Organization `gorm:"foreignKey:OrganizationID" json:"-"`

	Name        string `gorm:"type:varchar(255);not null" json:"name"`
	Slug        string `gorm:"type:varchar(120);not null" json:"slug"`
	Description string `gorm:"type:text" json:"description,omitempty"`

	// Provider/ExternalID stay empty for locally managed teams. When the GitHub
	// import lands, ExternalID holds the numeric team id — never the slug, which
	// GitHub regenerates on rename.
	Provider   string `gorm:"type:varchar(50)" json:"provider,omitempty"`
	ExternalID string `gorm:"type:varchar(255)" json:"external_id,omitempty"`

	CreatedByUserID *string `gorm:"type:uuid" json:"created_by_user_id,omitempty"`

	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	DeletedAt *time.Time `gorm:"index" json:"deleted_at,omitempty"`

	// MemberCount is populated by list queries; never persisted.
	MemberCount int `gorm:"-" json:"member_count"`
	// RepositoryCount is populated by list queries; never persisted.
	RepositoryCount int `gorm:"-" json:"repository_count"`
}

func (Team) TableName() string {
	return "teams"
}

func (t *Team) IsValid() bool {
	return t.Name != "" && t.Slug != "" && t.OrganizationID != ""
}

// Source reports whether the team is managed here or mirrored from a provider.
// Imported teams must not accept local membership edits.
func (t *Team) Source() TeamSource {
	if t.ExternalID != "" {
		return TeamSourceGitHub
	}
	return TeamSourceLocal
}

// TeamMember is a first-class model rather than a GORM many2many join, matching
// OrganizationMember. Association mode would clobber Role on write.
type TeamMember struct {
	TeamID string   `gorm:"type:uuid;primaryKey" json:"team_id"`
	UserID string   `gorm:"type:uuid;primaryKey" json:"user_id"`
	Role   TeamRole `gorm:"type:varchar(50);not null;default:'member'" json:"role"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	Team *Team `gorm:"foreignKey:TeamID" json:"-"`
	User User  `gorm:"foreignKey:UserID" json:"-"`
}

func (TeamMember) TableName() string {
	return "team_members"
}
