package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

type createWebhookRequest struct {
	Name   string              `json:"name"`
	Active bool                `json:"active"`
	Events []string            `json:"events"`
	Config webhookConfigPayload `json:"config"`
}

type webhookConfigPayload struct {
	URL         string `json:"url"`
	ContentType string `json:"content_type"`
	Secret      string `json:"secret"`
	InsecureSSL string `json:"insecure_ssl"`
}

type createWebhookResponse struct {
	ID int64 `json:"id"`
}

func (c *Client) CreateWebhook(ctx context.Context, owner, repo, webhookURL, secret string) (int64, error) {
	payload := createWebhookRequest{
		Name:   "web",
		Active: true,
		Events: []string{"push", "pull_request", "issues"},
		Config: webhookConfigPayload{
			URL:         webhookURL,
			ContentType: "json",
			Secret:      secret,
			InsecureSSL: "0",
		},
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return 0, fmt.Errorf("marshal webhook payload: %w", err)
	}

	var result createWebhookResponse
	path := fmt.Sprintf("/repos/%s/%s/hooks", owner, repo)
	if err := c.do(ctx, http.MethodPost, path, bytes.NewReader(data), &result); err != nil {
		return 0, err
	}
	return result.ID, nil
}

func (c *Client) DeleteWebhook(ctx context.Context, owner, repo string, webhookID int64) error {
	path := fmt.Sprintf("/repos/%s/%s/hooks/%d", owner, repo, webhookID)
	return c.do(ctx, http.MethodDelete, path, nil, nil)
}
