package models

import (
	"time"

	"gorm.io/datatypes"
)

type RepositoryGraphResponse struct {
	Nodes []RepositoryGraphNode `json:"nodes"`
	Edges []RepositoryGraphEdge `json:"edges"`
}

type RepositoryGraphNode struct {
	ID             string             `json:"id"`
	Name           string             `json:"name"`
	Description    string             `json:"description"`
	URL            string             `json:"url"`
	Type           RepositoryType     `json:"type"`
	Metadata       RepositoryMetadata `json:"metadata"`
	AnalysisStatus string             `json:"analysis_status"`
	SyncStatus     string             `json:"sync_status"`
}

type RepositoryGraphEdge struct {
	ID                 string                       `json:"id"`
	SourceRepositoryID string                       `json:"source_repository_id"`
	TargetRepositoryID string                       `json:"target_repository_id"`
	Kind               RepositoryRelationshipKind   `json:"kind"`
	Label              string                       `json:"label,omitempty"`
	Description        string                       `json:"description,omitempty"`
	Source             RepositoryRelationshipSource `json:"source"`
	Confidence         float64                      `json:"confidence"`
	Metadata           datatypes.JSONMap            `json:"metadata,omitempty"`
}

type CreateRepositoryRelationshipRequest struct {
	SourceRepositoryID string                     `json:"source_repository_id" binding:"required"`
	TargetRepositoryID string                     `json:"target_repository_id" binding:"required"`
	Kind               RepositoryRelationshipKind `json:"kind" binding:"required"`
	Label              string                     `json:"label,omitempty"`
	Description        string                     `json:"description,omitempty"`
	Metadata           datatypes.JSONMap          `json:"metadata,omitempty"`
}

type UpdateRepositoryRelationshipRequest struct {
	Kind        *RepositoryRelationshipKind `json:"kind,omitempty"`
	Label       *string                     `json:"label,omitempty"`
	Description *string                     `json:"description,omitempty"`
	Confidence  *float64                    `json:"confidence,omitempty"`
	Metadata    datatypes.JSONMap           `json:"metadata,omitempty"`
}

type RepositoryRelationshipResponse struct {
	ID                 string                       `json:"id"`
	OrganizationID     string                       `json:"organization_id"`
	SourceRepositoryID string                       `json:"source_repository_id"`
	TargetRepositoryID string                       `json:"target_repository_id"`
	Kind               RepositoryRelationshipKind   `json:"kind"`
	Label              string                       `json:"label,omitempty"`
	Description        string                       `json:"description,omitempty"`
	Source             RepositoryRelationshipSource `json:"source"`
	Confidence         float64                      `json:"confidence"`
	Metadata           datatypes.JSONMap            `json:"metadata,omitempty"`
	CreatedByUserID    string                       `json:"created_by_user_id,omitempty"`
	CreatedAt          time.Time                    `json:"created_at"`
	UpdatedAt          time.Time                    `json:"updated_at"`
}

func RepositoryRelationshipToResponse(r *RepositoryRelationship) *RepositoryRelationshipResponse {
	return &RepositoryRelationshipResponse{
		ID:                 r.ID,
		OrganizationID:     r.OrganizationID,
		SourceRepositoryID: r.SourceRepositoryID,
		TargetRepositoryID: r.TargetRepositoryID,
		Kind:               r.Kind,
		Label:              r.Label,
		Description:        r.Description,
		Source:             r.Source,
		Confidence:         r.Confidence,
		Metadata:           r.Metadata,
		CreatedByUserID:    r.CreatedByUserID,
		CreatedAt:          r.CreatedAt,
		UpdatedAt:          r.UpdatedAt,
	}
}

func RepositoryToGraphNode(r *Repository) RepositoryGraphNode {
	return RepositoryGraphNode{
		ID:             r.ID,
		Name:           r.Name,
		Description:    r.Description,
		URL:            r.URL,
		Type:           r.Type,
		Metadata:       r.Metadata,
		AnalysisStatus: r.AnalysisStatus,
		SyncStatus:     r.SyncStatus,
	}
}

func RepositoryRelationshipToGraphEdge(r *RepositoryRelationship, includeMetadata bool) RepositoryGraphEdge {
	edge := RepositoryGraphEdge{
		ID:                 r.ID,
		SourceRepositoryID: r.SourceRepositoryID,
		TargetRepositoryID: r.TargetRepositoryID,
		Kind:               r.Kind,
		Label:              r.Label,
		Description:        r.Description,
		Source:             r.Source,
		Confidence:         r.Confidence,
	}
	if includeMetadata {
		edge.Metadata = r.Metadata
	}
	return edge
}
