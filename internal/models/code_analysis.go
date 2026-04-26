package models

import (
	"database/sql/driver"
	"encoding/json"
	"time"

	"gorm.io/datatypes"
)

type AnalysisType string

const (
	AnalysisTypeCodeReview   AnalysisType = "code_review"
	AnalysisTypeMetrics      AnalysisType = "metrics"
	AnalysisTypeDependency   AnalysisType = "dependency"
	AnalysisTypeSecurity     AnalysisType = "security"
	AnalysisTypeArchitecture AnalysisType = "architecture"
)

type AnalysisStatus string

const (
	AnalysisStatusPending    AnalysisStatus = "pending"
	AnalysisStatusProcessing AnalysisStatus = "processing"
	AnalysisStatusCompleted  AnalysisStatus = "completed"
	AnalysisStatusFailed     AnalysisStatus = "failed"
	AnalysisStatusPartial    AnalysisStatus = "partial"
)

type SeverityLevel string

const (
	SeverityInfo     SeverityLevel = "info"
	SeverityWarning  SeverityLevel = "warning"
	SeverityError    SeverityLevel = "error"
	SeverityCritical SeverityLevel = "critical"
)

type CodeIssue struct {
	ID          string        `json:"id"`
	File        string        `json:"file"`
	Line        int           `json:"line,omitempty"`
	Column      int           `json:"column,omitempty"`
	Severity    SeverityLevel `json:"severity"`
	Category    string        `json:"category"` // e.g., "bug", "performance", "style", "security"
	Title       string        `json:"title"`
	Description string        `json:"description"`
	Suggestion  string        `json:"suggestion,omitempty"`
	Code        string        `json:"code,omitempty"`

	IsAIGenerated bool    `json:"is_ai_generated"`
	Confidence    float64 `json:"confidence,omitempty"` // 0-1 confidence score

	URL           string   `json:"url,omitempty"` // Link to docs/example
	RelatedIssues []string `json:"related_issues,omitempty"`
}

type CodeMetrics struct {
	TotalLines   int     `json:"total_lines"`
	CodeLines    int     `json:"code_lines"`
	CommentLines int     `json:"comment_lines"`
	BlankLines   int     `json:"blank_lines"`
	CommentRatio float64 `json:"comment_ratio"` // Comments / Total lines

	// Complexity
	CyclomaticComplexity    float64 `json:"cyclomatic_complexity"`
	AvgCyclomaticComplexity float64 `json:"avg_cyclomatic_complexity"`
	CognitiveComplexity     float64 `json:"cognitive_complexity,omitempty"`

	TestCoverage   float64 `json:"test_coverage"` // 0-100
	TestedLines    int     `json:"tested_lines"`
	UncoveredLines int     `json:"uncovered_lines"`
	TestsCount     int     `json:"tests_count"`
	TestPassRate   float64 `json:"test_pass_rate,omitempty"`

	DuplicatedLines  int     `json:"duplicated_lines"`
	DuplicationRatio float64 `json:"duplication_ratio"` // Duplicated / Total

	// Maintainability
	HalsteadMetrics      *HalsteadMetrics `json:"halstead_metrics,omitempty"`
	MaintainabilityIndex float64          `json:"maintainability_index"` // 0-100

	Languages map[string]int `json:"languages,omitempty"` // {language: line_count}
}

type HalsteadMetrics struct {
	DistinctOperators int     `json:"distinct_operators"`
	DistinctOperands  int     `json:"distinct_operands"`
	TotalOperators    int     `json:"total_operators"`
	TotalOperands     int     `json:"total_operands"`
	ProgramLength     int     `json:"program_length"`
	ProgramVolume     float64 `json:"program_volume"`
	Difficulty        float64 `json:"difficulty"`
	Effort            float64 `json:"effort"`
	TimeToUnderstand  float64 `json:"time_to_understand"` // Minutes
	BugsEstimate      float64 `json:"bugs_estimate"`
}

func (cm *CodeMetrics) Scan(value interface{}) error {
	bytes, _ := value.([]byte)
	return json.Unmarshal(bytes, &cm)
}

func (cm CodeMetrics) Value() (driver.Value, error) {
	return json.Marshal(cm)
}

type CodeAnalysis struct {
	ID string `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`

	RepositoryID string      `gorm:"type:uuid;index" json:"repository_id"`
	Repository   *Repository `gorm:"foreignKey:RepositoryID" json:"repository,omitempty"`

	Type        AnalysisType   `gorm:"type:varchar(50);index" json:"type"`
	Status      AnalysisStatus `gorm:"type:varchar(50);index" json:"status"`
	Title       string         `gorm:"type:varchar(500)" json:"title"`
	Description string         `gorm:"type:text" json:"description,omitempty"`

	// Scope
	CommitSHA     string `gorm:"type:varchar(255);index" json:"commit_sha,omitempty"`
	Branch        string `gorm:"type:varchar(255)" json:"branch,omitempty"`
	PullRequestID *int   `gorm:"index" json:"pull_request_id,omitempty"`
	FilePath      string `gorm:"type:varchar(1000)" json:"file_path,omitempty"` // If analysis is for specific file

	Issues      datatypes.JSONType[[]CodeIssue] `gorm:"type:jsonb" json:"issues"` // Array of CodeIssue
	Metrics     CodeMetrics                     `gorm:"type:jsonb" json:"metrics,omitempty"`
	SummaryText string                          `gorm:"type:text" json:"summary_text,omitempty"` // Human readable summary

	IssueCount    int `gorm:"index" json:"issue_count"`
	CriticalCount int `json:"critical_count"`
	ErrorCount    int `json:"error_count"`
	WarningCount  int `json:"warning_count"`
	InfoCount     int `json:"info_count"`

	TriggeredBy   string `gorm:"type:varchar(255)" json:"triggered_by"` // user, webhook, schedule
	TriggeredByID string `gorm:"type:uuid" json:"triggered_by_id,omitempty"`

	IsAIAnalysis bool   `gorm:"default:true" json:"is_ai_analysis"`
	AIModel      string `gorm:"type:varchar(100)" json:"ai_model,omitempty"` // claude-3-sonnet, etc
	TokensUsed   int    `json:"tokens_used,omitempty"`
	ProcessingMs int64  `json:"processing_ms,omitempty"`

	ErrorMessage string `gorm:"type:text" json:"error_message,omitempty"`
	// Audit
	CreatedAt time.Time                     `json:"created_at"`
	UpdatedAt time.Time                     `json:"updated_at"`
	DeletedAt datatypes.JSONQueryExpression `gorm:"index" json:"deleted_at,omitempty"` // soft delete

	EmbeddingID string `gorm:"type:uuid" json:"embedding_id,omitempty"` // Link to code embedding if created
}

func (CodeAnalysis) TableName() string {
	return "code_analyses"
}

func (ca *CodeAnalysis) IsValid() bool {
	return ca.RepositoryID != "" && ca.Type != ""
}

func (ca *CodeAnalysis) ParseIssues() ([]CodeIssue, error) {
	val, err := ca.Issues.Value()
	if err != nil {
		return nil, err
	}

	bytes, _ := val.([]byte)
	var issues []CodeIssue
	err = json.Unmarshal(bytes, &issues)
	return issues, err
}

func (ca *CodeAnalysis) AddIssue(issue CodeIssue) error {
	issues, err := ca.ParseIssues()
	if err != nil {
		return err
	}

	issues = append(issues, issue)
	data, err := json.Marshal(issues)
	if err != nil {
		return err
	}

	if err := ca.Issues.Scan(data); err != nil {
		return err
	}

	ca.IssueCount++

	switch issue.Severity {
	case SeverityCritical:
		ca.CriticalCount++
	case SeverityError:
		ca.ErrorCount++
	case SeverityWarning:
		ca.WarningCount++
	case SeverityInfo:
		ca.InfoCount++
	}

	return nil
}

func (ca *CodeAnalysis) HasCriticalIssues() bool {
	return ca.CriticalCount > 0
}

func (ca *CodeAnalysis) GetQualityScore() int {
	if ca.IssueCount == 0 && ca.Metrics.TestCoverage >= 80 {
		return 100
	}

	score := 100

	// Deduct for issues (max 50 points)
	issueDeduction := int(float64(ca.CriticalCount)*5 + float64(ca.ErrorCount)*3 + float64(ca.WarningCount))
	if issueDeduction > 50 {
		issueDeduction = 50
	}
	score -= issueDeduction

	// Deduct for low test coverage (max 20 points)
	if ca.Metrics.TestCoverage < 80 {
		coverageDeduction := int((80.0 - ca.Metrics.TestCoverage) / 4)
		if coverageDeduction > 20 {
			coverageDeduction = 20
		}
		score -= coverageDeduction
	}

	// Deduct for high complexity (max 10 points)
	if ca.Metrics.AvgCyclomaticComplexity > 5 {
		complexityDeduction := int((ca.Metrics.AvgCyclomaticComplexity - 5) * 2)
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

type CodeEmbedding struct {
	ID           string      `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	RepositoryID string      `gorm:"type:uuid;index" json:"repository_id"`
	Repository   *Repository `gorm:"foreignKey:RepositoryID" json:"repository,omitempty"`

	// Content
	FilePath    string `gorm:"type:varchar(1000)" json:"file_path"`
	Content     string `gorm:"type:text" json:"content"`
	ContentHash string `gorm:"type:varchar(255);index" json:"content_hash"` // For deduplication

	Description string `gorm:"type:text" json:"description"`
	Language    string `gorm:"type:varchar(50)" json:"language"`
	StartLine   int    `json:"start_line,omitempty"`
	EndLine     int    `json:"end_line,omitempty"`

	Embedding []float32 `gorm:"type:vector(1536)"  json:"embedding,omitempty"` // 1536 for OpenAI embeddings

	Tokens    int       `json:"tokens,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (CodeEmbedding) TableName() string {
	return "code_embeddings"
}
