package ai

import (
	"fmt"
	"testing"
)

func TestIsRetryableStatusBasedNotBodySubstring(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"500 retryable", fmt.Errorf("anthropic api error 500: internal"), true},
		{"429 retryable", fmt.Errorf("anthropic api error 429: rate limit"), true},
		{"503 retryable", fmt.Errorf("openai api error 503: overloaded"), true},
		{"400 not retryable even if body has 500", fmt.Errorf(`anthropic api error 400: {"request_id":"50012"}`), false},
		{"401 not retryable even if body says timeout", fmt.Errorf("anthropic api error 401: token timeout"), false},
		{"400 not retryable even if body says capacity", fmt.Errorf("gemini api error 400: account capacity exceeded"), false},
		{"network deadline retryable", fmt.Errorf("context deadline exceeded"), true},
		{"connection refused retryable", fmt.Errorf("dial tcp: connection refused"), true},
		{"generic error not retryable", fmt.Errorf("invalid model"), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isRetryable(tc.err); got != tc.want {
				t.Errorf("isRetryable(%q) = %v, want %v", tc.err.Error(), got, tc.want)
			}
		})
	}
}
