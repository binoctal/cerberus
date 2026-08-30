package llm

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewRateLimitError pins the run37 shape: anthropic-proxy code 1308 body
// with a Chinese quota message carrying a rendered reset timestamp, plus the
// legacy plain-429 body without one.
func TestNewRateLimitError(t *testing.T) {
	rle := NewRateLimitError("anthropic api", 429,
		`{"type":"error","error":{"type":"rate_limit_error","code":"1308","message":"[1308][已达到 5 小时的使用上限。您的限额将在 2026-08-30 13:00:23 重置。][20260830125750d9b434fc29534b3f]"}}`)
	require.False(t, rle.ResetAt.IsZero(), "reset timestamp must be parsed")
	want, err := time.ParseInLocation("2006-01-02 15:04:05", "2026-08-30 13:00:23", time.Local)
	require.NoError(t, err)
	assert.Equal(t, want, rle.ResetAt)
	assert.Contains(t, rle.Error(), "anthropic api error 429", "error text stays log-compatible")

	plain := NewRateLimitError("anthropic stream", 429, `{"type":"error","error":{"type":"rate_limit_error"}}`)
	assert.True(t, plain.ResetAt.IsZero(), "no timestamp in body → zero reset")
	assert.Contains(t, plain.Error(), "anthropic stream error 429")
}
