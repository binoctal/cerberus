// Package embed provides local, deterministic embedding generation
// using character-trigram hashing. No API calls required.
package embed

import (
	"math"
	"strings"
	"unicode"
)

// Generate produces a deterministic embedding vector from the input text.
// It uses character trigram hashing: each 3-character substring is hashed
// to a bucket index, and buckets are accumulated with ±1 based on sign hash.
// The result is L2-normalized to unit length.
func Generate(content string, dim int) []float64 {
	if dim <= 0 {
		dim = 128
	}
	vec := make([]float64, dim)

	// Normalize: lowercase, collapse whitespace.
	text := strings.ToLower(content)
	text = collapseWhitespace(text)

	// Extract character trigrams and hash into vector.
	for i := 0; i+2 < len(text); i++ {
		trigram := text[i : i+3]
		h := fnvHash(trigram)
		idx := int(h % uint64(dim))
		sign := float64(1)
		if (h>>16)%2 == 1 {
			sign = -1
		}
		vec[idx] += sign
	}

	// L2 normalize.
	norm := 0.0
	for _, v := range vec {
		norm += v * v
	}
	if norm > 0 {
		norm = math.Sqrt(norm)
		for i := range vec {
			vec[i] /= norm
		}
	}

	return vec
}

// collapseWhitespace replaces sequences of whitespace with a single space.
func collapseWhitespace(s string) string {
	var b strings.Builder
	prevSpace := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			if !prevSpace {
				b.WriteByte(' ')
				prevSpace = true
			}
		} else {
			b.WriteRune(r)
			prevSpace = false
		}
	}
	return b.String()
}

// fnvHash is a simple FNV-1a 64-bit hash for strings.
func fnvHash(s string) uint64 {
	h := uint64(14695981039346656037)
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= 1099511628211
	}
	return h
}
