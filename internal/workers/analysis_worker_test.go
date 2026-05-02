package workers

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/hibiken/asynq"
	"github.com/paulozy/idp-with-ai-backend/internal/ai"
	"github.com/paulozy/idp-with-ai-backend/internal/integrations/github"
	"github.com/paulozy/idp-with-ai-backend/internal/jobs/tasks"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
	"gorm.io/datatypes"
)

type mockRepository struct {
	storage.Repository
	getRepoFunc                           func(ctx context.Context, id string) (*models.Repository, error)
	updateRepoFunc                        func(ctx context.Context, repo *models.Repository) error
	createAnalysisFunc                    func(ctx context.Context, analysis *models.CodeAnalysis) error
	updateAnalysisFunc                    func(ctx context.Context, analysis *models.CodeAnalysis) error
	createDocGenerationFunc               func(ctx context.Context, doc *models.DocGeneration) error
	updateDocGenerationFunc               func(ctx context.Context, doc *models.DocGeneration) error
	getDocGenerationFunc                  func(ctx context.Context, id string) (*models.DocGeneration, error)
	getLatestDocGenerationFunc            func(ctx context.Context, repoID string) (*models.DocGeneration, error)
	getLatestAnalysisFunc                 func(ctx context.Context, repoID string, analysisType models.AnalysisType) (*models.CodeAnalysis, error)
	getConfigFunc                         func(ctx context.Context, orgID string) (*models.OrganizationConfig, error)
	getLatestCoverageUploadFunc           func(ctx context.Context, repoID, sha string) (*models.CoverageUpload, error)
	upsertPackageDependencyFunc           func(ctx context.Context, dep *models.PackageDependency) error
	listPackageDependenciesFunc           func(ctx context.Context, repoID string, onlyVulnerable bool) ([]*models.PackageDependency, error)
	updatePackageDependencyVulnStatusFunc func(ctx context.Context, id string, isVulnerable bool, cves []string, latestVersion string) error
}

func (m *mockRepository) GetRepository(ctx context.Context, id string) (*models.Repository, error) {
	if m.getRepoFunc != nil {
		return m.getRepoFunc(ctx, id)
	}
	return nil, nil
}

func (m *mockRepository) UpdateRepository(ctx context.Context, repo *models.Repository) error {
	if m.updateRepoFunc != nil {
		return m.updateRepoFunc(ctx, repo)
	}
	return nil
}

func (m *mockRepository) CreateCodeAnalysis(ctx context.Context, analysis *models.CodeAnalysis) error {
	if m.createAnalysisFunc != nil {
		return m.createAnalysisFunc(ctx, analysis)
	}
	return nil
}

func (m *mockRepository) UpdateCodeAnalysis(ctx context.Context, analysis *models.CodeAnalysis) error {
	if m.updateAnalysisFunc != nil {
		return m.updateAnalysisFunc(ctx, analysis)
	}
	return nil
}

func (m *mockRepository) CreateDocGeneration(ctx context.Context, doc *models.DocGeneration) error {
	if m.createDocGenerationFunc != nil {
		return m.createDocGenerationFunc(ctx, doc)
	}
	return nil
}

func (m *mockRepository) UpdateDocGeneration(ctx context.Context, doc *models.DocGeneration) error {
	if m.updateDocGenerationFunc != nil {
		return m.updateDocGenerationFunc(ctx, doc)
	}
	return nil
}

func (m *mockRepository) GetDocGeneration(ctx context.Context, id string) (*models.DocGeneration, error) {
	if m.getDocGenerationFunc != nil {
		return m.getDocGenerationFunc(ctx, id)
	}
	return nil, nil
}

func (m *mockRepository) GetLatestDocGenerationForRepo(ctx context.Context, repoID string) (*models.DocGeneration, error) {
	if m.getLatestDocGenerationFunc != nil {
		return m.getLatestDocGenerationFunc(ctx, repoID)
	}
	return nil, nil
}

func (m *mockRepository) GetLatestAnalysis(ctx context.Context, repoID string, analysisType models.AnalysisType) (*models.CodeAnalysis, error) {
	if m.getLatestAnalysisFunc != nil {
		return m.getLatestAnalysisFunc(ctx, repoID, analysisType)
	}
	return nil, nil
}

func (m *mockRepository) GetOrganizationConfig(ctx context.Context, orgID string) (*models.OrganizationConfig, error) {
	if m.getConfigFunc != nil {
		return m.getConfigFunc(ctx, orgID)
	}
	return nil, nil
}

func (m *mockRepository) GetLatestCoverageUpload(ctx context.Context, repoID, sha string) (*models.CoverageUpload, error) {
	if m.getLatestCoverageUploadFunc != nil {
		return m.getLatestCoverageUploadFunc(ctx, repoID, sha)
	}
	return nil, nil
}

func (m *mockRepository) UpsertPackageDependency(ctx context.Context, dep *models.PackageDependency) error {
	if m.upsertPackageDependencyFunc != nil {
		return m.upsertPackageDependencyFunc(ctx, dep)
	}
	return nil
}

func (m *mockRepository) ListPackageDependencies(ctx context.Context, repoID string, onlyVulnerable bool) ([]*models.PackageDependency, error) {
	if m.listPackageDependenciesFunc != nil {
		return m.listPackageDependenciesFunc(ctx, repoID, onlyVulnerable)
	}
	return nil, nil
}

func (m *mockRepository) UpdatePackageDependencyVulnStatus(ctx context.Context, id string, isVulnerable bool, cves []string, latestVersion string) error {
	if m.updatePackageDependencyVulnStatusFunc != nil {
		return m.updatePackageDependencyVulnStatusFunc(ctx, id, isVulnerable, cves, latestVersion)
	}
	return nil
}

type mockGithubClient struct {
	github.ClientInterface
	getCommitsFunc          func(ctx context.Context, owner, repo, branch string, limit int) ([]github.Commit, error)
	getPullRequestFunc      func(ctx context.Context, owner, repo string, prID int64) (*github.PullRequest, error)
	getPullRequestFilesFunc func(ctx context.Context, owner, repo string, prID int64) ([]github.PRFile, error)
}

func (m *mockGithubClient) GetCommits(ctx context.Context, owner, repo, branch string, limit int) ([]github.Commit, error) {
	if m.getCommitsFunc != nil {
		return m.getCommitsFunc(ctx, owner, repo, branch, limit)
	}
	return []github.Commit{}, nil
}

func (m *mockGithubClient) GetPullRequest(ctx context.Context, owner, repo string, prID int64) (*github.PullRequest, error) {
	if m.getPullRequestFunc != nil {
		return m.getPullRequestFunc(ctx, owner, repo, prID)
	}
	return nil, nil
}

func (m *mockGithubClient) GetPullRequestFiles(ctx context.Context, owner, repo string, prID int64) ([]github.PRFile, error) {
	if m.getPullRequestFilesFunc != nil {
		return m.getPullRequestFilesFunc(ctx, owner, repo, prID)
	}
	return []github.PRFile{}, nil
}

func TestAnalysisWorker_Handle(t *testing.T) {
	mockRepo := &mockRepository{
		getRepoFunc: func(ctx context.Context, id string) (*models.Repository, error) {
			return &models.Repository{
				ID:             id,
				OrganizationID: "org-1",
				Name:           "test-repo",
				URL:            "https://github.com/owner/repo",
				Metadata: models.RepositoryMetadata{
					DefaultBranch: "develop",
				},
			}, nil
		},
		updateRepoFunc: func(ctx context.Context, repo *models.Repository) error {
			return nil
		},
		createAnalysisFunc: func(ctx context.Context, analysis *models.CodeAnalysis) error {
			return nil
		},
		getConfigFunc: func(ctx context.Context, orgID string) (*models.OrganizationConfig, error) {
			return &models.OrganizationConfig{
				OrganizationID:  orgID,
				AnthropicAPIKey: "anthropic-key",
				GithubToken:     "github-token",
			}, nil
		},
	}

	mockGH := &mockGithubClient{
		getCommitsFunc: func(ctx context.Context, owner, repo, branch string, limit int) ([]github.Commit, error) {
			if branch != "main" {
				t.Fatalf("GetCommits branch = %q, want main", branch)
			}
			return []github.Commit{}, nil
		},
	}

	mockAnalyzer := &ai.MockAnalyzer{
		AnalyzeCodeFunc: func(ctx context.Context, req *ai.AnalysisRequest) (*ai.AnalysisResult, error) {
			if req.Branch != "main" {
				t.Fatalf("AnalysisRequest branch = %q, want main", req.Branch)
			}
			if req.Metrics == nil || req.Metrics.LinesOfCode != 42 {
				t.Fatalf("AnalysisRequest metrics = %+v, want calculated metrics", req.Metrics)
			}
			return &ai.AnalysisResult{
				Summary:    "Good code",
				Issues:     []ai.CodeIssue{},
				Model:      "test",
				TokensUsed: 100,
				Metrics: ai.CodeMetrics{
					LinesOfCode: 42,
				},
			}, nil
		},
	}

	worker := NewAnalysisWorker(mockRepo)
	worker.analyzerFactory = func(apiKey string) ai.Analyzer {
		if apiKey != "anthropic-key" {
			t.Fatalf("analyzer apiKey = %q, want configured key", apiKey)
		}
		return mockAnalyzer
	}
	worker.githubFactory = func(token string) github.ClientInterface {
		if token != "github-token" {
			t.Fatalf("github token = %q, want configured token", token)
		}
		return mockGH
	}
	worker.calculateMetrics = func(ctx context.Context, repoURL, githubToken, branch string) (*ai.CodeMetrics, error) {
		if repoURL != "https://github.com/owner/repo" {
			t.Fatalf("metrics repoURL = %q, want repository URL", repoURL)
		}
		if githubToken != "github-token" {
			t.Fatalf("metrics githubToken = %q, want configured token", githubToken)
		}
		if branch != "main" {
			t.Fatalf("metrics branch = %q, want payload branch", branch)
		}
		return &ai.CodeMetrics{LinesOfCode: 42}, nil
	}

	payload := tasks.AnalyzeRepoPayload{
		RepositoryID: "repo-1",
		Branch:       "main",
		CommitSHA:    "abc123",
		Type:         "code_review",
	}

	data, _ := json.Marshal(payload)
	task := asynq.NewTask(tasks.TypeAnalyzeRepo, data)

	err := worker.Handle(context.Background(), task)
	if err != nil {
		t.Fatalf("Handle failed: %v", err)
	}
}

func TestAnalysisWorker_Handle_PopulatesMetricsFromCoverageUpload(t *testing.T) {
	var saved *models.CodeAnalysis
	mockRepo := &mockRepository{
		getRepoFunc: func(ctx context.Context, id string) (*models.Repository, error) {
			return &models.Repository{
				ID:             id,
				OrganizationID: "org-1",
				Name:           "test-repo",
				URL:            "https://github.com/owner/repo",
				Metadata:       models.RepositoryMetadata{DefaultBranch: "main"},
			}, nil
		},
		updateRepoFunc: func(ctx context.Context, repo *models.Repository) error { return nil },
		createAnalysisFunc: func(ctx context.Context, analysis *models.CodeAnalysis) error {
			cp := *analysis
			saved = &cp
			return nil
		},
		getConfigFunc: func(ctx context.Context, orgID string) (*models.OrganizationConfig, error) {
			return &models.OrganizationConfig{OrganizationID: orgID, AnthropicAPIKey: "k", GithubToken: "g"}, nil
		},
		getLatestCoverageUploadFunc: func(ctx context.Context, repoID, sha string) (*models.CoverageUpload, error) {
			if repoID != "repo-1" || sha != "abc123" {
				t.Fatalf("lookup args = %q/%q", repoID, sha)
			}
			return &models.CoverageUpload{
				RepositoryID: repoID,
				CommitSHA:    sha,
				LinesCovered: 30,
				LinesTotal:   40,
				Percentage:   75.0,
				Status:       "ok",
			}, nil
		},
	}

	worker := NewAnalysisWorker(mockRepo)
	worker.analyzerFactory = func(string) ai.Analyzer {
		return &ai.MockAnalyzer{
			AnalyzeCodeFunc: func(ctx context.Context, req *ai.AnalysisRequest) (*ai.AnalysisResult, error) {
				return &ai.AnalysisResult{Summary: "ok", Issues: []ai.CodeIssue{}, Model: "test", TokensUsed: 50}, nil
			},
		}
	}
	worker.githubFactory = func(string) github.ClientInterface {
		return &mockGithubClient{
			getCommitsFunc: func(ctx context.Context, _, _, _ string, _ int) ([]github.Commit, error) {
				return nil, nil
			},
		}
	}
	worker.calculateMetrics = func(context.Context, string, string, string) (*ai.CodeMetrics, error) {
		return &ai.CodeMetrics{LinesOfCode: 1}, nil
	}

	payload := tasks.AnalyzeRepoPayload{RepositoryID: "repo-1", Branch: "main", CommitSHA: "abc123", Type: "code_review"}
	data, _ := json.Marshal(payload)
	task := asynq.NewTask(tasks.TypeAnalyzeRepo, data)

	if err := worker.Handle(context.Background(), task); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if saved == nil {
		t.Fatal("expected saved analysis")
	}
	if saved.Metrics.TestCoverage != 75.0 || saved.Metrics.TestedLines != 30 || saved.Metrics.UncoveredLines != 10 {
		t.Fatalf("metrics = %+v, want covered=75, tested=30, uncovered=10", saved.Metrics)
	}
	if saved.Metrics.CoverageStatus != "ok" {
		t.Fatalf("CoverageStatus = %q, want ok", saved.Metrics.CoverageStatus)
	}
}

func TestAnalysisWorker_Handle_NoCoverageUpload_LeavesMetricsUnmeasured(t *testing.T) {
	var saved *models.CodeAnalysis
	mockRepo := &mockRepository{
		getRepoFunc: func(ctx context.Context, id string) (*models.Repository, error) {
			return &models.Repository{
				ID: id, OrganizationID: "org-1", Name: "r",
				URL: "https://github.com/owner/repo", Metadata: models.RepositoryMetadata{DefaultBranch: "main"},
			}, nil
		},
		updateRepoFunc:     func(context.Context, *models.Repository) error { return nil },
		createAnalysisFunc: func(_ context.Context, a *models.CodeAnalysis) error { cp := *a; saved = &cp; return nil },
		getConfigFunc: func(context.Context, string) (*models.OrganizationConfig, error) {
			return &models.OrganizationConfig{OrganizationID: "org-1", AnthropicAPIKey: "k", GithubToken: "g"}, nil
		},
		// getLatestCoverageUploadFunc nil → default returns (nil, nil)
	}

	worker := NewAnalysisWorker(mockRepo)
	worker.analyzerFactory = func(string) ai.Analyzer {
		return &ai.MockAnalyzer{
			AnalyzeCodeFunc: func(context.Context, *ai.AnalysisRequest) (*ai.AnalysisResult, error) {
				return &ai.AnalysisResult{Summary: "ok", Issues: []ai.CodeIssue{}, Model: "t"}, nil
			},
		}
	}
	worker.githubFactory = func(string) github.ClientInterface {
		return &mockGithubClient{getCommitsFunc: func(context.Context, string, string, string, int) ([]github.Commit, error) { return nil, nil }}
	}
	worker.calculateMetrics = func(context.Context, string, string, string) (*ai.CodeMetrics, error) {
		return &ai.CodeMetrics{}, nil
	}

	payload := tasks.AnalyzeRepoPayload{RepositoryID: "r1", Branch: "main", CommitSHA: "abc", Type: "code_review"}
	data, _ := json.Marshal(payload)
	if err := worker.Handle(context.Background(), asynq.NewTask(tasks.TypeAnalyzeRepo, data)); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if saved == nil {
		t.Fatal("expected saved")
	}
	if saved.Metrics.TestCoverage != 0 || saved.Metrics.CoverageStatus != "" {
		t.Fatalf("metrics should be unmeasured: %+v", saved.Metrics)
	}
}

func TestAnalysisWorker_Handle_PRModeUsesPullRequestDiff(t *testing.T) {
	mockRepo := &mockRepository{
		getRepoFunc: func(ctx context.Context, id string) (*models.Repository, error) {
			return &models.Repository{
				ID:             id,
				OrganizationID: "org-1",
				Name:           "test-repo",
				URL:            "https://github.com/owner/repo",
				Metadata: models.RepositoryMetadata{
					DefaultBranch: "main",
				},
			}, nil
		},
		updateRepoFunc: func(ctx context.Context, repo *models.Repository) error {
			return nil
		},
		createAnalysisFunc: func(ctx context.Context, analysis *models.CodeAnalysis) error {
			if analysis.PullRequestID == nil || *analysis.PullRequestID != 42 {
				t.Fatalf("analysis PullRequestID = %+v, want 42", analysis.PullRequestID)
			}
			return nil
		},
		getConfigFunc: func(ctx context.Context, orgID string) (*models.OrganizationConfig, error) {
			return &models.OrganizationConfig{
				OrganizationID:  orgID,
				AnthropicAPIKey: "anthropic-key",
				GithubToken:     "github-token",
			}, nil
		},
	}

	mockGH := &mockGithubClient{
		getCommitsFunc: func(ctx context.Context, owner, repo, branch string, limit int) ([]github.Commit, error) {
			t.Fatal("GetCommits should not be called for PR analysis")
			return nil, nil
		},
		getPullRequestFunc: func(ctx context.Context, owner, repo string, prID int64) (*github.PullRequest, error) {
			if owner != "owner" || repo != "repo" || prID != 42 {
				t.Fatalf("GetPullRequest(%q, %q, %d), want owner/repo/42", owner, repo, prID)
			}
			return &github.PullRequest{
				Number: 42,
				Title:  "Add auth",
				Body:   "PR body",
				User:   github.User{Login: "developer"},
			}, nil
		},
		getPullRequestFilesFunc: func(ctx context.Context, owner, repo string, prID int64) ([]github.PRFile, error) {
			return []github.PRFile{
				{Filename: "auth.go", Status: "modified", Patch: "@@ -1 +1 @@\n-old\n+new"},
				{Filename: "go.sum", Status: "modified", Patch: "+dependency"},
			}, nil
		},
	}

	mockAnalyzer := &ai.MockAnalyzer{
		AnalyzeCodeFunc: func(ctx context.Context, req *ai.AnalysisRequest) (*ai.AnalysisResult, error) {
			if req.PullRequestID != 42 {
				t.Fatalf("AnalysisRequest PullRequestID = %d, want 42", req.PullRequestID)
			}
			if req.PRTitle != "Add auth" || req.PRBody != "PR body" || req.PRAuthor != "developer" {
				t.Fatalf("AnalysisRequest PR metadata = title %q body %q author %q", req.PRTitle, req.PRBody, req.PRAuthor)
			}
			if len(req.RecentCommits) != 0 {
				t.Fatalf("AnalysisRequest RecentCommits = %+v, want empty for PR analysis", req.RecentCommits)
			}
			if req.Metrics == nil || req.Metrics.LinesOfCode != 0 {
				t.Fatalf("AnalysisRequest metrics = %+v, want zero-value metrics for PR analysis", req.Metrics)
			}
			if len(req.ChangedFiles) != 1 {
				t.Fatalf("AnalysisRequest ChangedFiles = %+v, want one filtered diff", req.ChangedFiles)
			}
			if req.ChangedFiles[0].Path != "auth.go" || req.ChangedFiles[0].Patch == "" {
				t.Fatalf("AnalysisRequest ChangedFiles[0] = %+v, want auth.go with patch", req.ChangedFiles[0])
			}
			return &ai.AnalysisResult{
				Summary:    "PR analysis",
				Issues:     []ai.CodeIssue{},
				Model:      "test",
				TokensUsed: 100,
			}, nil
		},
	}

	worker := NewAnalysisWorker(mockRepo)
	worker.analyzerFactory = func(apiKey string) ai.Analyzer {
		return mockAnalyzer
	}
	worker.githubFactory = func(token string) github.ClientInterface {
		return mockGH
	}
	worker.calculateMetrics = func(ctx context.Context, repoURL, githubToken, branch string) (*ai.CodeMetrics, error) {
		t.Fatal("calculateMetrics should not be called for PR analysis")
		return nil, nil
	}

	payload := tasks.AnalyzeRepoPayload{
		RepositoryID:  "repo-1",
		Branch:        "feature/auth",
		CommitSHA:     "def456",
		Type:          "code_review",
		PullRequestID: 42,
	}

	data, _ := json.Marshal(payload)
	task := asynq.NewTask(tasks.TypeAnalyzeRepo, data)

	err := worker.Handle(context.Background(), task)
	if err != nil {
		t.Fatalf("Handle failed: %v", err)
	}
}

func TestAnalysisWorker_Handle_PRFailurePreservesPullRequestScope(t *testing.T) {
	var captured *models.CodeAnalysis
	mockRepo := &mockRepository{
		getRepoFunc: func(ctx context.Context, id string) (*models.Repository, error) {
			return &models.Repository{
				ID:             id,
				OrganizationID: "org-1",
				Name:           "test-repo",
				URL:            "https://github.com/owner/repo",
			}, nil
		},
		updateRepoFunc: func(ctx context.Context, repo *models.Repository) error {
			return nil
		},
		createAnalysisFunc: func(ctx context.Context, analysis *models.CodeAnalysis) error {
			copied := *analysis
			captured = &copied
			return nil
		},
		getConfigFunc: func(ctx context.Context, orgID string) (*models.OrganizationConfig, error) {
			return &models.OrganizationConfig{
				OrganizationID:  orgID,
				AnthropicAPIKey: "anthropic-key",
				GithubToken:     "github-token",
			}, nil
		},
	}

	worker := NewAnalysisWorker(mockRepo)
	worker.githubFactory = func(token string) github.ClientInterface {
		return &mockGithubClient{
			getPullRequestFunc: func(ctx context.Context, owner, repo string, prID int64) (*github.PullRequest, error) {
				return nil, github.ErrNotFound
			},
		}
	}

	payload := tasks.AnalyzeRepoPayload{
		RepositoryID:  "repo-1",
		Branch:        "feature",
		CommitSHA:     "abc123",
		Type:          "code_review",
		PullRequestID: 42,
		TriggeredBy:   "webhook",
	}
	data, _ := json.Marshal(payload)
	task := asynq.NewTask(tasks.TypeAnalyzeRepo, data)

	err := worker.Handle(context.Background(), task)
	if err == nil {
		t.Fatal("expected Handle to fail")
	}
	if captured == nil {
		t.Fatal("expected failed analysis to be captured")
	}
	if captured.PullRequestID == nil || *captured.PullRequestID != 42 {
		t.Fatalf("PullRequestID = %+v, want 42", captured.PullRequestID)
	}
	if captured.Branch != "feature" || captured.CommitSHA != "abc123" {
		t.Fatalf("scope = %q/%q, want feature/abc123", captured.Branch, captured.CommitSHA)
	}
	if captured.Status != models.AnalysisStatusFailed {
		t.Fatalf("status = %q, want failed", captured.Status)
	}
}

func TestAnalysisWorker_Handle_InvalidPayload(t *testing.T) {
	worker := NewAnalysisWorker(nil)

	task := asynq.NewTask(tasks.TypeAnalyzeRepo, []byte("invalid json"))

	err := worker.Handle(context.Background(), task)
	if err == nil {
		t.Fatal("Expected error for invalid payload")
	}
}

func TestBuildAnalysisRequest_WithProjectStandards(t *testing.T) {
	mockRepo := &mockRepository{
		getLatestDocGenerationFunc: func(ctx context.Context, repoID string) (*models.DocGeneration, error) {
			if repoID != "repo-1" {
				t.Fatalf("repoID = %q, want repo-1", repoID)
			}
			return &models.DocGeneration{
				RepositoryID: repoID,
				Status:       models.DocGenerationStatusCompleted,
				Content: datatypes.NewJSONType(map[string]string{
					"guidelines": "Use explicit errors.",
					"adr":        "ADR-003: Prefer async workers.",
				}),
			}, nil
		},
	}
	worker := NewAnalysisWorker(mockRepo)
	req, _, err := worker.buildAnalysisRequest(
		context.Background(),
		&mockGithubClient{},
		&models.Repository{
			ID:             "repo-1",
			Name:           "repo",
			URL:            "https://github.com/owner/repo",
			OrganizationID: "org-1",
			Metadata:       models.RepositoryMetadata{DefaultBranch: "main"},
		},
		tasks.AnalyzeRepoPayload{RepositoryID: "repo-1", Type: "code_review"},
		"owner",
		"repo",
		&ai.CodeMetrics{},
	)
	if err != nil {
		t.Fatalf("buildAnalysisRequest returned error: %v", err)
	}
	if !strings.Contains(req.ProjectStandards, "Use explicit errors.") || !strings.Contains(req.ProjectStandards, "ADR-003") {
		t.Fatalf("ProjectStandards = %q, want generated docs", req.ProjectStandards)
	}
}
