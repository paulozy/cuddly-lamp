package models

import "time"

// MemberResponse is a row in the organization's member list.
type MemberResponse struct {
	UserID   string    `json:"user_id"`
	Email    string    `json:"email"`
	FullName string    `json:"full_name"`
	Role     UserRole  `json:"role"`
	IsActive bool      `json:"is_active"`
	JoinedAt time.Time `json:"joined_at"`
}

type MemberListResponse struct {
	Items []MemberResponse `json:"items"`
	Total int              `json:"total"`
}

type UpdateMemberRoleRequest struct {
	Role UserRole `json:"role" binding:"required"`
}

// InviteResponse never carries the token — that is returned exactly once, by
// CreateInviteResponse.
type InviteResponse struct {
	ID         string       `json:"id"`
	Email      string       `json:"email"`
	Role       UserRole     `json:"role"`
	Status     InviteStatus `json:"status"`
	ExpiresAt  time.Time    `json:"expires_at"`
	CreatedAt  time.Time    `json:"created_at"`
	AcceptedAt *time.Time   `json:"accepted_at,omitempty"`
	RevokedAt  *time.Time   `json:"revoked_at,omitempty"`
}

type InviteListResponse struct {
	Items []InviteResponse `json:"items"`
	Total int              `json:"total"`
}

type CreateInviteRequest struct {
	Email string   `json:"email" binding:"required"`
	Role  UserRole `json:"role"`
	// TTLHours overrides the default invite lifetime. Optional.
	TTLHours int `json:"ttl_hours,omitempty"`
}

// CreateInviteResponse is the only place the plaintext token is ever returned.
// The client must surface it immediately; it cannot be recovered afterwards.
type CreateInviteResponse struct {
	Invite InviteResponse `json:"invite"`
	Token  string         `json:"token"`
}

func OrganizationInviteToResponse(invite *OrganizationInvite, now time.Time) InviteResponse {
	return InviteResponse{
		ID:         invite.ID,
		Email:      invite.Email,
		Role:       invite.Role,
		Status:     invite.Status(now),
		ExpiresAt:  invite.ExpiresAt,
		CreatedAt:  invite.CreatedAt,
		AcceptedAt: invite.AcceptedAt,
		RevokedAt:  invite.RevokedAt,
	}
}
