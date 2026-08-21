package anthropic

import (
	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

const defaultModel = "claude-haiku-4-5-20251001"

// Client implements ai.DocumentationGenerator using the Anthropic SDK.
type Client struct {
	client *anthropic.Client
	model  string
}

// NewClient creates a new Anthropic client.
func NewClient(apiKey string) *Client {
	client := anthropic.NewClient(
		option.WithAPIKey(apiKey),
	)
	return &Client{
		client: &client,
		model:  defaultModel,
	}
}

// Provider returns the name of the AI provider.
func (c *Client) Provider() string {
	return "anthropic"
}
