package agent

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/types"
)

// 429 backoff knobs: a rate limiter answering 429 is working as designed, so
// the executor honors Retry-After (clamped) and retries a bounded number of
// times instead of letting the sweep's burst become case failures.
const (
	max429Retries         = 2
	minRetryAfterWait     = 250 * time.Millisecond
	defaultRetryAfterWait = 1 * time.Second
	maxRetryAfterWait     = 60 * time.Second
)

// HTTPExecutor handles HTTP API and navigation actions.
type HTTPExecutor struct {
	client         *http.Client
	logger         *zap.Logger
	serviceHeaders map[string]map[string]string

	cooldownMu sync.Mutex
	cooldown   map[string]time.Time // per-host "next request no earlier than"
}

// NewHTTPExecutor creates an HTTP executor with no service-level headers.
func NewHTTPExecutor(logger *zap.Logger) *HTTPExecutor {
	return NewHTTPExecutorWithServiceHeaders(logger, nil)
}

// NewHTTPExecutorWithServiceHeaders creates an HTTP executor that injects
// service-level headers (keyed by request "host:port") into every matching
// request. Action headers override service headers; an empty action value
// removes the header.
func NewHTTPExecutorWithServiceHeaders(logger *zap.Logger, serviceHeaders map[string]map[string]string) *HTTPExecutor {
	return &HTTPExecutor{
		client:         &http.Client{Timeout: 30 * time.Second},
		logger:         logger,
		serviceHeaders: serviceHeaders,
		cooldown:       map[string]time.Time{},
	}
}

// Execute dispatches HTTP actions.
func (e *HTTPExecutor) Execute(ctx context.Context, action types.TypedAction) types.ExecutorResult {
	start := time.Now()
	switch a := action.(type) {
	case types.HTTPAction:
		return e.doHTTP(ctx, a, start)
	case types.NavigateAction:
		return e.doHTTP(ctx, types.HTTPAction{Method: "GET", URL: a.URL}, start)
	default:
		return types.ErrorResult{Err: fmt.Sprintf("http executor: unsupported action %T", action)}
	}
}

func (e *HTTPExecutor) doHTTP(ctx context.Context, a types.HTTPAction, start time.Time) types.ExecutorResult {
	// Phase 0: Merge service-level headers (matched by URL host) under the
	// action's own headers before preparing the request.
	a = e.withServiceHeaders(a)
	// Phase 0.5: Honor an outstanding per-host 429 cool-down so the request
	// behind a throttled one does not walk straight back into the limiter.
	if err := e.awaitCooldown(ctx, a.URL); err != nil {
		return buildHTTPErrorResult(err, a.URL, start)
	}

	// Phase 1+2: Prepare and execute the request, retrying 429s after the
	// clamped Retry-After backoff (the limiter working as designed must not
	// read as a case failure). A persistent limiter surfaces its final 429.
	var resp *http.Response
	for attempt := 0; ; attempt++ {
		req, err := prepareHTTPRequest(ctx, a)
		if err != nil {
			return buildHTTPErrorResult(err, a.URL, start)
		}
		resp, err = executeHTTPRequest(e.client, req)
		if err != nil {
			return buildHTTPErrorResult(err, a.URL, start)
		}
		if resp.StatusCode != http.StatusTooManyRequests || attempt >= max429Retries {
			break
		}
		wait := retryAfterWait(resp)
		_ = resp.Body.Close()
		e.setCooldown(a.URL, wait)
		if err := sleepContext(ctx, wait); err != nil {
			return buildHTTPErrorResult(err, a.URL, start)
		}
	}
	defer func() { _ = resp.Body.Close() }()

	// Phase 3: Read response body
	respBody, err := readResponseBody(resp)
	if err != nil {
		return buildHTTPErrorWithStatusCode(err, resp.StatusCode, a.URL, start)
	}

	// Phase 4: Build success result
	return buildHTTPSuccessResult(resp, respBody, a.URL, start)
}

// retryAfterWait reads the Retry-After header as seconds and clamps it into
// [minRetryAfterWait, maxRetryAfterWait]. A missing or unparseable header
// falls back to the default wait; the ceiling keeps a hostile or huge value
// from stalling a run for minutes.
func retryAfterWait(resp *http.Response) time.Duration {
	if secs, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && secs >= 0 {
		d := time.Duration(secs) * time.Second
		if d > maxRetryAfterWait {
			return maxRetryAfterWait
		}
		if d < minRetryAfterWait {
			return minRetryAfterWait
		}
		return d
	}
	return defaultRetryAfterWait
}

// awaitCooldown blocks until the host's 429 cool-down deadline passes (a no-op
// when none is outstanding). Context cancellation aborts the wait.
func (e *HTTPExecutor) awaitCooldown(ctx context.Context, rawURL string) error {
	deadline, ok := e.cooldownOf(rawURL)
	if !ok {
		return nil
	}
	return sleepContext(ctx, time.Until(deadline))
}

// setCooldown records the per-host "no earlier than" deadline after a 429.
// cooldownKey mirrors hostOf but stays unexported-test-name-free.
func (e *HTTPExecutor) setCooldown(rawURL string, wait time.Duration) {
	host := cooldownKey(rawURL)
	e.cooldownMu.Lock()
	e.cooldown[host] = time.Now().Add(wait)
	e.cooldownMu.Unlock()
}

func (e *HTTPExecutor) cooldownOf(rawURL string) (time.Time, bool) {
	host := cooldownKey(rawURL)
	e.cooldownMu.Lock()
	defer e.cooldownMu.Unlock()
	deadline, ok := e.cooldown[host]
	if !ok || time.Now().After(deadline) {
		return time.Time{}, false
	}
	return deadline, true
}

func cooldownKey(rawURL string) string {
	if u, err := url.Parse(rawURL); err == nil {
		return u.Host
	}
	return rawURL
}

func sleepContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// withServiceHeaders merges service-level headers (matched by the request URL
// host) underneath the action's own headers. Action headers override service
// headers; an empty-string action value removes the header entirely.
func (e *HTTPExecutor) withServiceHeaders(a types.HTTPAction) types.HTTPAction {
	if len(e.serviceHeaders) == 0 {
		return a
	}
	u, err := url.Parse(a.URL)
	if err != nil {
		return a
	}
	svc, ok := e.serviceHeaders[u.Host]
	if !ok || len(svc) == 0 {
		return a
	}
	merged := make(map[string]string, len(svc)+len(a.Headers))
	for k, v := range svc {
		merged[k] = v
	}
	for k, v := range a.Headers {
		if v == "" {
			delete(merged, k)
			continue
		}
		merged[k] = v
	}
	a.Headers = merged
	return a
}
