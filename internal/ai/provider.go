package ai

import "context"

// AnalysisType defines what type of analysis to perform
type AnalysisType string

const (
	AnalysisTypeCodeReview    AnalysisType = "code_review"
	AnalysisTypeSecurity      AnalysisType = "security"
	AnalysisTypeArchitecture  AnalysisType = "architecture"
)

// CommitSummary represents a simplified commit for AI analysis
type CommitSummary struct {
	SHA     string
	Message string
	Author  string
	Date    string // RFC3339 format
}

// ChangedFile represents a file changed in a PR diff
type ChangedFile struct {
	Path   string // file path
	Patch  string // unified diff content
	Status string // "added", "modified", "removed"
}

// AnalysisRequest contains all info needed for AI to analyze code
type AnalysisRequest struct {
	// Repository context
	RepositoryID  string
	RepoName      string
	Languages     []string
	Topics        []string
	HasCI         bool
	HasTests      bool
	TestCoverage  float32
	DefaultBranch string

	// Commit/Branch context
	Branch        string
	CommitSHA     string
	RecentCommits []CommitSummary

	// PR-specific context (populated when analyzing a PR)
	PullRequestID int64
	PRTitle       string
	PRBody        string
	PRAuthor      string
	ChangedFiles  []ChangedFile

	// Computed metrics (populated by local analysis)
	Metrics *CodeMetrics

	// Analysis parameters
	AnalysisType AnalysisType
}

// CodeIssue represents a single issue found by the AI
// Note: This mirrors models.CodeIssue structure
type CodeIssue struct {
	Category      string
	Severity      string // "critical", "high", "medium", "low", "info"
	Title         string
	Description   string
	Suggestion    string
	FilePath      string
	Line          int
	Column        int
	IsAIGenerated bool
	Confidence    float32
}

// CodeMetrics represents code quality metrics
// Note: This mirrors models.CodeMetrics structure
type CodeMetrics struct {
	LinesOfCode         int32
	CyclomaticComplexity int32
	TestCoverage        float32
	CodeDuplication     float32
	MaintainabilityIndex float32
}

// AnalysisResult is the output from AI analysis
type AnalysisResult struct {
	Summary    string
	Issues     []CodeIssue
	Metrics    CodeMetrics
	Model      string // e.g., "claude-3-5-haiku"
	TokensUsed int
}

// PRReviewComment represents a single comment on a specific line
type PRReviewComment struct {
	Path     string // file path in the PR
	Position int    // position in the diff (not absolute line number)
	Body     string // comment content
}

// PRReviewRequest represents a GitHub PR review to be posted
type PRReviewRequest struct {
	Body     string              // review summary
	Event    string              // "COMMENT", "APPROVE", or "REQUEST_CHANGES"
	Comments []PRReviewComment   // line-specific comments
}

// Analyzer is the extension point for pluggable AI providers
type Analyzer interface {
	// AnalyzeCode performs AI analysis on code and returns structured results
	AnalyzeCode(ctx context.Context, req *AnalysisRequest) (*AnalysisResult, error)

	// Provider returns the name of the AI provider
	Provider() string
}
