package anthropic

import (
	"strings"
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
		TruncatedFiles: []string{"large.go"},
	}

	prompt := client.buildPrompt(req)

	// Check PR-specific components
	tests := []string{
		"PULL REQUEST",
		"42",
		"authentication",
		"CHANGED FILES",
		"auth.go",
		"<diff file=\"auth.go\" status=\"modified\">",
		"Focus exclusively on the changes",
		"FILES NOT SHOWN",
		"large.go",
	}

	for _, test := range tests {
		if !contains(prompt, test) {
			t.Errorf("PR prompt missing expected string: %s", test)
		}
	}
}

func TestClient_BuildPrompt_DependencyAnalysis(t *testing.T) {
	client := NewClient("test-key")
	req := &ai.AnalysisRequest{
		RepoName:     "my-api",
		Branch:       "main",
		AnalysisType: ai.AnalysisTypeDependency,
		ChangedFiles: []ai.ChangedFile{
			{Path: "go.mod", Status: "modified", Patch: "require golang.org/x/crypto v0.1.0"},
		},
	}

	prompt := client.buildPrompt(req)
	for _, want := range []string{"Dependency Analysis", "Known CVEs", "recommended_version", "go.mod"} {
		if !contains(prompt, want) {
			t.Fatalf("dependency prompt missing %q", want)
		}
	}
}

func TestClient_ParseResponse_RecommendedVersion(t *testing.T) {
	client := NewClient("test-key")
	result, err := client.parseResponse(`{
		"summary": "dependency issue",
		"issues": [{
			"category": "vulnerable_dependency",
			"severity": "high",
			"title": "x/crypto vulnerable",
			"description": "CVE-2024-0001",
			"suggestion": "Upgrade package",
			"file_path": "go.mod",
			"line": 4,
			"recommended_version": "v0.31.0"
		}],
		"metrics": {"lines_of_code": 0, "cyclomatic_complexity": 0}
	}`, 123)
	if err != nil {
		t.Fatalf("parseResponse returned error: %v", err)
	}
	if len(result.Issues) != 1 {
		t.Fatalf("issues = %d, want 1", len(result.Issues))
	}
	if !contains(result.Issues[0].Suggestion, "recommended: v0.31.0") {
		t.Fatalf("suggestion = %q", result.Issues[0].Suggestion)
	}
}

// TestClient_AnalyzeCode_NoAPIKey tests error handling when API key is invalid
// (Skipped: requires valid API key to run full integration test)

// TestExtractJSON tests the extractJSON function with various input formats
func TestExtractJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Plain JSON",
			input:    `{"summary": "test"}`,
			expected: `{"summary": "test"}`,
		},
		{
			name:     "JSON with ```json fence",
			input:    "```json\n{\"summary\": \"test\"}\n```",
			expected: `{"summary": "test"}`,
		},
		{
			name:     "JSON with ``` fence (no language tag)",
			input:    "```\n{\"summary\": \"test\"}\n```",
			expected: `{"summary": "test"}`,
		},
		{
			name:     "Truncated response with ```json but no closing fence",
			input:    "```json\n{\"summary\": \"test\", \"issues\": [{\"title\": \"bug\"}",
			expected: `{"summary": "test", "issues": [{"title": "bug"}`,
		},
		{
			name:     "Mixed content with JSON",
			input:    "Here is the analysis:\n```json\n{\"summary\": \"code review\"}\n```\nEnd of response",
			expected: `{"summary": "code review"}`,
		},
		{
			name:     "JSON without fences but with surrounding text",
			input:    "Analysis result: {\"summary\": \"test\"} end",
			expected: `{"summary": "test"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractJSON(tt.input)
			if result != tt.expected {
				t.Errorf("extractJSON() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
