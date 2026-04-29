package embeddings

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const voyageEmbeddingsURL = "https://api.voyageai.com/v1/embeddings"

type VoyageClient struct {
	apiKey     string
	model      string
	dimension  int
	httpClient *http.Client
}

func NewVoyageClient(apiKey, model string, dimension int) *VoyageClient {
	if model == "" {
		model = "voyage-code-3"
	}
	if dimension <= 0 {
		dimension = 1024
	}
	return &VoyageClient{
		apiKey:    apiKey,
		model:     model,
		dimension: dimension,
		httpClient: &http.Client{
			Timeout: 45 * time.Second,
		},
	}
}

func (c *VoyageClient) Provider() string {
	return ProviderVoyage
}

func (c *VoyageClient) Model() string {
	return c.model
}

func (c *VoyageClient) Dimension() int {
	return c.dimension
}

func (c *VoyageClient) Embed(ctx context.Context, input []string, inputType string) (*Result, error) {
	if c.apiKey == "" {
		return nil, fmt.Errorf("voyage api key is not configured")
	}
	if len(input) == 0 {
		return &Result{Model: c.model, Dimension: c.dimension}, nil
	}
	if inputType == "" {
		inputType = InputTypeDocument
	}

	payload := voyageRequest{
		Input:           input,
		Model:           c.model,
		InputType:       inputType,
		OutputDimension: c.dimension,
		Truncation:      true,
		OutputDType:     "float",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal voyage request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, voyageEmbeddingsURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create voyage request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("voyage request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var apiErr voyageError
		_ = json.NewDecoder(resp.Body).Decode(&apiErr)
		if apiErr.Detail != "" {
			return nil, fmt.Errorf("voyage api error %d: %s", resp.StatusCode, apiErr.Detail)
		}
		return nil, fmt.Errorf("voyage api error: status %d", resp.StatusCode)
	}

	var out voyageResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode voyage response: %w", err)
	}

	result := &Result{
		Embeddings: make([][]float32, len(out.Data)),
		Tokens:     out.Usage.TotalTokens,
		Model:      c.model,
		Dimension:  c.dimension,
	}
	for _, item := range out.Data {
		if item.Index < 0 || item.Index >= len(result.Embeddings) {
			return nil, fmt.Errorf("voyage response index out of range: %d", item.Index)
		}
		if len(item.Embedding) != c.dimension {
			return nil, fmt.Errorf("voyage embedding dimension mismatch: got %d, want %d", len(item.Embedding), c.dimension)
		}
		result.Embeddings[item.Index] = item.Embedding
	}

	return result, nil
}

type voyageRequest struct {
	Input           []string `json:"input"`
	Model           string   `json:"model"`
	InputType       string   `json:"input_type,omitempty"`
	OutputDimension int      `json:"output_dimension,omitempty"`
	Truncation      bool     `json:"truncation"`
	OutputDType     string   `json:"output_dtype,omitempty"`
}

type voyageResponse struct {
	Data  []voyageEmbedding `json:"data"`
	Usage voyageUsage       `json:"usage"`
}

type voyageEmbedding struct {
	Embedding []float32 `json:"embedding"`
	Index     int       `json:"index"`
}

type voyageUsage struct {
	TotalTokens int `json:"total_tokens"`
}

type voyageError struct {
	Detail string `json:"detail"`
}
