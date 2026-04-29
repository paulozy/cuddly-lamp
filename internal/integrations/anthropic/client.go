package anthropic

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/paulozy/idp-with-ai-backend/internal/ai"
)

const (
	defaultModel = "claude-haiku-4-5-20251001"
	maxTokens    = 4096
)

// Client implements ai.Analyzer using the Anthropic SDK
type Client struct {
	client *anthropic.Client
	model  string
}

// NewClient creates a new Anthropic client
func NewClient(apiKey string) *Client {
	client := anthropic.NewClient(
		option.WithAPIKey(apiKey),
	)
	return &Client{
		client: &client,
		model:  defaultModel,
	}
}

// AnalyzeCode implements ai.Analyzer
func (c *Client) AnalyzeCode(ctx context.Context, req *ai.AnalysisRequest) (*ai.AnalysisResult, error) {
	prompt := c.buildPrompt(req)

	// Call Claude API using SDK
	message, err := c.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     c.model,
		MaxTokens: int64(maxTokens),
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("anthropic api error: %w", err)
	}

	// Extract text content from response
	var responseText string
	if message.Content != nil && len(message.Content) > 0 {
		// Access the first content block - it's a union type with Type and Text fields
		if message.Content[0].Type == "text" {
			responseText = message.Content[0].Text
		}
	}

	if responseText == "" {
		return nil, fmt.Errorf("empty response from anthropic")
	}

	// Get token usage
	tokensUsed := int(message.Usage.InputTokens + message.Usage.OutputTokens)

	return c.parseResponse(responseText, tokensUsed)
}

// Provider implements ai.Analyzer
func (c *Client) Provider() string {
	return "anthropic"
}

// buildPrompt constructs a prompt for Claude based on the analysis request
func (c *Client) buildPrompt(req *ai.AnalysisRequest) string {
	sb := strings.Builder{}

	sb.WriteString("You are an expert code reviewer and software architect. ")
	sb.WriteString("Analyze the following code repository information and provide structured feedback.\n\n")

	sb.WriteString("REPOSITORY INFO:\n")
	sb.WriteString(fmt.Sprintf("- Name: %s\n", req.RepoName))
	sb.WriteString(fmt.Sprintf("- Languages: %s\n", strings.Join(req.Languages, ", ")))
	sb.WriteString(fmt.Sprintf("- Topics: %s\n", strings.Join(req.Topics, ", ")))
	sb.WriteString(fmt.Sprintf("- Has CI/CD: %v\n", req.HasCI))
	sb.WriteString(fmt.Sprintf("- Has Tests: %v\n", req.HasTests))
	sb.WriteString(fmt.Sprintf("- Test Coverage: %.1f%%\n", req.TestCoverage*100))

	sb.WriteString("\nCONTEXT:\n")
	sb.WriteString(fmt.Sprintf("- Branch: %s\n", req.Branch))
	sb.WriteString(fmt.Sprintf("- Commit: %s\n", req.CommitSHA))

	// For PR analysis
	if req.PullRequestID > 0 {
		sb.WriteString("\nPULL REQUEST:\n")
		sb.WriteString(fmt.Sprintf("- PR ID: %d\n", req.PullRequestID))
		sb.WriteString(fmt.Sprintf("- Title: %s\n", req.PRTitle))
		sb.WriteString(fmt.Sprintf("- Author: %s\n", req.PRAuthor))
		if req.PRBody != "" {
			sb.WriteString(fmt.Sprintf("- Description: %s\n", req.PRBody))
		}

		sb.WriteString("\nCHANGED FILES:\n")
		for _, file := range req.ChangedFiles {
			sb.WriteString(fmt.Sprintf("\n%s (%s):\n```\n%s\n```\n", file.Path, file.Status, truncatePatch(file.Patch, 500)))
		}
	}

	// For commit-based analysis
	if len(req.RecentCommits) > 0 {
		sb.WriteString("\nRECENT COMMITS:\n")
		for _, commit := range req.RecentCommits {
			shaShort := commit.SHA
			if len(shaShort) > 7 {
				shaShort = shaShort[:7]
			}
			sb.WriteString(fmt.Sprintf("- %s: %s (by %s)\n", shaShort, commit.Message, commit.Author))
		}
	}

	sb.WriteString("\n\nANALYSIS TYPE: ")
	switch req.AnalysisType {
	case ai.AnalysisTypeCodeReview:
		sb.WriteString("Code Review - Analyze code quality, best practices, potential bugs.\n")
	case ai.AnalysisTypeSecurity:
		sb.WriteString("Security Analysis - Focus on security vulnerabilities and best practices.\n")
	case ai.AnalysisTypeArchitecture:
		sb.WriteString("Architecture Review - Evaluate system design and architectural decisions.\n")
	default:
		sb.WriteString("Code Review\n")
	}

	sb.WriteString("\nProvide your analysis as a JSON response with the following structure:\n")
	sb.WriteString(`{"summary": "...", "issues": [{"category": "...", "severity": "...", "title": "...", "description": "...", "suggestion": "...", "file_path": "...", "line": 0}], "metrics": {"lines_of_code": 0, "cyclomatic_complexity": 0, "test_coverage": 0.0}}`)
	sb.WriteString("\n\nIMPORTANT: Return ONLY valid JSON. Do not wrap your response in markdown code fences or backticks.")

	return sb.String()
}

// parseResponse parses Claude's JSON response into AnalysisResult
func (c *Client) parseResponse(text string, tokensUsed int) (*ai.AnalysisResult, error) {
	// Try to extract JSON from the response (Claude may wrap in markdown code blocks)
	jsonStr := extractJSON(text)

	var rawResp map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &rawResp); err != nil {
		return nil, fmt.Errorf("failed to parse claude response as json: %w (text: %s)", err, text[:min(len(text), 200)])
	}

	result := &ai.AnalysisResult{
		Model:      c.model,
		TokensUsed: tokensUsed,
		Issues:     []ai.CodeIssue{},
		Metrics:    ai.CodeMetrics{},
	}

	// Extract summary
	if summary, ok := rawResp["summary"].(string); ok {
		result.Summary = summary
	}

	// Extract issues
	if issuesRaw, ok := rawResp["issues"].([]interface{}); ok {
		for _, issueRaw := range issuesRaw {
			if issueMap, ok := issueRaw.(map[string]interface{}); ok {
				issue := ai.CodeIssue{
					IsAIGenerated: true,
					Confidence:    0.85,
				}

				if v, ok := issueMap["category"].(string); ok {
					issue.Category = v
				}
				if v, ok := issueMap["severity"].(string); ok {
					issue.Severity = v
				}
				if v, ok := issueMap["title"].(string); ok {
					issue.Title = v
				}
				if v, ok := issueMap["description"].(string); ok {
					issue.Description = v
				}
				if v, ok := issueMap["suggestion"].(string); ok {
					issue.Suggestion = v
				}
				if v, ok := issueMap["file_path"].(string); ok {
					issue.FilePath = v
				}
				if v, ok := issueMap["line"].(float64); ok {
					issue.Line = int(v)
				}

				result.Issues = append(result.Issues, issue)
			}
		}
	}

	// Extract metrics
	if metricsRaw, ok := rawResp["metrics"].(map[string]interface{}); ok {
		if v, ok := metricsRaw["lines_of_code"].(float64); ok {
			result.Metrics.LinesOfCode = int32(v)
		}
		if v, ok := metricsRaw["cyclomatic_complexity"].(float64); ok {
			result.Metrics.CyclomaticComplexity = int32(v)
		}
		if v, ok := metricsRaw["test_coverage"].(float64); ok {
			result.Metrics.TestCoverage = float32(v)
		}
	}

	return result, nil
}

// extractJSON extracts JSON from text that may be wrapped in markdown code blocks
func extractJSON(text string) string {
	// Branch 1: Try to extract from ```json ... ``` code block
	if idx := strings.Index(text, "```json"); idx != -1 {
		start := idx + len("```json")
		if end := strings.Index(text[start:], "```"); end != -1 {
			return strings.TrimSpace(text[start : start+end])
		}
		// Closing fence missing (response may be truncated) — fall through to brace extraction
	}
	// Branch 2: Try to extract from ``` ... ``` code block (without language tag)
	if idx := strings.Index(text, "```"); idx != -1 {
		start := idx + len("```")
		// Skip language identifier (e.g., "json\n") if present
		if newline := strings.Index(text[start:], "\n"); newline != -1 {
			start += newline + 1
		}
		if end := strings.Index(text[start:], "```"); end != -1 {
			return strings.TrimSpace(text[start : start+end])
		}
		// Closing fence missing — fall through to brace extraction
	}
	// Branch 3: Extract raw JSON by finding { ... } boundaries (fallback for truncated responses)
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start != -1 && end != -1 && end > start {
		return strings.TrimSpace(text[start : end+1])
	}
	// No JSON found, return trimmed text as-is
	return strings.TrimSpace(text)
}

// min returns the smaller of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// truncatePatch limits patch length to avoid token limits
func truncatePatch(patch string, maxLines int) string {
	lines := strings.Split(patch, "\n")
	if len(lines) <= maxLines {
		return patch
	}

	return strings.Join(lines[:maxLines], "\n") + "\n... (truncated)"
}
