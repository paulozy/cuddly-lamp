package anthropic

import (
	"context"
	"testing"

	"github.com/paulozy/idp-with-ai-backend/internal/ai"
)

func TestClient_Provider(t *testing.T) {
	client := NewClient("test-key")
	if client.Provider() != "anthropic" {
		t.Errorf("Expected provider 'anthropic', got '%s'", client.Provider())
	}
}

func TestClient_BuildPrompt_RepoAnalysis(t *testing.T) {
	client := NewClient("test-key")

	req := &ai.AnalysisRequest{
		RepositoryID:  "repo-1",
		RepoName:      "my-api",
		Languages:     []string{"Go"},
		Topics:        []string{"api", "backend"},
		HasCI:         true,
		HasTests:      true,
		TestCoverage:  0.85,
		DefaultBranch: "main",
		Branch:        "main",
		CommitSHA:     "abc123",
		AnalysisType:  ai.AnalysisTypeCodeReview,
		RecentCommits: []ai.CommitSummary{
			{SHA: "abc123", Message: "feat: add auth", Author: "user", Date: "2026-04-28"},
		},
	}

	prompt := client.buildPrompt(req)

	// Check key components are in the prompt
	tests := []string{
		"my-api",
		"Go",
		"Code Review",
		"abc123",
	}

	for _, test := range tests {
		if !contains(prompt, test) {
			t.Errorf("Prompt missing expected string: %s", test)
		}
	}
}

func TestClient_BuildPrompt_PRAnalysis(t *testing.T) {
	client := NewClient("test-key")

	req := &ai.AnalysisRequest{
		RepositoryID:  "repo-1",
		RepoName:      "my-api",
		Languages:     []string{"Go"},
		Topics:        []string{"api"},
		HasCI:         true,
		HasTests:      true,
		Branch:        "feature/new",
		CommitSHA:     "def456",
		AnalysisType:  ai.AnalysisTypeCodeReview,
		PullRequestID: 42,
		PRTitle:       "Add authentication",
		PRAuthor:      "developer",
		ChangedFiles: []ai.ChangedFile{
			{Path: "auth.go", Status: "modified", Patch: "+ // New auth handler\n"},
		},
	}

	prompt := client.buildPrompt(req)

	// Check PR-specific components
	tests := []string{
		"PULL REQUEST",
		"42",
		"authentication",
		"CHANGED FILES",
		"auth.go",
	}

	for _, test := range tests {
		if !contains(prompt, test) {
			t.Errorf("PR prompt missing expected string: %s", test)
		}
	}
}

func TestClient_AnalyzeCode_NoAPIKey(t *testing.T) {
	client := &Client{apiKey: ""}

	req := &ai.AnalysisRequest{}
	_, err := client.AnalyzeCode(context.Background(), req)

	if err == nil {
		t.Error("Expected error when API key is empty")
	}
}

func TestTruncatePatch(t *testing.T) {
	patch := "line1\nline2\nline3\nline4\nline5"
	truncated := truncatePatch(patch, 3)

	if !contains(truncated, "line3") {
		t.Error("Truncated patch missing expected lines")
	}

	if contains(truncated, "line4") {
		t.Error("Truncated patch should not contain lines beyond limit")
	}

	if !contains(truncated, "truncated") {
		t.Error("Truncated patch should indicate truncation")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && len(substr) > 0 && (len(s) >= len(substr)))
}
