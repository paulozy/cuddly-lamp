package workers

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/hibiken/asynq"
	"github.com/paulozy/idp-with-ai-backend/internal/ai"
	"github.com/paulozy/idp-with-ai-backend/internal/jobs/tasks"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
)

func TestDependencyWorker_Handle(t *testing.T) {
	repoDir := createTestGitRepo(t, map[string]string{
		"go.mod": "module example.com/app\n\nrequire golang.org/x/crypto v0.1.0\n",
	})

	var upserted []*models.PackageDependency
	var updatedVuln bool
	mockRepo := &mockRepository{
		getRepoFunc: func(ctx context.Context, id string) (*models.Repository, error) {
			return &models.Repository{
				ID:             id,
				OrganizationID: "org-1",
				Name:           "test-repo",
				URL:            repoDir,
				Type:           models.RepositoryTypeCustom,
				Metadata: models.RepositoryMetadata{
					DefaultBranch: "master",
				},
			}, nil
		},
		getConfigFunc: func(ctx context.Context, orgID string) (*models.OrganizationConfig, error) {
			return &models.OrganizationConfig{OrganizationID: orgID, AnthropicAPIKey: "key"}, nil
		},
		createAnalysisFunc: func(ctx context.Context, analysis *models.CodeAnalysis) error {
			analysis.ID = "analysis-1"
			if analysis.Type != models.AnalysisTypeDependency {
				t.Fatalf("analysis type = %q, want dependency", analysis.Type)
			}
			return nil
		},
		updateRepoFunc: func(ctx context.Context, repo *models.Repository) error {
			return nil
		},
	}
	mockRepo.upsertPackageDependencyFunc = func(ctx context.Context, dep *models.PackageDependency) error {
		dep.ID = "dep-1"
		upserted = append(upserted, dep)
		return nil
	}
	mockRepo.listPackageDependenciesFunc = func(ctx context.Context, repoID string, onlyVulnerable bool) ([]*models.PackageDependency, error) {
		return upserted, nil
	}
	mockRepo.updatePackageDependencyVulnStatusFunc = func(ctx context.Context, id string, isVulnerable bool, cves []string, latestVersion string) error {
		updatedVuln = true
		if id != "dep-1" || !isVulnerable || latestVersion != "v0.31.0" {
			t.Fatalf("vuln update = id:%s vuln:%v cves:%v latest:%s", id, isVulnerable, cves, latestVersion)
		}
		return nil
	}
	mockRepo.updateAnalysisFunc = func(ctx context.Context, analysis *models.CodeAnalysis) error {
		if analysis.Status != models.AnalysisStatusCompleted {
			t.Fatalf("analysis status = %q, want completed", analysis.Status)
		}
		if analysis.IssueCount != 1 {
			t.Fatalf("analysis issue count = %d, want 1", analysis.IssueCount)
		}
		return nil
	}

	analyzer := &ai.MockAnalyzer{
		AnalyzeCodeFunc: func(ctx context.Context, req *ai.AnalysisRequest) (*ai.AnalysisResult, error) {
			if req.AnalysisType != ai.AnalysisTypeDependency {
				t.Fatalf("analysis type = %q, want dependency", req.AnalysisType)
			}
			if len(req.ChangedFiles) != 1 || req.ChangedFiles[0].Path != "go.mod" {
				t.Fatalf("changed files = %+v, want go.mod", req.ChangedFiles)
			}
			return &ai.AnalysisResult{
				Summary: "vulnerable dependency",
				Issues: []ai.CodeIssue{{
					Category:    "vulnerable_dependency",
					Severity:    "high",
					Title:       "golang.org/x/crypto vulnerable",
					Description: "CVE-2024-0001",
					Suggestion:  "Update package (recommended: v0.31.0)",
					FilePath:    "go.mod",
					Line:        3,
				}},
				Model: "mock",
			}, nil
		},
	}

	payload := tasks.ScanDependenciesPayload{RepositoryID: "repo-1", Branch: "master", TriggeredBy: "user"}
	data, _ := json.Marshal(payload)
	err := NewDependencyWorker(mockRepo, analyzer, nil).Handle(context.Background(), asynq.NewTask(tasks.TypeScanDependencies, data))
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if len(upserted) != 1 {
		t.Fatalf("upserted dependencies = %d, want 1", len(upserted))
	}
	if upserted[0].Name != "golang.org/x/crypto" || upserted[0].CurrentVersion != "v0.1.0" {
		t.Fatalf("upserted dependency = %+v", upserted[0])
	}
	if !updatedVuln {
		t.Fatal("expected vulnerability status update")
	}
}

func createTestGitRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("init git repo: %v", err)
	}
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}
	if _, err := wt.Add("."); err != nil {
		t.Fatalf("git add: %v", err)
	}
	_, err = wt.Commit("initial", &git.CommitOptions{
		Author: &object.Signature{Name: "Test", Email: "test@example.com"},
	})
	if err != nil {
		t.Fatalf("git commit: %v", err)
	}
	return dir
}
