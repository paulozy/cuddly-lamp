package models

import "time"

type CreateRepositoryRequest struct {
	URL         string `json:"url" binding:"required"`
	Description string `json:"description"`
	IsPublic    bool   `json:"is_public"`
}

type UpdateRepositoryRequest struct {
	Description *string `json:"description"`
	IsPublic    *bool   `json:"is_public"`
}

type RepositoryResponse struct {
	ID          string             `json:"id"`
	Name        string             `json:"name"`
	Description string             `json:"description"`
	URL         string             `json:"url"`
	Type        RepositoryType     `json:"type"`
	OwnerUserID string             `json:"owner_user_id"`
	IsPublic    bool               `json:"is_public"`
	Metadata    RepositoryMetadata `json:"metadata"`
	CreatedAt   time.Time          `json:"created_at"`
	UpdatedAt   time.Time          `json:"updated_at"`
}

type RepositoryListResponse struct {
	Items  []RepositoryResponse `json:"items"`
	Total  int64                `json:"total"`
	Limit  int                  `json:"limit"`
	Offset int                  `json:"offset"`
}

func RepositoryToResponse(r *Repository) *RepositoryResponse {
	return &RepositoryResponse{
		ID:          r.ID,
		Name:        r.Name,
		Description: r.Description,
		URL:         r.URL,
		Type:        r.Type,
		OwnerUserID: r.OwnerUserID,
		IsPublic:    r.IsPublic,
		Metadata:    r.Metadata,
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
	}
}
