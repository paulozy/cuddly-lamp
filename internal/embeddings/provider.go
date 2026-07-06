package embeddings

import "context"

const (
	ProviderVoyage = "voyage"

	InputTypeDocument = "document"
	InputTypeQuery    = "query"

	// DefaultDimension is the fixed embedding dimension used across the system.
	// It is pinned to the `code_embeddings.embedding` pgvector column type
	// (VECTOR(1024), migration 007) — changing it would break inserts, so it is
	// not configurable per organization.
	DefaultDimension = 1024
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
