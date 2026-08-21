package models

import "time"

type CreateTeamRequest struct {
	Name        string `json:"name" binding:"required"`
	Slug        string `json:"slug,omitempty"`
	Description string `json:"description,omitempty"`
}

type UpdateTeamRequest struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
}

type TeamResponse struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	Slug            string     `json:"slug"`
	Description     string     `json:"description,omitempty"`
	Source          TeamSource `json:"source"`
	MemberCount     int        `json:"member_count"`
	RepositoryCount int        `json:"repository_count"`
	CreatedAt       time.Time  `json:"created_at"`
}

type TeamListResponse struct {
	Items []TeamResponse `json:"items"`
	Total int            `json:"total"`
}

type TeamMemberResponse struct {
	UserID   string   `json:"user_id"`
	Email    string   `json:"email"`
	FullName string   `json:"full_name"`
	Role     TeamRole `json:"role"`
}

type TeamMemberListResponse struct {
	Items []TeamMemberResponse `json:"items"`
	Total int                  `json:"total"`
}

type AddTeamMemberRequest struct {
	UserID string   `json:"user_id" binding:"required"`
	Role   TeamRole `json:"role,omitempty"`
}

// SetRepositoryOwnerRequest assigns or clears a repository's owning team. A null
// TeamID clears ownership, which is a legitimate state — "unowned" is what the
// catalog reports for anything nobody has claimed.
type SetRepositoryOwnerRequest struct {
	TeamID *string `json:"team_id"`
}

// TeamRef is the owner summary embedded in repository payloads, so the catalog
// can render "owned by X" without a second request.
type TeamRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

func TeamToResponse(t *Team) TeamResponse {
	return TeamResponse{
		ID:              t.ID,
		Name:            t.Name,
		Slug:            t.Slug,
		Description:     t.Description,
		Source:          t.Source(),
		MemberCount:     t.MemberCount,
		RepositoryCount: t.RepositoryCount,
		CreatedAt:       t.CreatedAt,
	}
}
