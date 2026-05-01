package workers

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/hibiken/asynq"
	"github.com/paulozy/idp-with-ai-backend/internal/ai"
	"github.com/paulozy/idp-with-ai-backend/internal/dependencies"
	anthropicclient "github.com/paulozy/idp-with-ai-backend/internal/integrations/anthropic"
	"github.com/paulozy/idp-with-ai-backend/internal/integrations/github"
	"github.com/paulozy/idp-with-ai-backend/internal/jobs/tasks"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
	"github.com/paulozy/idp-with-ai-backend/internal/utils"
	"gorm.io/datatypes"
)

const maxManifestBytes = 512 * 1024

type DependencyWorker struct {
	repo            storage.Repository
	analyzer        ai.Analyzer
	analyzerFactory func(apiKey string) ai.Analyzer
	githubClient    github.ClientInterface
}

type parsedManifest struct {
	Path     string
	Content  string
	Packages []dependencies.Package
}

func NewDependencyWorker(repo storage.Repository, analyzer ai.Analyzer, githubClient github.ClientInterface) *DependencyWorker {
	return &DependencyWorker{
		repo:     repo,
		analyzer: analyzer,
		analyzerFactory: func(apiKey string) ai.Analyzer {
			if analyzer != nil {
				return analyzer
			}
			return anthropicclient.NewClient(apiKey)
		},
		githubClient: githubClient,
	}
}

func (w *DependencyWorker) Handle(ctx context.Context, task *asynq.Task) error {
	var payload tasks.ScanDependenciesPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return fmt.Errorf("dependency worker: unmarshal payload: %w", err)
	}
	if payload.RepositoryID == "" {
		return fmt.Errorf("dependency worker: empty repository_id")
	}

	repository, err := w.repo.GetRepository(ctx, payload.RepositoryID)
	if err != nil {
		return fmt.Errorf("dependency worker: get repository: %w", err)
	}
	if repository == nil {
		return fmt.Errorf("dependency worker: repository not found: %s", payload.RepositoryID)
	}

	cfg, err := w.repo.GetOrganizationConfig(ctx, repository.OrganizationID)
	if err != nil {
		return fmt.Errorf("dependency worker: get organization config: %w", err)
	}
	if cfg == nil || cfg.AnthropicAPIKey == "" {
		return fmt.Errorf("dependency worker: anthropic api key is not configured for organization")
	}
	analyzer := w.analyzerFactory(cfg.AnthropicAPIKey)

	branch := payload.Branch
	if branch == "" {
		branch = repository.Metadata.DefaultBranch
	}
	if branch == "" {
		branch = "main"
	}
	triggeredBy := payload.TriggeredBy
	if triggeredBy == "" {
		triggeredBy = "webhook"
	}

	analysis := &models.CodeAnalysis{
		RepositoryID: repository.ID,
		Type:         models.AnalysisTypeDependency,
		Status:       models.AnalysisStatusProcessing,
		Title:        "Dependency analysis",
		CommitSHA:    payload.CommitSHA,
		Branch:       branch,
		TriggeredBy:  triggeredBy,
		IsAIAnalysis: true,
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	if payload.PullRequestID > 0 {
		prID := payload.PullRequestID
		analysis.PullRequestID = &prID
	}
	if err := w.repo.CreateCodeAnalysis(ctx, analysis); err != nil {
		return fmt.Errorf("dependency worker: create analysis: %w", err)
	}

	manifests, err := collectDependencyManifests(ctx, repository.URL, cfg.GithubToken, branch)
	if err != nil {
		return w.failDependencyAnalysis(ctx, analysis, err)
	}

	now := time.Now().UTC()
	depsByManifest := make(map[string][]*models.PackageDependency)
	for _, manifest := range manifests {
		for _, pkg := range manifest.Packages {
			dep := &models.PackageDependency{
				RepositoryID:       repository.ID,
				Name:               pkg.Name,
				CurrentVersion:     pkg.Version,
				Ecosystem:          pkg.Ecosystem,
				ManifestFile:       manifest.Path,
				IsDirectDependency: pkg.IsDirect,
				IsVulnerable:       false,
				VulnerabilityCVEs:  models.StringArray{},
				UpdateAvailable:    false,
				LastScannedAt:      now,
				CreatedAt:          now,
				UpdatedAt:          now,
			}
			if err := w.repo.UpsertPackageDependency(ctx, dep); err != nil {
				return w.failDependencyAnalysis(ctx, analysis, err)
			}
			depsByManifest[manifest.Path] = append(depsByManifest[manifest.Path], dep)
		}
	}

	if len(manifests) == 0 {
		analysis.Status = models.AnalysisStatusCompleted
		analysis.SummaryText = "No supported dependency manifests found."
		analysis.Issues = datatypes.NewJSONType([]models.CodeIssue{})
		analysis.UpdatedAt = time.Now().UTC()
		if err := w.repo.UpdateCodeAnalysis(ctx, analysis); err != nil {
			return fmt.Errorf("dependency worker: update empty analysis: %w", err)
		}
		return nil
	}

	req := buildDependencyAnalysisRequest(repository, payload, branch, manifests)
	req.OutputLanguage = cfg.ResolvedOutputLanguage()
	start := time.Now()
	result, err := analyzer.AnalyzeCode(ctx, req)
	processingMs := time.Since(start).Milliseconds()
	if err != nil {
		return w.failDependencyAnalysis(ctx, analysis, err)
	}

	issues := mapIssues(result.Issues)
	analysis.Status = models.AnalysisStatusCompleted
	analysis.SummaryText = result.Summary
	analysis.Issues = datatypes.NewJSONType(issues)
	analysis.IssueCount = len(issues)
	analysis.AIModel = result.Model
	analysis.TokensUsed = result.TokensUsed
	analysis.ProcessingMs = processingMs
	analysis.Metrics = models.CodeMetrics{
		TotalLines:           int(result.Metrics.LinesOfCode),
		CyclomaticComplexity: float64(result.Metrics.CyclomaticComplexity),
		TestCoverage:         float64(result.Metrics.TestCoverage),
	}
	for _, issue := range issues {
		switch issue.Severity {
		case models.SeverityCritical:
			analysis.CriticalCount++
		case models.SeverityError:
			analysis.ErrorCount++
		case models.SeverityWarning:
			analysis.WarningCount++
		case models.SeverityInfo:
			analysis.InfoCount++
		}
	}

	allDeps, err := w.repo.ListPackageDependencies(ctx, repository.ID, false)
	if err == nil {
		depsByManifest = groupDepsByManifest(allDeps)
	}
	for _, issue := range result.Issues {
		for _, dep := range matchIssueDependencies(issue, depsByManifest) {
			if dep.ID == "" {
				continue
			}
			cves := extractCVEs(issue)
			latestVersion := extractRecommendedVersion(issue.Suggestion)
			if err := w.repo.UpdatePackageDependencyVulnStatus(ctx, dep.ID, true, cves, latestVersion); err != nil {
				utils.Warn("dependency worker: failed to update dependency vulnerability", "repo_id", repository.ID, "dependency", dep.Name, "error", err)
			}
		}
	}

	if err := w.repo.UpdateCodeAnalysis(ctx, analysis); err != nil {
		return fmt.Errorf("dependency worker: update analysis: %w", err)
	}

	repository.LastAnalyzedAt = time.Now().UTC()
	_ = w.repo.UpdateRepository(ctx, repository)

	if payload.PullRequestID > 0 && cfg.GitHubPRReviewEnabled {
		w.postDependencyPRReview(ctx, repository, payload.PullRequestID, result.Issues)
	}

	utils.Info("dependency worker: completed", "repo_id", repository.ID, "manifests", len(manifests), "issues", len(result.Issues))
	return nil
}

func (w *DependencyWorker) failDependencyAnalysis(ctx context.Context, analysis *models.CodeAnalysis, cause error) error {
	analysis.Status = models.AnalysisStatusFailed
	analysis.ErrorMessage = cause.Error()
	analysis.UpdatedAt = time.Now().UTC()
	_ = w.repo.UpdateCodeAnalysis(ctx, analysis)
	return fmt.Errorf("dependency worker: %w", cause)
}

func collectDependencyManifests(ctx context.Context, repoURL, githubToken, branch string) ([]parsedManifest, error) {
	dir, err := os.MkdirTemp("", "dependencies-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(dir)

	opts := &git.CloneOptions{URL: repoURL, Depth: 1}
	if branch != "" {
		opts.ReferenceName = plumbing.NewBranchReferenceName(branch)
		opts.SingleBranch = true
	}
	if githubToken != "" {
		opts.Auth = &githttp.BasicAuth{Username: "x-token", Password: githubToken}
	}
	if _, err := git.PlainCloneContext(ctx, dir, false, opts); err != nil {
		return nil, fmt.Errorf("clone repo: %w", err)
	}

	var manifests []parsedManifest
	err = filepath.Walk(dir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			if info.Name() == ".git" || info.Name() == "node_modules" || info.Name() == "vendor" || info.Name() == "target" {
				return filepath.SkipDir
			}
			return nil
		}
		parser, ok := dependencies.ManifestFiles[info.Name()]
		if !ok || info.Size() <= 0 || info.Size() > maxManifestBytes {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return nil
		}
		content := string(raw)
		manifests = append(manifests, parsedManifest{
			Path:     filepath.ToSlash(rel),
			Content:  content,
			Packages: parser(content),
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk repository: %w", err)
	}
	return manifests, nil
}

func buildDependencyAnalysisRequest(repository *models.Repository, payload tasks.ScanDependenciesPayload, branch string, manifests []parsedManifest) *ai.AnalysisRequest {
	req := &ai.AnalysisRequest{
		RepositoryID:  repository.ID,
		RepoName:      repository.Name,
		Branch:        branch,
		CommitSHA:     payload.CommitSHA,
		AnalysisType:  ai.AnalysisTypeDependency,
		DefaultBranch: repository.Metadata.DefaultBranch,
		HasCI:         repository.Metadata.HasCI,
		HasTests:      repository.Metadata.HasTests,
		Metrics:       &ai.CodeMetrics{},
	}
	for lang := range repository.Metadata.Languages {
		req.Languages = append(req.Languages, lang)
	}
	req.Topics = repository.Metadata.Topics
	if repository.Metadata.TestCoverage != nil {
		req.TestCoverage = float32(*repository.Metadata.TestCoverage)
	}
	if payload.PullRequestID > 0 {
		req.PullRequestID = int64(payload.PullRequestID)
	}
	for _, manifest := range manifests {
		req.ChangedFiles = append(req.ChangedFiles, ai.ChangedFile{
			Path:   manifest.Path,
			Patch:  manifest.Content,
			Status: "modified",
		})
	}
	return req
}

func groupDepsByManifest(deps []*models.PackageDependency) map[string][]*models.PackageDependency {
	out := make(map[string][]*models.PackageDependency)
	for _, dep := range deps {
		out[dep.ManifestFile] = append(out[dep.ManifestFile], dep)
	}
	return out
}

func matchIssueDependencies(issue ai.CodeIssue, depsByManifest map[string][]*models.PackageDependency) []*models.PackageDependency {
	candidates := depsByManifest[issue.FilePath]
	if len(candidates) == 0 {
		base := filepath.Base(issue.FilePath)
		for path, deps := range depsByManifest {
			if filepath.Base(path) == base {
				candidates = append(candidates, deps...)
			}
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	haystack := strings.ToLower(issue.Title + " " + issue.Description + " " + issue.Suggestion)
	var matches []*models.PackageDependency
	for _, dep := range candidates {
		if strings.Contains(haystack, strings.ToLower(dep.Name)) {
			matches = append(matches, dep)
		}
	}
	if len(matches) > 0 {
		return matches
	}
	return candidates
}

var cvePattern = regexp.MustCompile(`CVE-\d{4}-\d{4,}`)

func extractCVEs(issue ai.CodeIssue) []string {
	seen := make(map[string]bool)
	var cves []string
	for _, text := range []string{issue.CWEID, issue.Title, issue.Description, issue.Suggestion} {
		for _, match := range cvePattern.FindAllString(strings.ToUpper(text), -1) {
			if !seen[match] {
				seen[match] = true
				cves = append(cves, match)
			}
		}
	}
	return cves
}

func extractRecommendedVersion(suggestion string) string {
	lower := strings.ToLower(suggestion)
	if idx := strings.LastIndex(lower, "(recommended:"); idx >= 0 {
		value := suggestion[idx+len("(recommended:"):]
		value = strings.TrimSpace(strings.TrimSuffix(value, ")"))
		return value
	}
	if idx := strings.Index(lower, "update to "); idx >= 0 {
		value := strings.Fields(suggestion[idx+len("update to "):])
		if len(value) > 0 {
			return strings.Trim(value[0], ".,);")
		}
	}
	return ""
}

func (w *DependencyWorker) postDependencyPRReview(ctx context.Context, repository *models.Repository, prID int, issues []ai.CodeIssue) {
	if w.githubClient == nil || len(issues) == 0 {
		return
	}
	ownerRepo, _, err := utils.ParseRepositoryURL(repository.URL)
	if err != nil {
		utils.Warn("dependency worker: parse repo url for PR review failed", "repo_id", repository.ID, "error", err)
		return
	}
	parts := strings.Split(ownerRepo, "/")
	if len(parts) != 2 {
		return
	}
	comments := make([]github.ReviewCommentInput, 0, len(issues))
	for _, issue := range issues {
		if issue.FilePath == "" || issue.Line <= 0 {
			continue
		}
		comments = append(comments, github.ReviewCommentInput{
			Path:     issue.FilePath,
			Position: issue.Line,
			Body:     fmt.Sprintf("%s\n\n%s\n\n%s", issue.Title, issue.Description, issue.Suggestion),
		})
	}
	if len(comments) == 0 {
		return
	}
	if _, err := w.githubClient.CreatePullRequestReview(ctx, parts[0], parts[1], int64(prID), "Dependency analysis found issues.", "COMMENT", comments); err != nil {
		utils.Warn("dependency worker: create PR review failed", "repo_id", repository.ID, "pr_id", prID, "error", err)
	}
}
