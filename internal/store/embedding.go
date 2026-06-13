package store

import (
	"encoding/json"
	"math"
)

// CosineSimilarity computes the cosine similarity between two vectors.
// Returns 0 if either vector has zero magnitude.
func CosineSimilarity(a, b []float64) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

// ParseEmbedding deserializes a JSON string to a float64 slice.
func ParseEmbedding(s string) ([]float64, error) {
	if s == "" || s == "[]" {
		return nil, nil
	}
	var v []float64
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return nil, err
	}
	return v, nil
}

// FormatEmbedding serializes a float64 slice to a JSON string.
func FormatEmbedding(v []float64) string {
	if len(v) == 0 {
		return "[]"
	}
	b, _ := json.Marshal(v)
	return string(b)
}
