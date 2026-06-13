package embed

import (
	"context"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGenerate_Deterministic(t *testing.T) {
	text := "The quick brown fox jumps over the lazy dog"
	v1 := Generate(text, 64)
	v2 := Generate(text, 64)
	assert.Equal(t, v1, v2, "same input must produce identical embedding")
}

func TestGenerate_Dimension(t *testing.T) {
	text := "hello world"
	assert.Len(t, Generate(text, 32), 32)
	assert.Len(t, Generate(text, 128), 128)
	assert.Len(t, Generate(text, 256), 256)
}

func TestGenerate_Normalized(t *testing.T) {
	text := "some test content for normalization check"
	vec := Generate(text, 64)

	// L2 norm should be ~1.0.
	var norm float64
	for _, v := range vec {
		norm += v * v
	}
	norm = math.Sqrt(norm)
	assert.InDelta(t, 1.0, norm, 1e-9, "embedding should be L2-normalized")
}

func TestGenerate_DefaultDimension(t *testing.T) {
	vec := Generate("test", 0)
	assert.Len(t, vec, 128, "dim=0 should default to 128")
}

func TestGenerate_SimilarContent(t *testing.T) {
	// Similar content should have higher similarity than dissimilar.
	sim1 := Generate("POST /api/users creates a new user", 64)
	sim2 := Generate("POST /api/users creates new user account", 64)
	diff := Generate("The weather is sunny and warm today", 64)

	scoreSimilar := cosine(sim1, sim2)
	scoreDiff := cosine(sim1, diff)

	assert.Greater(t, scoreSimilar, scoreDiff,
		"semantically similar texts should have higher cosine similarity")
}

func TestGenerate_EmptyInput(t *testing.T) {
	vec := Generate("", 64)
	// Zero vector (no trigrams).
	var sum float64
	for _, v := range vec {
		sum += v
	}
	assert.Equal(t, 0.0, sum, "empty input should produce zero vector")
}

// cosine is a test helper.
func cosine(a, b []float64) float64 {
	var dot, na, nb float64
	for i := range a {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

func TestTrigramProvider_ImplementsInterface(t *testing.T) {
	// Compile-time check.
	var _ Provider = (*TrigramProvider)(nil)
}

func TestTrigramProvider_Embed(t *testing.T) {
	p := NewTrigramProvider(64)
	ctx := context.Background()

	vec, err := p.Embed(ctx, "hello world")
	assert.NoError(t, err)
	assert.Len(t, vec, 64)
	var n float64
	for _, v := range vec {
		n += v * v
	}
	assert.InDelta(t, 1.0, math.Sqrt(n), 1e-9)
}

func TestTrigramProvider_DefaultDim(t *testing.T) {
	p := NewTrigramProvider(0)
	assert.Equal(t, DefaultDimension, p.Dimension())
	assert.Equal(t, DefaultDimension, p.Dimension())
}

func TestTrigramProvider_NegativeDim(t *testing.T) {
	p := NewTrigramProvider(-1)
	assert.Equal(t, DefaultDimension, p.Dimension())
}

func TestTrigramProvider_ModelName(t *testing.T) {
	p := NewTrigramProvider(128)
	assert.Equal(t, "trigram-v1", p.ModelName())
}

func TestTrigramProvider_Deterministic(t *testing.T) {
	p := NewTrigramProvider(64)
	ctx := context.Background()

	v1, _ := p.Embed(ctx, "test input")
	v2, _ := p.Embed(ctx, "test input")
	for i := range v1 {
		assert.InDelta(t, v1[i], v2[i], 1e-15)
	}
}

func TestTrigramProvider_MatchesGenerate(t *testing.T) {
	// TrigramProvider.Embed should produce identical output to Generate.
	p := NewTrigramProvider(128)
	ctx := context.Background()

	text := "check provider matches generate"
	vec, _ := p.Embed(ctx, text)
	expected := Generate(text, 128)
	for i := range vec {
		assert.InDelta(t, vec[i], expected[i], 1e-15)
	}
}
