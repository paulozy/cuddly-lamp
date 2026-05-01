package ai

import "context"

// AnalysisType defines what type of analysis to perform
type AnalysisType string

const (
	AnalysisTypeCodeReview   AnalysisType = "code_review"
	AnalysisTypeSecurity     AnalysisType = "security"
	AnalysisTypeArchitecture AnalysisType = "architecture"
	AnalysisTypeDependency   AnalysisType = "dependency"
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
	PullRequestID  int64
	PRTitle        string
	PRBody         string
	PRAuthor       string
	ChangedFiles   []ChangedFile
	TruncatedFiles []string

	// Computed metrics (populated by local analysis)
	Metrics *CodeMetrics

	// Generated project documentation and standards used as analysis context.
	ProjectStandards string

	// Analysis parameters
	AnalysisType AnalysisType
}

type DocumentationType string

const (
	DocumentationTypeADR          DocumentationType = "adr"
	DocumentationTypeArchitecture DocumentationType = "architecture"
	DocumentationTypeServiceDoc   DocumentationType = "service_doc"
	DocumentationTypeGuidelines   DocumentationType = "guidelines"
)

type DocumentationRequest struct {
	Type            DocumentationType
	RepositoryID    string
	RepoName        string
	Branch          string
	Languages       []string
	Frameworks      []string
	Topics          []string
	ContextMarkdown string
}

type DocumentationResult struct {
	Content    string
	Model      string
	TokensUsed int
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

	// Security-specific fields (optional)
	CWEID         string
	OWASPCategory string

	// Architecture-specific fields (optional)
	Pattern      string
	DebtCategory string
}

// CodeMetrics represents code quality metrics
// Note: This mirrors models.CodeMetrics structure
type CodeMetrics struct {
	LinesOfCode          int32
	CyclomaticComplexity int32
	TestCoverage         float32
	CodeDuplication      float32
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
	Body     string            // review summary
	Event    string            // "COMMENT", "APPROVE", or "REQUEST_CHANGES"
	Comments []PRReviewComment // line-specific comments
}

// Analyzer is the extension point for pluggable AI providers
type Analyzer interface {
	// AnalyzeCode performs AI analysis on code and returns structured results
	AnalyzeCode(ctx context.Context, req *AnalysisRequest) (*AnalysisResult, error)

	// Provider returns the name of the AI provider
	Provider() string
}

type DocumentationGenerator interface {
	GenerateDocumentation(ctx context.Context, req *DocumentationRequest) (*DocumentationResult, error)
	Provider() string
}

// SearchSnippet represents a single code chunk returned by semantic search
// that will be passed to the synthesizer for summarization.
type SearchSnippet struct {
	FilePath  string
	Content   string
	Language  string
	StartLine int
	EndLine   int
	Score     float64
}

// SynthesisEventKind enumerates the kinds of events produced during a streaming
// synthesis call.
type SynthesisEventKind string

const (
	SynthesisEventTextDelta SynthesisEventKind = "text_delta"
	SynthesisEventUsage     SynthesisEventKind = "usage"
	SynthesisEventDone      SynthesisEventKind = "done"
	SynthesisEventError     SynthesisEventKind = "error"
)

// SynthesisUsage carries the final token accounting reported by the model.
type SynthesisUsage struct {
	InputTokens  int
	OutputTokens int
	Model        string
}

// SynthesisEvent is a single event emitted during a streaming synthesis.
// Exactly one of Text, Usage, or Err is meaningful per Kind:
//   - text_delta -> Text holds the incremental text fragment
//   - usage      -> Usage holds the final token totals
//   - done       -> terminal signal, fields are empty
//   - error      -> Err carries the failure reason
type SynthesisEvent struct {
	Kind  SynthesisEventKind
	Text  string
	Usage *SynthesisUsage
	Err   error
}

// Synthesizer streams a natural-language summary over a set of semantic search
// snippets. Implementations must close the returned channel when the stream
// ends (after a done or error event), and must respect ctx cancellation.
type Synthesizer interface {
	StreamSearchSynthesis(ctx context.Context, query string, snippets []SearchSnippet) (<-chan SynthesisEvent, error)
}
