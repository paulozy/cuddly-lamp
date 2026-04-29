package workers

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hibiken/asynq"
	"github.com/paulozy/idp-with-ai-backend/internal/ai"
	"github.com/paulozy/idp-with-ai-backend/internal/integrations/github"
	"github.com/paulozy/idp-with-ai-backend/internal/jobs/tasks"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
)

type mockRepository struct {
	storage.Repository
	getRepoFunc        func(ctx context.Context, id string) (*models.Repository, error)
	updateRepoFunc     func(ctx context.Context, repo *models.Repository) error
	createAnalysisFunc func(ctx context.Context, analysis *models.CodeAnalysis) error
	getConfigFunc      func(ctx context.Context, orgID string) (*models.OrganizationConfig, error)
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

func (m *mockRepository) GetOrganizationConfig(ctx context.Context, orgID string) (*models.OrganizationConfig, error) {
	if m.getConfigFunc != nil {
		return m.getConfigFunc(ctx, orgID)
	}
	return nil, nil
}

type mockGithubClient struct {
	github.ClientInterface
	getCommitsFunc func(ctx context.Context, owner, repo, branch string, limit int) ([]github.Commit, error)
}

func (m *mockGithubClient) GetCommits(ctx context.Context, owner, repo, branch string, limit int) ([]github.Commit, error) {
	if m.getCommitsFunc != nil {
		return m.getCommitsFunc(ctx, owner, repo, branch, limit)
	}
	return []github.Commit{}, nil
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

func TestAnalysisWorker_Handle_InvalidPayload(t *testing.T) {
	worker := NewAnalysisWorker(nil)

	task := asynq.NewTask(tasks.TypeAnalyzeRepo, []byte("invalid json"))

	err := worker.Handle(context.Background(), task)
	if err == nil {
		t.Fatal("Expected error for invalid payload")
	}
}
