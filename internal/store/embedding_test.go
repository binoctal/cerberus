package store

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCosineSimilarity(t *testing.T) {
	tests := []struct {
		name string
		a, b []float64
		want float64
	}{
		{"identical", []float64{1, 0, 0}, []float64{1, 0, 0}, 1.0},
		{"opposite", []float64{1, 0, 0}, []float64{-1, 0, 0}, -1.0},
		{"orthogonal", []float64{1, 0, 0}, []float64{0, 1, 0}, 0.0},
		{"45deg", []float64{1, 1, 0}, []float64{1, 0, 0}, math.Sqrt(2) / 2},
		{"zero_a", []float64{0, 0, 0}, []float64{1, 0, 0}, 0.0},
		{"different_len", []float64{1, 0}, []float64{1, 0, 0}, 0.0},
		{"empty", nil, nil, 0.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CosineSimilarity(tt.a, tt.b)
			assert.InDelta(t, tt.want, got, 1e-9)
		})
	}
}

func TestParseFormatEmbedding(t *testing.T) {
	original := []float64{0.1, -0.2, 0.3, 0.0}

	s := FormatEmbedding(original)
	parsed, err := ParseEmbedding(s)
	assert.NoError(t, err)
	assert.InDeltaSlice(t, original, parsed, 1e-9)

	// Empty cases.
	empty1, err := ParseEmbedding("")
	assert.NoError(t, err)
	assert.Nil(t, empty1)

	empty2, err := ParseEmbedding("[]")
	assert.NoError(t, err)
	assert.Nil(t, empty2)
}

func TestFormatEmbedding_Empty(t *testing.T) {
	assert.Equal(t, "[]", FormatEmbedding(nil))
	assert.Equal(t, "[]", FormatEmbedding([]float64{}))
}
