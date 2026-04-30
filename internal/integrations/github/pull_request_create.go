package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
)

func (c *Client) CreatePullRequest(ctx context.Context, owner, repo, title, head, base, body string) (*PullRequest, error) {
	reqBody := map[string]string{
		"title": title,
		"head":  head,
		"base":  base,
		"body":  body,
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal create pull request: %w", err)
	}

	var pr PullRequest
	path := fmt.Sprintf("/repos/%s/%s/pulls", owner, repo)
	if err := c.do(ctx, "POST", path, bytes.NewReader(bodyBytes), &pr); err != nil {
		return nil, err
	}
	return &pr, nil
}
