package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
)

// PRFile represents a file changed in a PR
type PRFile struct {
	SHA       string `json:"sha"`
	Filename  string `json:"filename"`
	Status    string `json:"status"` // added, modified, removed, renamed, copied, changed, unchanged
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Changes   int    `json:"changes"`
	Patch     string `json:"patch"` // Unified diff
}

// ReviewCommentInput represents a comment to add during review
type ReviewCommentInput struct {
	Path     string `json:"path"`
	Position int    `json:"position"`
	Body     string `json:"body"`
}

// GetPullRequest fetches details about a specific PR
func (c *Client) GetPullRequest(ctx context.Context, owner, repo string, prID int64) (*PullRequest, error) {
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d", owner, repo, prID)
	var pr PullRequest
	if err := c.do(ctx, "GET", path, nil, &pr); err != nil {
		return nil, err
	}
	return &pr, nil
}

// GetPullRequestFiles fetches the list of files changed in a PR
func (c *Client) GetPullRequestFiles(ctx context.Context, owner, repo string, prID int64) ([]PRFile, error) {
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d/files", owner, repo, prID)
	var files []PRFile
	if err := c.do(ctx, "GET", path, nil, &files); err != nil {
		return nil, err
	}
	return files, nil
}

// CreatePullRequestReview posts a review on a PR
func (c *Client) CreatePullRequestReview(ctx context.Context, owner, repo string, prID int64, body string, event string, comments []ReviewCommentInput) (int64, error) {
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d/reviews", owner, repo, prID)

	reqBody := map[string]interface{}{
		"body":     body,
		"event":    event, // COMMENT, APPROVE, REQUEST_CHANGES
		"comments": comments,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return 0, fmt.Errorf("marshal request: %w", err)
	}

	var respData map[string]interface{}
	if err := c.do(ctx, "POST", path, bytes.NewReader(bodyBytes), &respData); err != nil {
		return 0, err
	}

	// Extract review ID
	if id, ok := respData["id"].(float64); ok {
		return int64(id), nil
	}

	return 0, fmt.Errorf("missing review id in response")
}
