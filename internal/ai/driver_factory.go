package ai

import (
	"time"

	"github.com/binoctal/cerberus/internal/llm"
)

// NewDriver creates a new driver with default retry config.
func NewDriver(client llm.Client, budget *TokenBudget) *Driver {
	return &Driver{
		client: client,
		budget: budget,
		retry:  DefaultRetryConfig(),
		cache:  NewResponseCache(5 * time.Minute),
	}
}

// NewDriverWithRetry creates a driver with custom retry config.
func NewDriverWithRetry(client llm.Client, budget *TokenBudget, retry RetryConfig) *Driver {
	return &Driver{
		client: client,
		budget: budget,
		retry:  retry,
		cache:  NewResponseCache(5 * time.Minute),
	}
}
