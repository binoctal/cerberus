package ai

import (
	"crypto/sha256"
	"fmt"
	"sync"
	"time"
)

// cacheEntry holds a cached LLM response with expiration.
type cacheEntry struct {
	content  string
	usage    TokenUsage
	createdAt time.Time
}

// TokenUsage records token counts for a cached response.
type TokenUsage struct {
	TotalTokens int
}

// ResponseCache provides an in-memory LLM response cache keyed by prompt hash.
type ResponseCache struct {
	mu      sync.RWMutex
	entries map[string]cacheEntry
	ttl     time.Duration
}

// NewResponseCache creates a cache with the given TTL. Zero means no expiration.
func NewResponseCache(ttl time.Duration) *ResponseCache {
	return &ResponseCache{
		entries: make(map[string]cacheEntry),
		ttl:     ttl,
	}
}

// cacheKey computes a SHA-256 hash of the prompt for use as cache key.
func cacheKey(prompt string) string {
	h := sha256.Sum256([]byte(prompt))
	return fmt.Sprintf("%x", h)
}

// Get returns the cached response and true if found and not expired.
func (c *ResponseCache) Get(prompt string) (string, TokenUsage, bool) {
	key := cacheKey(prompt)

	c.mu.RLock()
	entry, ok := c.entries[key]
	c.mu.RUnlock()

	if !ok {
		return "", TokenUsage{}, false
	}

	if c.ttl > 0 && time.Since(entry.createdAt) > c.ttl {
		c.mu.Lock()
		delete(c.entries, key)
		c.mu.Unlock()
		return "", TokenUsage{}, false
	}

	return entry.content, entry.usage, true
}

// Set stores a response in the cache.
func (c *ResponseCache) Set(prompt, content string, usage TokenUsage) {
	key := cacheKey(prompt)

	c.mu.Lock()
	defer c.mu.Unlock()

	// Lazy eviction: remove expired entries when cache grows.
	if len(c.entries) > 256 {
		c.evictLocked()
	}

	c.entries[key] = cacheEntry{
		content:   content,
		usage:     usage,
		createdAt: time.Now(),
	}
}

// evictLocked removes expired entries. Caller must hold c.mu.
func (c *ResponseCache) evictLocked() {
	if c.ttl <= 0 {
		return
	}
	now := time.Now()
	for k, v := range c.entries {
		if now.Sub(v.createdAt) > c.ttl {
			delete(c.entries, k)
		}
	}
}

// Len returns the number of cached entries.
func (c *ResponseCache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}
