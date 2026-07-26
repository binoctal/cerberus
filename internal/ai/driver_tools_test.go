package ai

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/llm"
)

// TestDriver_DecideWithTools_RetriesTransientErrors is the S4 Task 0 regression
// test: every Scout/Agent site migrated in S2/S3 lost transient retry because
// DecideWithTools was a bare client.Complete. After Task 0 wraps the call in
// executeWithRetry, a mock that fails N-1 times then succeeds must surface the
// success result (not the first error).
func TestDriver_DecideWithTools_RetriesTransientErrors(t *testing.T) {
	callCount := 0
	client := &retryTestClient{
		fn: func(ctx context.Context, req llm.Request) (*llm.Response, error) {
			callCount++
			if callCount < 3 {
				return nil, fmt.Errorf("server error: 500 Internal Server Error")
			}
			return &llm.Response{
				Content: "ok",
				ToolCalls: []llm.ToolCall{
					{ID: "call_1", Name: "ws_relay", Input: map[string]any{"roles": []any{"web"}}},
				},
				Usage: llm.TokenUsage{TotalTokens: 100},
			}, nil
		},
	}

	retry := RetryConfig{MaxRetries: 3, BaseDelay: 1 * time.Millisecond, MaxDelay: 10 * time.Millisecond}
	driver := NewDriverWithRetry(client, NewTokenBudget(200000, 10000), retry)

	res, err := driver.DecideWithTools(context.Background(), "test the API", []llm.Tool{
		{Name: "ws_relay", Description: "emit a relay intent", InputSchema: map[string]any{"type": "object"}},
	})
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, "ok", res.Content)
	if len(res.ToolCalls) != 1 || res.ToolCalls[0].Name != "ws_relay" {
		t.Fatalf("ToolCalls = %+v, want one ws_relay", res.ToolCalls)
	}
	assert.Equal(t, 100, res.Usage.TotalTokens)
	assert.Equal(t, 3, callCount, "should retry twice then succeed")
	// Budget must be recorded from the successful attempt, not the failed ones.
	assert.Less(t, driver.Budget().Remaining(), 200000)
}

// TestDriver_DecideWithTools_NoRetryOnNonTransient confirms auth/class errors
// still fail fast (parity with Decide's TestDriver_NoRetryOnNonTransient).
func TestDriver_DecideWithTools_NoRetryOnNonTransient(t *testing.T) {
	callCount := 0
	client := &retryTestClient{
		fn: func(ctx context.Context, req llm.Request) (*llm.Response, error) {
			callCount++
			return nil, fmt.Errorf("authentication failed: invalid API key")
		},
	}

	retry := RetryConfig{MaxRetries: 3, BaseDelay: 1 * time.Millisecond, MaxDelay: 10 * time.Millisecond}
	driver := NewDriverWithRetry(client, NewTokenBudget(200000, 10000), retry)

	_, err := driver.DecideWithTools(context.Background(), "test", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "authentication failed")
	assert.Equal(t, 1, callCount, "should NOT retry on auth errors")
}

// TestDriver_DecideWithTools_ExhaustedRetries confirms transient errors that
// never recover bubble up after MaxRetries (parity with Decide).
func TestDriver_DecideWithTools_ExhaustedRetries(t *testing.T) {
	client := &retryTestClient{
		fn: func(ctx context.Context, req llm.Request) (*llm.Response, error) {
			return nil, fmt.Errorf("503 service unavailable")
		},
	}

	retry := RetryConfig{MaxRetries: 2, BaseDelay: 1 * time.Millisecond, MaxDelay: 10 * time.Millisecond}
	driver := NewDriverWithRetry(client, NewTokenBudget(200000, 10000), retry)

	_, err := driver.DecideWithTools(context.Background(), "test", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed after 2 retries")
}
