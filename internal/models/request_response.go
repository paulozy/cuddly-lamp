package models

import "time"

// AnalyzeRepositoryRequest represents a request to analyze a repository
type AnalyzeRepositoryRequest struct {
	Type      string `json:"type,omitempty"`       // code_review, security, architecture
	Branch    string `json:"branch,omitempty"`     // branch to analyze (default: main/master)
	CommitSHA string `json:"commit_sha,omitempty"` // specific commit to analyze
}

// JobResponse represents a queued job
type JobResponse struct {
	Status string `json:"status"` // queued, processing, completed, failed
	Type   string `json:"type"`   // job type (e.g., repo:analyze)
	Target string `json:"target"` // resource being processed
}

// AnalysisListResponse represents a list of analyses
type AnalysisListResponse struct {
	Total    int64          `json:"total"`
	Analyses []CodeAnalysis `json:"analyses"`
	Limit    int            `json:"limit,omitempty"`
	Offset   int            `json:"offset,omitempty"`
}

type PackageDependencyResponse struct {
	ID                 string    `json:"id"`
	RepositoryID       string    `json:"repository_id"`
	Name               string    `json:"name"`
	CurrentVersion     string    `json:"current_version"`
	LatestVersion      string    `json:"latest_version"`
	Ecosystem          string    `json:"ecosystem"`
	ManifestFile       string    `json:"manifest_file"`
	IsDirectDependency bool      `json:"is_direct_dependency"`
	IsVulnerable       bool      `json:"is_vulnerable"`
	VulnerabilityCVEs  []string  `json:"vulnerability_cves"`
	UpdateAvailable    bool      `json:"update_available"`
	LastScannedAt      time.Time `json:"last_scanned_at"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type PackageDependencyListResponse struct {
	Items  []PackageDependencyResponse `json:"items"`
	Total  int64                       `json:"total"`
	Limit  int                         `json:"limit"`
	Offset int                         `json:"offset"`
}

func PackageDependencyToResponse(dep *PackageDependency) PackageDependencyResponse {
	if dep == nil {
		return PackageDependencyResponse{}
	}

	return PackageDependencyResponse{
		ID:                 dep.ID,
		RepositoryID:       dep.RepositoryID,
		Name:               dep.Name,
		CurrentVersion:     dep.CurrentVersion,
		LatestVersion:      dep.LatestVersion,
		Ecosystem:          dep.Ecosystem,
		ManifestFile:       dep.ManifestFile,
		IsDirectDependency: dep.IsDirectDependency,
		IsVulnerable:       dep.IsVulnerable,
		VulnerabilityCVEs:  append([]string(nil), dep.VulnerabilityCVEs...),
		UpdateAvailable:    dep.UpdateAvailable,
		LastScannedAt:      dep.LastScannedAt,
		CreatedAt:          dep.CreatedAt,
		UpdatedAt:          dep.UpdatedAt,
	}
}

type GenerateEmbeddingsRequest struct {
	Branch    string `json:"branch,omitempty"`
	CommitSHA string `json:"commit_sha,omitempty"`
}

type SemanticSearchResponse struct {
	Query   string                 `json:"query"`
	Total   int                    `json:"total"`
	Results []SemanticSearchResult `json:"results"`
}

type SemanticSearchResult struct {
	FilePath  string  `json:"file_path"`
	Content   string  `json:"content"`
	Language  string  `json:"language,omitempty"`
	StartLine int     `json:"start_line,omitempty"`
	EndLine   int     `json:"end_line,omitempty"`
	Score     float64 `json:"score"`
	Provider  string  `json:"provider"`
	Model     string  `json:"model"`
	Branch    string  `json:"branch,omitempty"`
}

// Note: ErrorResponse is likely already defined elsewhere in models.
// If not, uncomment the definition below:
// type ErrorResponse struct {
// 	Error            string `json:"error"`
// 	ErrorDescription string `json:"error_description,omitempty"`
// }
