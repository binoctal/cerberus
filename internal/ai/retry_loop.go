package ai

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
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
			// Compute backoff delay with jitter to avoid retry thundering
			// herds under rate limiting.
			delay := jitteredDelay(computeBackoff(attempt, baseDelay, maxDelay))
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

// withJitter applies "equal jitter" to a backoff delay: at least half the
// delay, plus a random fraction of the remaining half. This spreads
// concurrent retries so they don't all wake at the same instant and re-trip
// the rate limit (thundering herd). A nil rand or non-positive delay returns
// the delay untouched.
func withJitter(d time.Duration, r *rand.Rand) time.Duration {
	if r == nil || d <= 0 {
		return d
	}
	half := int64(d) / 2
	if half <= 0 {
		return d
	}
	return time.Duration(half + r.Int63n(half))
}

// jitterR is guarded by jitterMu because *rand.Rand is not safe for
// concurrent use — executeWithRetry runs in parallel across heads and the
// examiner judge loop.
var (
	jitterMu sync.Mutex
	jitterR  = rand.New(rand.NewSource(time.Now().UnixNano()))
)

// jitteredDelay applies withJitter via the package-level concurrent-safe rand.
func jitteredDelay(d time.Duration) time.Duration {
	jitterMu.Lock()
	defer jitterMu.Unlock()
	return withJitter(d, jitterR)
}
