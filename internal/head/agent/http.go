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
// the executor absorbs it instead of letting the sweep's burst become case
// failures. Design (run31 lesson): one SHORT clamped backoff to let the
// window drain, then SUSTAINED light pacing (throttlePaceInterval between
// requests for throttlePaceWindow) — never serialize every request behind a
// full Retry-After deadline, which stalls the sweep to one case per window
// and starves per-case contexts into fake transport errors.
const (
	max429Retries         = 2
	minRetryAfterWait     = 250 * time.Millisecond
	defaultRetryAfterWait = 1 * time.Second
	maxRetryAfterWait     = 15 * time.Second
	throttlePaceInterval  = 400 * time.Millisecond
	throttlePaceWindow    = 2 * time.Minute
)

// hostPacing is one host's throttle state: no request before next while the
// sustained-pacing window (until) is open.
type hostPacing struct {
	next  time.Time
	until time.Time
}

// HTTPExecutor handles HTTP API and navigation actions.
type HTTPExecutor struct {
	client         *http.Client
	logger         *zap.Logger
	serviceHeaders map[string]map[string]string

	paceMu sync.Mutex
	pace   map[string]hostPacing
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
		pace:           map[string]hostPacing{},
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
	// Phase 0.5: Honor the host's throttle pacing — after a 429 the host is
	// lightly paced (min interval between requests) so the sweep stays under
	// the limiter window without stalling.
	if wait := e.acquirePace(a.URL); wait > 0 {
		if err := sleepContext(ctx, wait); err != nil {
			return buildHTTPErrorResult(err, a.URL, start)
		}
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
		e.markThrottled(a.URL, wait)
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

// acquirePace reserves the host's next request slot and returns how long the
// caller must wait first (0 when the host is unthrottled or the sustained
// pacing window has lapsed). Consecutive callers are spaced
// throttlePaceInterval apart while the window is open.
func (e *HTTPExecutor) acquirePace(rawURL string) time.Duration {
	host := cooldownKey(rawURL)
	now := time.Now()
	e.paceMu.Lock()
	defer e.paceMu.Unlock()
	st, ok := e.pace[host]
	if !ok || now.After(st.until) {
		return 0
	}
	next := st.next
	if next.Before(now) {
		next = now
	}
	st.next = next.Add(throttlePaceInterval)
	e.pace[host] = st
	return next.Sub(now)
}

// markThrottled records a 429: one clamped backoff for the retried request,
// then sustained light pacing for throttlePaceWindow so the requests behind
// it stay under the limiter without serializing behind a full window.
func (e *HTTPExecutor) markThrottled(rawURL string, wait time.Duration) {
	host := cooldownKey(rawURL)
	now := time.Now()
	e.paceMu.Lock()
	e.pace[host] = hostPacing{next: now.Add(wait), until: now.Add(throttlePaceWindow)}
	e.paceMu.Unlock()
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
