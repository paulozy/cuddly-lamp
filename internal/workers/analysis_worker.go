package workers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hibiken/asynq"
	"github.com/paulozy/idp-with-ai-backend/internal/ai"
	"github.com/paulozy/idp-with-ai-backend/internal/coverage"
	anthropicclient "github.com/paulozy/idp-with-ai-backend/internal/integrations/anthropic"
	"github.com/paulozy/idp-with-ai-backend/internal/integrations/github"
	"github.com/paulozy/idp-with-ai-backend/internal/jobs/tasks"
	"github.com/paulozy/idp-with-ai-backend/internal/metrics"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
	"github.com/paulozy/idp-with-ai-backend/internal/utils"
	"gorm.io/datatypes"
)

type AnalysisWorker struct {
	repo             storage.Repository
	analyzerFactory  func(apiKey string) ai.Analyzer
	githubFactory    func(token string) github.ClientInterface
	calculateMetrics func(ctx context.Context, repoURL, githubToken, branch string) (*ai.CodeMetrics, error)
	// lookupCoverage returns the latest coverage upload for (repoID, sha) or
	// (nil, nil) when none exists. Injectable for tests.
	lookupCoverage func(ctx context.Context, repoID, sha string) (*models.CoverageUpload, error)
}

func NewAnalysisWorker(repo storage.Repository) *AnalysisWorker {
	w := &AnalysisWorker{
		repo:             repo,
		analyzerFactory:  func(apiKey string) ai.Analyzer { return anthropicclient.NewClient(apiKey) },
		githubFactory:    func(token string) github.ClientInterface { return github.NewClient(token) },
		calculateMetrics: metrics.Calculate,
	}
	// Wrap GetLatestCoverageUpload in a closure so the receiver is resolved at
	// call time, not at construction time. This lets tests pass nil/mocks
	// without panicking before any method is invoked.
	w.lookupCoverage = func(ctx context.Context, repoID, sha string) (*models.CoverageUpload, error) {
		if w.repo == nil {
			return nil, nil
		}
		return w.repo.GetLatestCoverageUpload(ctx, repoID, sha)
	}
	return w
}

// normalizeSeverity maps ai.CodeIssue.Severity ("critical", "high", "medium", "low", "info")
// to models.SeverityLevel ("critical", "error", "warning", "info")
func normalizeSeverity(s string) models.SeverityLevel {
	switch s {
	case "critical":
		return models.SeverityCritical
	case "high":
		return models.SeverityError // high → error
	case "medium":
		return models.SeverityWarning // medium → warning
	case "low", "info":
		return models.SeverityInfo
	default:
		return models.SeverityInfo
	}
}

// mapIssues converts ai.CodeIssue to models.CodeIssue
func mapIssues(aiIssues []ai.CodeIssue) []models.CodeIssue {
	out := make([]models.CodeIssue, 0, len(aiIssues))
	for _, issue := range aiIssues {
		out = append(out, models.CodeIssue{
			File:          issue.FilePath,
			Line:          issue.Line,
			Column:        issue.Column,
			Severity:      normalizeSeverity(issue.Severity),
			Category:      issue.Category,
			Title:         issue.Title,
			Description:   issue.Description,
			Suggestion:    issue.Suggestion,
			IsAIGenerated: issue.IsAIGenerated,
			Confidence:    float64(issue.Confidence),
		})
	}
	return out
}

func (w *AnalysisWorker) Handle(ctx context.Context, task *asynq.Task) error {
	var payload tasks.AnalyzeRepoPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return fmt.Errorf("analysis worker: unmarshal payload: %w", err)
	}

	if payload.RepositoryID == "" {
		return fmt.Errorf("analysis worker: empty repository_id")
	}

	utils.Info("analysis worker: processing", "repo_id", payload.RepositoryID, "pr_id", payload.PullRequestID)

	// Fetch repository
	repository, err := w.repo.GetRepository(ctx, payload.RepositoryID)
	if err != nil {
		utils.Error("analysis worker: fetch repo failed", "repo_id", payload.RepositoryID, "error", err)
		return err
	}

	if repository == nil {
		return fmt.Errorf("analysis worker: repository not found: %s", payload.RepositoryID)
	}
	cfg, err := w.repo.GetOrganizationConfig(ctx, repository.OrganizationID)
	if err != nil {
		return fmt.Errorf("analysis worker: get organization config: %w", err)
	}
	if cfg == nil || cfg.AnthropicAPIKey == "" {
		return w.failAnalysis(ctx, repository, payload, "anthropic api key is not configured for organization")
	}
	analyzer := w.analyzerFactory(cfg.AnthropicAPIKey)
	githubToken := cfg.GithubToken
	ghClient := w.githubFactory(githubToken)

	// Update status
	repository.AnalysisStatus = "in_progress"
	if err := w.repo.UpdateRepository(ctx, repository); err != nil {
		utils.Error("analysis worker: update status failed", "repo_id", payload.RepositoryID, "error", err)
		return err
	}

	// Parse repository URL to extract owner/repo (use same logic as SyncService)
	ownerRepo, _, err := utils.ParseRepositoryURL(repository.URL)
	if err != nil {
		return w.failAnalysis(ctx, repository, payload, fmt.Sprintf("invalid repository URL: %v", err))
	}
	parts := strings.Split(ownerRepo, "/")
	if len(parts) != 2 {
		return w.failAnalysis(ctx, repository, payload, "invalid repository format")
	}
	owner, repo := parts[0], parts[1]

	branch := payload.Branch
	if branch == "" {
		branch = repository.Metadata.DefaultBranch
	}
	if branch == "" {
		branch = "main"
	}

	repoMetrics := &ai.CodeMetrics{}
	if payload.PullRequestID == 0 {
		// Calculate code metrics locally (lines of code, complexity).
		// Test coverage is sourced separately via the CI upload endpoint
		// (POST /repositories/:id/coverage); the analysis worker does not
		// read it from the clone.
		repoMetrics, err = w.calculateMetrics(ctx, repository.URL, githubToken, branch)
		if err != nil {
			utils.Warn("analysis worker: calculate metrics failed", "repo_id", payload.RepositoryID, "branch", branch, "auth_configured", githubToken != "", "error", err)
			repoMetrics = &ai.CodeMetrics{}
		}
	}

	// Build analysis request with computed metrics
	analysisReq, prFiles, err := w.buildAnalysisRequest(ctx, ghClient, repository, payload, owner, repo, repoMetrics)
	if err != nil {
		return w.failAnalysis(ctx, repository, payload, fmt.Sprintf("failed to build analysis request: %v", err))
	}
	analysisReq.OutputLanguage = cfg.ResolvedOutputLanguage()

	// Call analyzer
	startTime := time.Now()
	analysisResult, err := analyzer.AnalyzeCode(ctx, analysisReq)
	processingMs := time.Since(startTime).Milliseconds()

	if err != nil {
		utils.Error("analysis worker: analyzer failed", "repo_id", payload.RepositoryID, "error", err)
		return w.failAnalysis(ctx, repository, payload, fmt.Sprintf("analyzer error: %v", err))
	}

	// Save analysis result (ID auto-generated by database)
	triggeredBy := payload.TriggeredBy
	if triggeredBy == "" {
		triggeredBy = "webhook" // backward compatibility
	}
	codeAnalysisType := models.AnalysisType(payload.Type)
	if codeAnalysisType == "" {
		codeAnalysisType = models.AnalysisTypeCodeReview
	}
	codeAnalysis := &models.CodeAnalysis{
		RepositoryID: repository.ID,
		Type:         codeAnalysisType,
		Status:       "completed",
		CommitSHA:    payload.CommitSHA,
		Branch:       payload.Branch,
		SummaryText:  analysisResult.Summary,
		IsAIAnalysis: true,
		AIModel:      analysisResult.Model,
		TokensUsed:   analysisResult.TokensUsed,
		ProcessingMs: processingMs,
		TriggeredBy:  triggeredBy,
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}

	// Handle PR-specific fields
	if payload.PullRequestID > 0 {
		prID := int(payload.PullRequestID)
		codeAnalysis.PullRequestID = &prID
	}

	// Look up the latest coverage upload for this commit. Missing uploads are
	// the common case for repos that haven't wired the CI integration yet —
	// we degrade gracefully (no error, no issue, no metrics).
	covUpload, err := w.lookupCoverage(ctx, repository.ID, payload.CommitSHA)
	if err != nil {
		utils.Warn("analysis worker: coverage lookup failed", "repo_id", repository.ID, "sha", payload.CommitSHA, "error", err)
		covUpload = nil
	}

	// Append coverage-driven issues for PR analyses. The rule is deterministic
	// (no LLM), so the cost is negligible.
	aiIssues := analysisResult.Issues
	if payload.PullRequestID > 0 && covUpload != nil && len(prFiles) > 0 {
		fileMap := covUpload.Files.Data()
		gaps := coverage.PRCoverageGaps(prFiles, fileMap)
		if len(gaps) > 0 {
			aiIssues = append(aiIssues, gaps...)
		}
	}

	// Convert and store issues (datatypes.JSONType requires proper type conversion)
	convertedIssues := mapIssues(aiIssues)
	codeAnalysis.Issues = datatypes.NewJSONType(convertedIssues)

	// Count issues by severity (must run AFTER any deterministic rule appends)
	codeAnalysis.IssueCount = len(convertedIssues)
	for _, issue := range convertedIssues {
		switch issue.Severity {
		case models.SeverityCritical:
			codeAnalysis.CriticalCount++
		case models.SeverityError:
			codeAnalysis.ErrorCount++
		case models.SeverityWarning:
			codeAnalysis.WarningCount++
		case models.SeverityInfo:
			codeAnalysis.InfoCount++
		}
	}

	// Store metrics. Coverage comes from the CI upload table (authoritative);
	// missing uploads leave the fields zero and CoverageStatus empty, which
	// the quality-score guard interprets as "not measured" (no penalty).
	codeAnalysis.Metrics = models.CodeMetrics{
		TotalLines:           int(analysisResult.Metrics.LinesOfCode),
		CyclomaticComplexity: float64(analysisResult.Metrics.CyclomaticComplexity),
	}
	if covUpload != nil {
		codeAnalysis.Metrics.TestCoverage = covUpload.Percentage
		codeAnalysis.Metrics.TestedLines = covUpload.LinesCovered
		uncovered := covUpload.LinesTotal - covUpload.LinesCovered
		if uncovered < 0 {
			uncovered = 0
		}
		codeAnalysis.Metrics.UncoveredLines = uncovered
		codeAnalysis.Metrics.CoverageStatus = string(covUpload.Status)
	}

	if err := w.repo.CreateCodeAnalysis(ctx, codeAnalysis); err != nil {
		utils.Error("analysis worker: create analysis failed", "repo_id", payload.RepositoryID, "error", err)
		return w.failAnalysis(ctx, repository, payload, "failed to save analysis")
	}

	// Update repository status
	repository.AnalysisStatus = "completed"
	repository.LastAnalyzedAt = time.Now().UTC()
	if err := w.repo.UpdateRepository(ctx, repository); err != nil {
		utils.Error("analysis worker: final update failed", "repo_id", payload.RepositoryID, "error", err)
		// Don't fail the task, analysis was already saved
	}

	utils.Info("analysis worker: completed", "repo_id", payload.RepositoryID, "issues", len(analysisResult.Issues))
	return nil
}

func (w *AnalysisWorker) buildAnalysisRequest(ctx context.Context, ghClient github.ClientInterface, repository *models.Repository, payload tasks.AnalyzeRepoPayload, owner, repoName string, repoMetrics *ai.CodeMetrics) (*ai.AnalysisRequest, []github.PRFile, error) {
	analysisType := ai.AnalysisType(payload.Type)
	if analysisType == "" {
		analysisType = ai.AnalysisTypeCodeReview
	}

	req := &ai.AnalysisRequest{
		RepositoryID: repository.ID,
		RepoName:     repository.Name,
		Branch:       payload.Branch,
		CommitSHA:    payload.CommitSHA,
		AnalysisType: analysisType,
		Metrics:      repoMetrics, // computed metrics passed to Claude
	}

	// Extract metadata from repository
	metadata := repository.Metadata
	// Convert language map keys to slice
	for lang := range metadata.Languages {
		req.Languages = append(req.Languages, lang)
	}
	req.Topics = metadata.Topics
	req.HasCI = metadata.HasCI
	req.HasTests = metadata.HasTests
	if metadata.TestCoverage != nil {
		req.TestCoverage = float32(*metadata.TestCoverage)
	}
	req.DefaultBranch = metadata.DefaultBranch

	if docs, err := w.repo.GetLatestDocGenerationForRepo(ctx, repository.ID); err == nil && docs != nil {
		req.ProjectStandards = renderDocSummary(docs)
	} else if err != nil {
		utils.Warn("analysis worker: fetch generated docs failed", "repo_id", repository.ID, "error", err)
	}

	// Fallback to provided branch/commit
	if req.Branch == "" {
		req.Branch = req.DefaultBranch
		if req.Branch == "" {
			req.Branch = "main"
		}
	}

	if payload.PullRequestID > 0 {
		pr, err := ghClient.GetPullRequest(ctx, owner, repoName, payload.PullRequestID)
		if err != nil {
			return nil, nil, fmt.Errorf("fetch PR details: %w", err)
		}
		req.PullRequestID = payload.PullRequestID
		req.PRTitle = pr.Title
		req.PRBody = pr.Body
		req.PRAuthor = pr.User.Login

		files, err := ghClient.GetPullRequestFiles(ctx, owner, repoName, payload.PullRequestID)
		if err != nil {
			return nil, nil, fmt.Errorf("fetch PR files: %w", err)
		}
		changed := ai.FilterAndMapPRFiles(files)
		req.ChangedFiles, req.TruncatedFiles = ai.BudgetChangedFiles(changed)

		return req, files, nil
	}

	// Fetch recent commits for push/branch analysis context.
	commits, err := ghClient.GetCommits(ctx, owner, repoName, req.Branch, 10)
	if err != nil {
		utils.Warn("analysis worker: fetch commits failed", "error", err)
		// Don't fail, continue with empty commits.
	}

	for _, commit := range commits {
		req.RecentCommits = append(req.RecentCommits, ai.CommitSummary{
			SHA:     commit.SHA,
			Message: commit.Commit.Message,
			Author:  commit.Commit.Author.Name,
			Date:    commit.Commit.Author.Date.Format(time.RFC3339),
		})
	}

	return req, nil, nil
}

func renderDocSummary(doc *models.DocGeneration) string {
	if doc == nil {
		return ""
	}
	content := doc.Content.Data()
	if len(content) == 0 {
		return ""
	}

	orderedTypes := []string{
		string(ai.DocumentationTypeGuidelines),
		string(ai.DocumentationTypeADR),
		string(ai.DocumentationTypeArchitecture),
		string(ai.DocumentationTypeServiceDoc),
	}
	var sb strings.Builder
	for _, docType := range orderedTypes {
		markdown := strings.TrimSpace(content[docType])
		if markdown == "" {
			continue
		}
		sb.WriteString(fmt.Sprintf("## %s\n", docType))
		limit := 10000
		if docType == string(ai.DocumentationTypeArchitecture) || docType == string(ai.DocumentationTypeServiceDoc) {
			limit = 4000
		}
		sb.WriteString(truncateString(markdown, limit))
		sb.WriteString("\n\n")
	}
	return truncateString(strings.TrimSpace(sb.String()), 24000)
}

func (w *AnalysisWorker) failAnalysis(ctx context.Context, repository *models.Repository, payload tasks.AnalyzeRepoPayload, errorMsg string) error {
	utils.Error("analysis worker: failing analysis", "repo_id", repository.ID, "error", errorMsg)

	triggeredBy := payload.TriggeredBy
	if triggeredBy == "" {
		triggeredBy = "webhook" // backward compatibility
	}
	codeAnalysisType := models.AnalysisType(payload.Type)
	if codeAnalysisType == "" {
		codeAnalysisType = models.AnalysisTypeCodeReview
	}

	// Create failed analysis record (ID auto-generated by database)
	codeAnalysis := &models.CodeAnalysis{
		RepositoryID: repository.ID,
		Type:         codeAnalysisType,
		Status:       "failed",
		CommitSHA:    payload.CommitSHA,
		Branch:       payload.Branch,
		ErrorMessage: errorMsg,
		TriggeredBy:  triggeredBy,
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	if payload.PullRequestID > 0 {
		prID := int(payload.PullRequestID)
		codeAnalysis.PullRequestID = &prID
	}

	if err := w.repo.CreateCodeAnalysis(ctx, codeAnalysis); err != nil {
		utils.Error("analysis worker: create failed analysis record", "error", err)
	}

	// Update repository status
	repository.AnalysisStatus = "failed"
	if err := w.repo.UpdateRepository(ctx, repository); err != nil {
		utils.Error("analysis worker: update failed status", "error", err)
	}

	return errors.New(errorMsg)
}

// Note: UUID generation is handled by PostgreSQL (gen_random_uuid())
