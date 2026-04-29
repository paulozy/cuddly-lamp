package models

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
	Total     int64          `json:"total"`
	Analyses  []CodeAnalysis `json:"analyses"`
	Limit     int            `json:"limit,omitempty"`
	Offset    int            `json:"offset,omitempty"`
}

// Note: ErrorResponse is likely already defined elsewhere in models.
// If not, uncomment the definition below:
// type ErrorResponse struct {
// 	Error            string `json:"error"`
// 	ErrorDescription string `json:"error_description,omitempty"`
// }
