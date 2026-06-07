package ai

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/binoctal/cerberus/internal/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTokenBudget(t *testing.T) {
	b := NewTokenBudget(100000, 10000)
	assert.Equal(t, 100000, b.SessionTotal)
	assert.Equal(t, 100000, b.Remaining())

	b.Record(30000)
	assert.Equal(t, 70000, b.Remaining())
	assert.False(t, b.Exhausted())

	b.Record(70000)
	assert.True(t, b.Exhausted())
}

func TestTokenBudgetCanSpend(t *testing.T) {
	b := NewTokenBudget(100000, 10000)
	assert.True(t, b.CanSpend(5000))
	assert.True(t, b.CanSpend(10000))
	assert.False(t, b.CanSpend(10001))
}

func TestPromptBuilder(t *testing.T) {
	prompt := NewPrompt().
		System("You are a test judge.").
		Task("Evaluate this evidence: status code 200").
		Output("JSON with status and confidence fields").
		Build()

	assert.Contains(t, prompt, "You are a test judge.")
	assert.Contains(t, prompt, "Evaluate this evidence")
	assert.Contains(t, prompt, "JSON with status")
}

func TestContextInjection(t *testing.T) {
	entries := []ContextEntry{
		{Source: "memory", Content: "Last test found 500 error", Relevance: 0.9},
		{Source: "code", Content: "Endpoint: POST /api/v1/users", Relevance: 0.8},
	}
	ctx := BuildContext(entries)
	assert.Contains(t, ctx, "Last test found 500 error")
	assert.Contains(t, ctx, "POST /api/v1/users")
}

func TestParseStructuredOutput(t *testing.T) {
	type TestResult struct {
		Status     string  `json:"status"`
		Confidence float64 `json:"confidence"`
	}

	input := `Here is my analysis:
` + "```" + `json
{"status": "pass", "confidence": 0.95}
` + "```" + `
The test passed.`

	var result TestResult
	err := ParseStructuredOutput(input, &result)
	require.NoError(t, err)
	assert.Equal(t, "pass", result.Status)
	assert.InDelta(t, 0.95, result.Confidence, 0.01)
}

func TestParseStructuredOutputDirect(t *testing.T) {
	type Result struct{ Status string `json:"status"` }
	var r Result
	err := ParseStructuredOutput(`{"status":"fail"}`, &r)
	require.NoError(t, err)
	assert.Equal(t, "fail", r.Status)
}

func TestDriverDecide(t *testing.T) {
	mockClient := llm.NewMockClient(map[string]string{
		"default": `{"verdict":"pass","confidence":0.9,"reasoning":"looks good"}`,
	})

	driver := NewDriver(mockClient, NewTokenBudget(200000, 10000))

	type Verdict struct {
		Verdict    string  `json:"verdict"`
		Confidence float64 `json:"confidence"`
		Reasoning  string  `json:"reasoning"`
	}

	var v Verdict
	err := driver.Decide(context.Background(),
		NewPrompt().System("judge").Task("evaluate").Output("JSON verdict").Build(),
		&v,
	)
	require.NoError(t, err)
	assert.Equal(t, "pass", v.Verdict)
	assert.Equal(t, "looks good", v.Reasoning)

	assert.Less(t, driver.Budget().Remaining(), 200000)
}

// --- Retry tests ---

func TestDriver_RetryOn500(t *testing.T) {
	callCount := 0
	client := &retryTestClient{
		fn: func(ctx context.Context, req llm.Request) (*llm.Response, error) {
			callCount++
			if callCount < 3 {
				return nil, fmt.Errorf("server error: 500 Internal Server Error")
			}
			return &llm.Response{
				Content: `{"status":"pass"}`,
				Usage:   llm.TokenUsage{TotalTokens: 100},
			}, nil
		},
	}

	retry := RetryConfig{MaxRetries: 3, BaseDelay: 1 * time.Millisecond, MaxDelay: 10 * time.Millisecond}
	driver := NewDriverWithRetry(client, NewTokenBudget(200000, 10000), retry)

	type Result struct{ Status string `json:"status"` }
	var r Result
	err := driver.Decide(context.Background(), "test", &r)
	require.NoError(t, err)
	assert.Equal(t, "pass", r.Status)
	assert.Equal(t, 3, callCount, "should retry twice then succeed")
}

func TestDriver_RetryOn429(t *testing.T) {
	callCount := 0
	client := &retryTestClient{
		fn: func(ctx context.Context, req llm.Request) (*llm.Response, error) {
			callCount++
			if callCount < 2 {
				return nil, fmt.Errorf("429 rate limit exceeded")
			}
			return &llm.Response{
				Content: `{"status":"pass"}`,
				Usage:   llm.TokenUsage{TotalTokens: 100},
			}, nil
		},
	}

	retry := RetryConfig{MaxRetries: 3, BaseDelay: 1 * time.Millisecond, MaxDelay: 10 * time.Millisecond}
	driver := NewDriverWithRetry(client, NewTokenBudget(200000, 10000), retry)

	var r struct{ Status string `json:"status"` }
	err := driver.Decide(context.Background(), "test", &r)
	require.NoError(t, err)
	assert.Equal(t, 2, callCount)
}

func TestDriver_NoRetryOnParseError(t *testing.T) {
	callCount := 0
	client := &retryTestClient{
		fn: func(ctx context.Context, req llm.Request) (*llm.Response, error) {
			callCount++
			return &llm.Response{
				Content: "not valid json",
				Usage:   llm.TokenUsage{TotalTokens: 50},
			}, nil
		},
	}

	retry := RetryConfig{MaxRetries: 3, BaseDelay: 1 * time.Millisecond, MaxDelay: 10 * time.Millisecond}
	driver := NewDriverWithRetry(client, NewTokenBudget(200000, 10000), retry)

	var r struct{ Status string `json:"status"` }
	err := driver.Decide(context.Background(), "test", &r)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse output")
	assert.Equal(t, 1, callCount, "should NOT retry on parse errors")
}

func TestDriver_NoRetryOnNonTransient(t *testing.T) {
	callCount := 0
	client := &retryTestClient{
		fn: func(ctx context.Context, req llm.Request) (*llm.Response, error) {
			callCount++
			return nil, fmt.Errorf("authentication failed: invalid API key")
		},
	}

	retry := RetryConfig{MaxRetries: 3, BaseDelay: 1 * time.Millisecond, MaxDelay: 10 * time.Millisecond}
	driver := NewDriverWithRetry(client, NewTokenBudget(200000, 10000), retry)

	var r struct{ Status string `json:"status"` }
	err := driver.Decide(context.Background(), "test", &r)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "authentication failed")
	assert.Equal(t, 1, callCount, "should NOT retry on auth errors")
}

func TestDriver_ExhaustedRetries(t *testing.T) {
	client := &retryTestClient{
		fn: func(ctx context.Context, req llm.Request) (*llm.Response, error) {
			return nil, fmt.Errorf("503 service unavailable")
		},
	}

	retry := RetryConfig{MaxRetries: 2, BaseDelay: 1 * time.Millisecond, MaxDelay: 10 * time.Millisecond}
	driver := NewDriverWithRetry(client, NewTokenBudget(200000, 10000), retry)

	var r struct{ Status string `json:"status"` }
	err := driver.Decide(context.Background(), "test", &r)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed after 2 retries")
}

func TestDriver_RetryCancelledByContext(t *testing.T) {
	client := &retryTestClient{
		fn: func(ctx context.Context, req llm.Request) (*llm.Response, error) {
			return nil, fmt.Errorf("503 service unavailable")
		},
	}

	retry := RetryConfig{MaxRetries: 10, BaseDelay: 5 * time.Second, MaxDelay: 30 * time.Second}
	driver := NewDriverWithRetry(client, NewTokenBudget(200000, 10000), retry)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	var r struct{ Status string `json:"status"` }
	err := driver.Decide(ctx, "test", &r)
	require.Error(t, err)
	// Should exit early via context cancellation, not wait for full retry.
}

func TestBackoff(t *testing.T) {
	retry := RetryConfig{MaxRetries: 5, BaseDelay: 100 * time.Millisecond, MaxDelay: 2 * time.Second}
	d := &Driver{retry: retry}

	assert.Equal(t, 100*time.Millisecond, d.backoff(1))
	assert.Equal(t, 200*time.Millisecond, d.backoff(2))
	assert.Equal(t, 400*time.Millisecond, d.backoff(3))
	assert.Equal(t, 800*time.Millisecond, d.backoff(4))
	assert.Equal(t, 1600*time.Millisecond, d.backoff(5))
	assert.Equal(t, 2*time.Second, d.backoff(6), "should cap at maxDelay")
}

func TestIsRetryable(t *testing.T) {
	tests := []struct {
		errMsg     string
		retryable  bool
	}{
		{"500 Internal Server Error", true},
		{"502 Bad Gateway", true},
		{"503 service unavailable", true},
		{"504 Gateway Timeout", true},
		{"429 rate limit exceeded", true},
		{"timeout: context deadline exceeded", true},
		{"connection refused", true},
		{"overloaded: API is at capacity", true},
		{"authentication failed: invalid API key", false},
		{"model not found", false},
		{"invalid request: malformed JSON", false},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.retryable, isRetryable(fmt.Errorf("%s", tt.errMsg)), tt.errMsg)
	}
}

// retryTestClient is a test LLM client with a configurable function.
type retryTestClient struct {
	fn func(ctx context.Context, req llm.Request) (*llm.Response, error)
}

func (c *retryTestClient) Complete(ctx context.Context, req llm.Request) (*llm.Response, error) {
	return c.fn(ctx, req)
}

func (c *retryTestClient) CompleteWithVision(ctx context.Context, prompt string, images [][]byte) (*llm.Response, error) {
	return c.fn(ctx, llm.Request{Messages: []llm.Message{{Role: "user", Content: prompt}}})
}
