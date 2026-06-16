package ai

import (
	"context"
	"fmt"
	"time"
)

// retryExecutor defines the function signature for the operation to retry.
type retryExecutor func(ctx context.Context) (int, error)

// executeWithRetry runs an operation with exponential backoff retry logic.
// The executor function should return (tokensUsed, error).
// Returns the final error if all retries are exhausted.
func executeWithRetry(
	ctx context.Context,
	maxRetries int,
	baseDelay, maxDelay time.Duration,
	executor retryExecutor,
) (int, error) {
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			// Compute backoff delay
			delay := computeBackoff(attempt, baseDelay, maxDelay)
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return 0, ctx.Err()
			}
		}

		// Execute the operation
		tokens, err := executor(ctx)
		if err != nil {
			lastErr = err
			if !isRetryable(err) {
				return 0, err
			}
			continue // Retry on transient errors
		}

		return tokens, nil // Success
	}

	return 0, fmt.Errorf("operation failed after %d retries: %w", maxRetries, lastErr)
}

// computeBackoff calculates exponential backoff with jitter: base * 2^(attempt-1), capped at maxDelay.
func computeBackoff(attempt int, baseDelay, maxDelay time.Duration) time.Duration {
	if attempt <= 1 {
		return baseDelay
	}

	// Calculate exponential backoff
	delay := baseDelay * time.Duration(1<<uint(attempt-1))
	if delay > maxDelay {
		delay = maxDelay
	}
	return delay
}
