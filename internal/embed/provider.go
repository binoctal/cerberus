package embed

import "context"

// Provider is the interface for embedding generation.
// Implementations may call external APIs or use local algorithms.
type Provider interface {
	// Embed generates an embedding vector for the given text.
	Embed(ctx context.Context, text string) ([]float64, error)
	// Dimension returns the dimensionality of the embedding vectors.
	Dimension() int
	// ModelName returns a human-readable identifier for the model.
	ModelName() string
}
