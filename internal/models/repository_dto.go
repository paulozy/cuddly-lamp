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
	AnalysisStatus string `json:"analysis_status,omitempty"`
	AnalysisError  string `json:"analysis_error,omitempty"`
	ReviewsCount   int    `json:"reviews_count,omitempty"`
	SyncStatus     string `json:"sync_status,omitempty"`
	SyncError      string `json:"sync_error,omitempty"`
	LastSyncedAt   *time.Time `json:"last_synced_at,omitempty"`

	// Aggregated stats
	Stats RepositoryStats `json:"stats"`
}

type RepositoryListResponse struct {
	Items  []RepositoryResponse `json:"items"`
	Total  int64                `json:"total"`
	Limit  int                  `json:"limit"`
	Offset int                  `json:"offset"`
}

// RepositoryStats contains aggregated statistics from related tables.
type RepositoryStats struct {
	TotalAnalyses  int     `json:"total_analyses"`
	LatestScore    int     `json:"latest_quality_score"`
	HasAnalysis    bool    `json:"has_analysis"`
	LastAnalyzedAt *string `json:"last_analyzed_at"`
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
		AnalysisStatus:  r.AnalysisStatus,
		AnalysisError:   r.AnalysisError,
		ReviewsCount:    r.ReviewsCount,
		SyncStatus:      r.SyncStatus,
		SyncError:       r.SyncError,
	}
	if !r.LastSyncedAt.IsZero() {
		t := r.LastSyncedAt
		resp.LastSyncedAt = &t
	}

	// Populate Stats from EnrichedStats when available (list endpoint).
	// Falls back to zero-value RepositoryStats for single-get endpoint.
	if r.EnrichedStats != nil {
		es := r.EnrichedStats
		score := 0
		if es.HasMetricsAnalysis {
			// Reuse existing GetQualityScore logic inline to avoid constructing
			// a full CodeAnalysis object.
			score = computeQualityScore(es.IssueCount, es.CriticalCount, es.ErrorCount, es.WarningCount, es.TestCoverage, es.AvgComplexity, es.CoverageStatus)
		}
		resp.Stats = RepositoryStats{
			TotalAnalyses:  es.TotalAnalyses,
			LatestScore:    score,
			HasAnalysis:    es.TotalAnalyses > 0,
			LastAnalyzedAt: es.LatestAnalyzedAt,
		}
	}

	return resp
}

// computeQualityScore mirrors CodeAnalysis.GetQualityScore() exactly.
// It is kept here so the DTO layer does not import the analysis model.
// coverageStatus controls whether the coverage deduction applies — values
// other than "ok"/"partial" mean the report was missing or invalid, in which
// case we don't penalize the repo for an unmeasured 0%.
func computeQualityScore(issueCount, criticalCount, errorCount, warningCount int, testCoverage, avgComplexity float64, coverageStatus string) int {
	coverageMeasured := coverageStatus == "ok" || coverageStatus == "partial"

	if issueCount == 0 && coverageMeasured && testCoverage >= 80 {
		return 100
	}
	score := 100
	issueDeduction := criticalCount*5 + errorCount*3 + warningCount
	if issueDeduction > 50 {
		issueDeduction = 50
	}
	score -= issueDeduction
	if coverageMeasured && testCoverage < 80 {
		coverageDeduction := int((80.0 - testCoverage) / 4)
		if coverageDeduction > 20 {
			coverageDeduction = 20
		}
		score -= coverageDeduction
	}
	if avgComplexity > 5 {
		complexityDeduction := int((avgComplexity - 5) * 2)
		if complexityDeduction > 10 {
			complexityDeduction = 10
		}
		score -= complexityDeduction
	}
	if score < 0 {
		return 0
	}
	return score
}
