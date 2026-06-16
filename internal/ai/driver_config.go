package ai

import (
	"github.com/binoctal/cerberus/internal/llm"
)

// SetCache replaces the default cache. Pass nil to disable caching.
func (d *Driver) SetCache(c *ResponseCache) {
	d.cache = c
}

// Budget returns the token budget for this driver.
func (d *Driver) Budget() *TokenBudget {
	return d.budget
}

// Client returns the underlying LLM client for direct access (e.g., raw completion fallback).
func (d *Driver) Client() llm.Client {
	return d.client
}
