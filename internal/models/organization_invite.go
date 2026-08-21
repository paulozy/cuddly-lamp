package models

import (
	"strings"
	"time"
)

// OrganizationInvite is the gate for joining an existing organization.
//
// Registration used to accept any organization slug and add the caller as a
// developer, which meant guessing a slug was enough to get inside. An invite is
// now required, and it is bound to a specific e-mail so a leaked link cannot be
// redeemed by whoever finds it.
//
// Like CoverageUploadToken, the plaintext is presented once on creation and only
// the SHA-256 hash is persisted. Acceptance and revocation flip timestamps rather
// than deleting the row, so there is a record of who admitted whom.
type OrganizationInvite struct {
	ID string `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`

	OrganizationID string        `gorm:"type:uuid;not null;index" json:"organization_id"`
	Organization   *Organization `gorm:"foreignKey:OrganizationID" json:"-"`

	Email string   `gorm:"type:varchar(255);not null" json:"email"`
	Role  UserRole `gorm:"type:varchar(50);not null;default:'developer'" json:"role"`

	TokenHash string `gorm:"type:varchar(64);not null;uniqueIndex" json:"-"`

	CreatedByUserID *string `gorm:"type:uuid" json:"created_by_user_id,omitempty"`
	CreatedByUser   *User   `gorm:"foreignKey:CreatedByUserID" json:"-"`

	AcceptedAt       *time.Time `json:"accepted_at,omitempty"`
	AcceptedByUserID *string    `gorm:"type:uuid" json:"accepted_by_user_id,omitempty"`
	AcceptedByUser   *User      `gorm:"foreignKey:AcceptedByUserID" json:"-"`

	RevokedAt *time.Time `json:"revoked_at,omitempty"`
	ExpiresAt time.Time  `gorm:"not null" json:"expires_at"`

	CreatedAt time.Time `json:"created_at"`
}

func (OrganizationInvite) TableName() string {
	return "organization_invites"
}

// IsRedeemable reports whether the invite can still be accepted. Single-use is
// enforced here: once AcceptedAt is set the invite is spent, even if it has not
// expired.
func (i *OrganizationInvite) IsRedeemable(now time.Time) bool {
	if i.AcceptedAt != nil || i.RevokedAt != nil {
		return false
	}
	return i.ExpiresAt.After(now)
}

// MatchesEmail compares the invited address to the one registering. Comparison is
// case-insensitive and whitespace-tolerant because e-mail is; without this check an
// invite link would grant access to anyone holding it, not just the intended person.
func (i *OrganizationInvite) MatchesEmail(email string) bool {
	return strings.EqualFold(strings.TrimSpace(i.Email), strings.TrimSpace(email))
}

// InviteStatus is the derived state the settings screen renders.
type InviteStatus string

const (
	InviteStatusPending  InviteStatus = "pending"
	InviteStatusAccepted InviteStatus = "accepted"
	InviteStatusRevoked  InviteStatus = "revoked"
	InviteStatusExpired  InviteStatus = "expired"
)

func (i *OrganizationInvite) Status(now time.Time) InviteStatus {
	switch {
	case i.AcceptedAt != nil:
		return InviteStatusAccepted
	case i.RevokedAt != nil:
		return InviteStatusRevoked
	case !i.ExpiresAt.After(now):
		return InviteStatusExpired
	default:
		return InviteStatusPending
	}
}
