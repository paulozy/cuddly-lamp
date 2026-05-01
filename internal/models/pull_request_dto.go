package models

import "time"

type PullRequestResponse struct {
	ID             int64  `json:"id"`
	Number         int64  `json:"number"`
	Title          string `json:"title"`
	Body           string `json:"body,omitempty"`
	State          string `json:"state"`
	AuthorLogin    string `json:"author_login"`
	HeadBranch     string `json:"head_branch"`
	HeadSHA        string `json:"head_sha,omitempty"`
	BaseBranch     string `json:"base_branch"`
	BaseSHA        string `json:"base_sha,omitempty"`
	Draft          bool   `json:"draft"`
	CommitsCount   int    `json:"commits_count"`
	ChangedFiles   int    `json:"changed_files"`
	AdditionsCount int    `json:"additions_count"`
	DeletionsCount int    `json:"deletions_count"`
	HTMLURL        string `json:"html_url"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
	MergedAt       string `json:"merged_at,omitempty"`
}

type PullRequestListItemResponse struct {
	PullRequest    PullRequestResponse                `json:"pull_request"`
	LatestAnalysis *PullRequestReviewAnalysisResponse `json:"latest_analysis,omitempty"`
}

type PullRequestListResponse struct {
	Items []PullRequestListItemResponse `json:"items"`
	Total int                           `json:"total"`
}

type PullRequestDetailResponse struct {
	PullRequest    PullRequestResponse                `json:"pull_request"`
	Files          []PullRequestFileResponse          `json:"files"`
	LatestAnalysis *PullRequestReviewAnalysisResponse `json:"latest_analysis,omitempty"`
}

type PullRequestFilesResponse struct {
	Items []PullRequestFileResponse `json:"items"`
	Total int                       `json:"total"`
}

type PullRequestFileResponse struct {
	SHA       string `json:"sha"`
	Filename  string `json:"filename"`
	Status    string `json:"status"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Changes   int    `json:"changes"`
	Patch     string `json:"patch,omitempty"`
}

type PullRequestReviewAnalysisResponse struct {
	ID            string         `json:"id"`
	RepositoryID  string         `json:"repository_id"`
	PullRequestID int            `json:"pull_request_id"`
	Type          AnalysisType   `json:"type"`
	Status        AnalysisStatus `json:"status"`
	SummaryText   string         `json:"summary_text,omitempty"`
	Issues        []CodeIssue    `json:"issues"`
	IssueCount    int            `json:"issue_count"`
	CriticalCount int            `json:"critical_count"`
	ErrorCount    int            `json:"error_count"`
	WarningCount  int            `json:"warning_count"`
	InfoCount     int            `json:"info_count"`
	AIModel       string         `json:"ai_model,omitempty"`
	TokensUsed    int            `json:"tokens_used,omitempty"`
	ProcessingMs  int64          `json:"processing_ms,omitempty"`
	ErrorMessage  string         `json:"error_message,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
}

type CreatePullRequestReviewRequest struct {
	Event    string                     `json:"event" binding:"required"`
	Body     string                     `json:"body,omitempty"`
	Comments []PullRequestReviewComment `json:"comments,omitempty"`
}

type PullRequestReviewComment struct {
	Path     string `json:"path" binding:"required"`
	Position int    `json:"position" binding:"required"`
	Body     string `json:"body" binding:"required"`
}

type CreatePullRequestReviewResponse struct {
	ReviewID int64  `json:"review_id"`
	Event    string `json:"event"`
	Status   string `json:"status"`
}
