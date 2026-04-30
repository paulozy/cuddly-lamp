package workers

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/hibiken/asynq"
	"github.com/paulozy/idp-with-ai-backend/internal/ai"
	"github.com/paulozy/idp-with-ai-backend/internal/integrations/github"
	"github.com/paulozy/idp-with-ai-backend/internal/jobs/tasks"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"gorm.io/datatypes"
)

type docsMockGithub struct {
	github.ClientInterface
	files map[string]string
	pr    *github.PullRequest
}

func (m *docsMockGithub) GetCommits(context.Context, string, string, string, int) ([]github.Commit, error) {
	return nil, nil
}

func (m *docsMockGithub) ListPullRequests(context.Context, string, string) ([]github.PullRequest, error) {
	return nil, nil
}

func (m *docsMockGithub) CreateBranch(context.Context, string, string, string, string) error {
	return nil
}

func (m *docsMockGithub) CreateOrUpdateFile(_ context.Context, _, _, _, path, _, content string) error {
	m.files[path] = content
	return nil
}

func (m *docsMockGithub) CreatePullRequest(context.Context, string, string, string, string, string, string) (*github.PullRequest, error) {
	return m.pr, nil
}

func TestDocsWorker_Handle(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "README.md"), []byte("# Repo"), 0o600); err != nil {
		t.Fatalf("write README: %v", err)
	}

	doc := &models.DocGeneration{
		ID:           "doc-1",
		RepositoryID: "repo-1",
		Status:       models.DocGenerationStatusPending,
		Types:        datatypes.JSONSlice[string]{"guidelines", "adr"},
		Content:      datatypes.NewJSONType(map[string]string{}),
	}
	var completed *models.DocGeneration
	mockRepo := &mockRepository{
		getDocGenerationFunc: func(ctx context.Context, id string) (*models.DocGeneration, error) {
			return doc, nil
		},
		getRepoFunc: func(ctx context.Context, id string) (*models.Repository, error) {
			return &models.Repository{
				ID:             id,
				OrganizationID: "org-1",
				Name:           "repo",
				URL:            "https://github.com/owner/repo",
				Type:           models.RepositoryTypeGitHub,
				Metadata:       models.RepositoryMetadata{DefaultBranch: "main", Languages: map[string]int{"Go": 10}},
			}, nil
		},
		getConfigFunc: func(ctx context.Context, orgID string) (*models.OrganizationConfig, error) {
			return &models.OrganizationConfig{OrganizationID: orgID, AnthropicAPIKey: "anthropic", GithubToken: "github"}, nil
		},
		updateDocGenerationFunc: func(ctx context.Context, doc *models.DocGeneration) error {
			copy := *doc
			completed = &copy
			return nil
		},
	}
	gh := &docsMockGithub{files: map[string]string{}, pr: &github.PullRequest{Number: 7, HTMLURL: "https://github.com/owner/repo/pull/7"}}
	worker := NewDocsWorker(mockRepo)
	worker.cloneRepo = func(context.Context, string, string, string) (string, func(), error) {
		return tmp, func() {}, nil
	}
	worker.githubFactory = func(string) github.ClientInterface { return gh }
	worker.generatorFactory = func(string) ai.DocumentationGenerator {
		return &ai.MockDocumentationGenerator{
			GenerateDocumentationFunc: func(ctx context.Context, req *ai.DocumentationRequest) (*ai.DocumentationResult, error) {
				return &ai.DocumentationResult{Content: "# " + string(req.Type), Model: "mock", TokensUsed: 10}, nil
			},
		}
	}

	data, err := json.Marshal(tasks.GenerateDocsPayload{DocGenerationID: "doc-1", RepositoryID: "repo-1"})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	task := asynq.NewTask(tasks.TypeGenerateDocs, data)
	if err := worker.Handle(context.Background(), task); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if completed == nil || completed.Status != models.DocGenerationStatusCompleted || completed.PullRequestNumber != 7 {
		t.Fatalf("completed = %+v, want completed PR 7", completed)
	}
	if gh.files["CONTRIBUTING.md"] == "" || gh.files["docs/adr/README.md"] == "" {
		t.Fatalf("files = %+v, want guidelines and ADR commits", gh.files)
	}
}
