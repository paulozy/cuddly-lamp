package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/paulozy/idp-with-ai-backend/internal/ai"
)

const (
	defaultModel = "claude-haiku-4-5-20251001"
	maxTokens    = 2048
	apiVersion   = "2024-06-01"
	apiURL       = "https://api.anthropic.com/v1"
)

// Client implements ai.Analyzer using the Anthropic API
type Client struct {
	apiKey     string
	httpClient *http.Client
	model      string
}

// NewClient creates a new Anthropic client
func NewClient(apiKey string) *Client {
	return &Client{
		apiKey:     apiKey,
		httpClient: &http.Client{},
		model:      defaultModel,
	}
}

// AnalyzeCode implements ai.Analyzer
func (c *Client) AnalyzeCode(ctx context.Context, req *ai.AnalysisRequest) (*ai.AnalysisResult, error) {
	if c.apiKey == "" {
		return nil, fmt.Errorf("anthropic api key not configured")
	}

	prompt := c.buildPrompt(req)

	// Build request body
	reqBody := map[string]interface{}{
		"model":      c.model,
		"max_tokens": maxTokens,
		"messages": []map[string]string{
			{
				"role":    "user",
				"content": prompt,
			},
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "POST", apiURL+"/messages", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("anthropic-version", apiVersion)

	// Make request
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to call anthropic api: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("anthropic api error: status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var respData map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&respData); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Extract text content
	var responseText string
	if content, ok := respData["content"].([]interface{}); ok && len(content) > 0 {
		if textBlock, ok := content[0].(map[string]interface{}); ok {
			if text, ok := textBlock["text"].(string); ok {
				responseText = text
			}
		}
	}

	if responseText == "" {
		return nil, fmt.Errorf("empty response from anthropic")
	}

	// Get token usage
	var tokensUsed int64
	if usage, ok := respData["usage"].(map[string]interface{}); ok {
		if inputTokens, ok := usage["input_tokens"].(float64); ok {
			tokensUsed += int64(inputTokens)
		}
		if outputTokens, ok := usage["output_tokens"].(float64); ok {
			tokensUsed += int64(outputTokens)
		}
	}

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
			sb.WriteString(fmt.Sprintf("- %s: %s (by %s)\n", commit.SHA[:7], commit.Message, commit.Author))
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

	return sb.String()
}

// parseResponse parses Claude's JSON response into AnalysisResult
func (c *Client) parseResponse(text string, tokensUsed int64) (*ai.AnalysisResult, error) {
	// Try to extract JSON from the response
	jsonStr := text
	if strings.Contains(text, "```json") {
		start := strings.Index(text, "```json") + len("```json")
		end := strings.LastIndex(text, "```")
		if start > len("```json") && end > start {
			jsonStr = text[start:end]
		}
	} else if strings.Contains(text, "```") {
		start := strings.Index(text, "```") + len("```")
		end := strings.LastIndex(text, "```")
		if start > len("```") && end > start {
			jsonStr = text[start:end]
		}
	}

	var rawResp map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &rawResp); err != nil {
		return nil, fmt.Errorf("failed to parse claude response as json: %w", err)
	}

	result := &ai.AnalysisResult{
		Model:      c.model,
		TokensUsed: int(tokensUsed),
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

// truncatePatch limits patch length to avoid token limits
func truncatePatch(patch string, maxLines int) string {
	lines := strings.Split(patch, "\n")
	if len(lines) <= maxLines {
		return patch
	}

	return strings.Join(lines[:maxLines], "\n") + "\n... (truncated)"
}
