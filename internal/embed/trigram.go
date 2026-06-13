package embed

import "context"

// DefaultDimension is the default embedding vector dimensionality.
const DefaultDimension = 128

// TrigramProvider generates embeddings using local character-trigram hashing.
// No API calls required — deterministic and fast.
type TrigramProvider struct {
	dim int
}

// NewTrigramProvider creates a TrigramProvider with the given dimension.
// If dim <= 0, DefaultDimension (128) is used.
func NewTrigramProvider(dim int) *TrigramProvider {
	if dim <= 0 {
		dim = DefaultDimension
	}
	return &TrigramProvider{dim: dim}
}

// Embed generates a deterministic embedding vector from the input text.
func (p *TrigramProvider) Embed(_ context.Context, text string) ([]float64, error) {
	return Generate(text, p.dim), nil
}

// Dimension returns the configured dimensionality.
func (p *TrigramProvider) Dimension() int { return p.dim }

// ModelName returns the provider identifier.
func (p *TrigramProvider) ModelName() string { return "trigram-v1" }
