package embeddings

import "context"

const (
	ProviderVoyage = "voyage"

	InputTypeDocument = "document"
	InputTypeQuery    = "query"
)

type Provider interface {
	Embed(ctx context.Context, input []string, inputType string) (*Result, error)
	Provider() string
	Model() string
	Dimension() int
}

type Result struct {
	Embeddings [][]float32
	Tokens     int
	Model      string
	Dimension  int
}
