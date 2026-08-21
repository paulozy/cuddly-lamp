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
	ID              string             `json:"id"`
	Name            string             `json:"name"`
	Description     string             `json:"description"`
	URL             string             `json:"url"`
	Type            RepositoryType     `json:"type"`
	OrganizationID  string             `json:"organization_id"`
	OwnerUserID     string             `json:"owner_user_id,omitempty"`
	CreatedByUserID string             `json:"created_by_user_id,omitempty"`
	IsPublic        bool               `json:"is_public"`
	Metadata        RepositoryMetadata `json:"metadata"`
	CreatedAt       time.Time          `json:"created_at"`
	UpdatedAt       time.Time          `json:"updated_at"`

	// Zero-cost fields (already on repositories table)
	SyncStatus   string     `json:"sync_status,omitempty"`
	SyncError    string     `json:"sync_error,omitempty"`
	LastSyncedAt *time.Time `json:"last_synced_at,omitempty"`

	// Aggregated stats
	Stats RepositoryStats `json:"stats"`
}

type RepositoryListResponse struct {
	Items  []RepositoryResponse `json:"items"`
	Total  int64                `json:"total"`
	Limit  int                  `json:"limit"`
	Offset int                  `json:"offset"`
}

// RepositoryStats carries the coverage numbers reported by the repository's CI
// via the coverage upload endpoint. HasCoverage separates "never uploaded" from
// "uploaded and genuinely 0%", which the UI renders differently.
type RepositoryStats struct {
	HasCoverage        bool    `json:"has_coverage"`
	TestCoverage       float64 `json:"test_coverage"`
	TestedLines        int     `json:"tested_lines"`
	UncoveredLines     int     `json:"uncovered_lines"`
	CoverageStatus     string  `json:"coverage_status,omitempty"`
	CoverageUploadedAt *string `json:"coverage_uploaded_at,omitempty"`
}

func RepositoryToResponse(r *Repository) *RepositoryResponse {
	resp := &RepositoryResponse{
		ID:              r.ID,
		Name:            r.Name,
		Description:     r.Description,
		URL:             r.URL,
		Type:            r.Type,
		OrganizationID:  r.OrganizationID,
		OwnerUserID:     r.OwnerUserID,
		CreatedByUserID: r.CreatedByUserID,
		IsPublic:        r.IsPublic,
		Metadata:        r.Metadata,
		CreatedAt:       r.CreatedAt,
		UpdatedAt:       r.UpdatedAt,
		SyncStatus:      r.SyncStatus,
		SyncError:       r.SyncError,
	}
	if !r.LastSyncedAt.IsZero() {
		t := r.LastSyncedAt
		resp.LastSyncedAt = &t
	}

	// Populate Stats from EnrichedStats when available (list/get endpoints).
	// Falls back to zero-value RepositoryStats elsewhere.
	if r.EnrichedStats != nil {
		es := r.EnrichedStats
		resp.Stats = RepositoryStats{
			HasCoverage:        es.HasCoverage,
			TestCoverage:       es.TestCoverage,
			TestedLines:        es.TestedLines,
			UncoveredLines:     es.UncoveredLines,
			CoverageStatus:     es.CoverageStatus,
			CoverageUploadedAt: es.CoverageUploadedAt,
		}
	}

	return resp
}
