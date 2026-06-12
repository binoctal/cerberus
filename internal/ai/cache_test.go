package ai

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestResponseCache_SetGet(t *testing.T) {
	c := NewResponseCache(5 * time.Minute)

	// Miss.
	_, _, ok := c.Get("hello")
	assert.False(t, ok)

	// Set + hit.
	c.Set("hello", `{"ok":true}`, TokenUsage{TotalTokens: 100})
	content, usage, ok := c.Get("hello")
	assert.True(t, ok)
	assert.Equal(t, `{"ok":true}`, content)
	assert.Equal(t, 100, usage.TotalTokens)
}

func TestResponseCache_TTLExpiration(t *testing.T) {
	c := NewResponseCache(50 * time.Millisecond)

	c.Set("prompt1", "response1", TokenUsage{TotalTokens: 50})

	// Immediate hit.
	_, _, ok := c.Get("prompt1")
	assert.True(t, ok)

	// Wait for TTL.
	time.Sleep(80 * time.Millisecond)

	_, _, ok = c.Get("prompt1")
	assert.False(t, ok, "entry should be expired")
}

func TestResponseCache_ZeroTTL(t *testing.T) {
	c := NewResponseCache(0) // No expiration.

	c.Set("prompt", "response", TokenUsage{TotalTokens: 10})
	time.Sleep(20 * time.Millisecond)

	_, _, ok := c.Get("prompt")
	assert.True(t, ok, "zero TTL means no expiration")
}

func TestResponseCache_DifferentPrompts(t *testing.T) {
	c := NewResponseCache(5 * time.Minute)

	c.Set("prompt A", "response A", TokenUsage{TotalTokens: 10})
	c.Set("prompt B", "response B", TokenUsage{TotalTokens: 20})

	contentA, _, okA := c.Get("prompt A")
	contentB, _, okB := c.Get("prompt B")

	assert.True(t, okA)
	assert.True(t, okB)
	assert.Equal(t, "response A", contentA)
	assert.Equal(t, "response B", contentB)
}

func TestResponseCache_Eviction(t *testing.T) {
	c := NewResponseCache(5 * time.Minute)

	// Fill cache beyond eviction threshold.
	for i := 0; i < 300; i++ {
		c.Set("prompt"+string(rune(i)), "response", TokenUsage{})
	}

	// Cache should still work after eviction check.
	assert.Equal(t, 300, c.Len())

	// Set one more to trigger eviction.
	c.Set("trigger", "eviction", TokenUsage{})
	// Entries exist (not all evicted since none are expired with 5-min TTL).
	assert.True(t, c.Len() > 0)
}
