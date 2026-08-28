package agent

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/types"
)

// TestRetryAfterWait pins the Retry-After parse-and-clamp the 429 backoff
// relies on: header seconds, floor for a missing/unparseable header, hard
// ceiling so a hostile/huge value can never stall a run for minutes.
func TestRetryAfterWait(t *testing.T) {
	resp := func(v string) *http.Response {
		r := &http.Response{Header: http.Header{}}
		if v != "" {
			r.Header.Set("Retry-After", v)
		}
		return r
	}

	require.Equal(t, 5*time.Second, retryAfterWait(resp("5")))
	require.Equal(t, 250*time.Millisecond, retryAfterWait(resp("0")),
		"a zero Retry-After still backs off a little — the window needs time to drain")
	require.Equal(t, defaultRetryAfterWait, retryAfterWait(resp("")),
		"missing Retry-After falls back to the default wait")
	require.Equal(t, defaultRetryAfterWait, retryAfterWait(resp("soon")),
		"unparseable Retry-After falls back to the default wait")
	require.Equal(t, maxRetryAfterWait, retryAfterWait(resp("300")),
		"a huge Retry-After is clamped to the ceiling")
}

// TestHTTPExecutor429Retries: a 429 with Retry-After is retried after the
// backoff — the rate limiter working as designed must not become a case
// failure when the window resets within reach.
func TestHTTPExecutor429Retries(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if hits.Add(1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"code":"RATE_LIMIT_EXCEEDED"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)

	e := NewHTTPExecutor(zap.NewNop())
	res := e.Execute(context.Background(), types.HTTPAction{Method: "GET", URL: srv.URL})

	hr, ok := res.(types.HTTPResult)
	require.True(t, ok, "http action yields HTTPResult")
	require.Equal(t, 200, hr.StatusCode, "the 429 must be retried to success")
	require.Equal(t, int32(2), hits.Load(), "exactly one retry")
}

// TestHTTPExecutor429Cooldown: after a 429, the per-host cool-down paces the
// NEXT request too — without it, the request behind the retried one walks
// straight back into the same limiter window.
func TestHTTPExecutor429Cooldown(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := hits.Add(1)
		if n <= 2 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"code":"RATE_LIMIT_EXCEEDED"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)

	e := NewHTTPExecutor(zap.NewNop())
	start := time.Now()
	for i := 0; i < 2; i++ {
		res := e.Execute(context.Background(), types.HTTPAction{Method: "GET", URL: srv.URL})
		hr, ok := res.(types.HTTPResult)
		require.True(t, ok)
		require.Equal(t, 200, hr.StatusCode, fmt.Sprintf("request %d must eventually succeed", i+1))
	}
	elapsed := time.Since(start)
	require.GreaterOrEqual(t, elapsed, 2*time.Second,
		"the second request must honor the first request's cool-down (>=2s total)")
}

// TestHTTPExecutor429ExhaustsHonestly: when the limiter never relents within
// the retry budget, the final 429 is returned as-is — an honest verdict, not
// a masked success.
func TestHTTPExecutor429ExhaustsHonestly(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"code":"RATE_LIMIT_EXCEEDED"}}`))
	}))
	t.Cleanup(srv.Close)

	e := NewHTTPExecutor(zap.NewNop())
	res := e.Execute(context.Background(), types.HTTPAction{Method: "GET", URL: srv.URL})

	hr, ok := res.(types.HTTPResult)
	require.True(t, ok)
	require.Equal(t, 429, hr.StatusCode, "a persistent limiter surfaces the real status")
	require.Equal(t, int32(1+max429Retries), hits.Load(), "retry budget is bounded")
}
