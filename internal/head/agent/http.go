package agent

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/types"
)

// HTTPExecutor handles HTTP API and navigation actions.
type HTTPExecutor struct {
	client         *http.Client
	logger         *zap.Logger
	serviceHeaders map[string]map[string]string
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
	// Phase 1: Prepare HTTP request
	req, err := prepareHTTPRequest(ctx, a)
	if err != nil {
		return buildHTTPErrorResult(err, a.URL, start)
	}

	// Phase 2: Execute request
	resp, err := executeHTTPRequest(e.client, req)
	if err != nil {
		return buildHTTPErrorResult(err, a.URL, start)
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
