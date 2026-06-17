package agent

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/types"
)

// HTTPExecutor handles HTTP API and navigation actions.
type HTTPExecutor struct {
	client *http.Client
	logger *zap.Logger
}

// NewHTTPExecutor creates an HTTP executor.
func NewHTTPExecutor(logger *zap.Logger) *HTTPExecutor {
	return &HTTPExecutor{
		client: &http.Client{Timeout: 30 * time.Second},
		logger: logger,
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
