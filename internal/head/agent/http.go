package agent

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
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
	var body io.Reader
	if a.Body != "" {
		body = strings.NewReader(a.Body)
	}
	req, err := http.NewRequestWithContext(ctx, a.Method, a.URL, body)
	if err != nil {
		return types.HTTPResult{OK: false, URL: a.URL, Err: err.Error(), Latency: time.Since(start)}
	}
	for k, v := range a.Headers {
		req.Header.Set(k, v)
	}
	if body != nil && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return types.HTTPResult{OK: false, URL: a.URL, Err: err.Error(), Latency: time.Since(start)}
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return types.HTTPResult{OK: false, StatusCode: resp.StatusCode, URL: a.URL, Err: err.Error(), Latency: time.Since(start)}
	}

	headers := make(map[string]string)
	for k, v := range resp.Header {
		if len(v) > 0 {
			headers[k] = v[0]
		}
	}

	ok := resp.StatusCode >= 200 && resp.StatusCode < 400
	return types.HTTPResult{
		OK: ok, StatusCode: resp.StatusCode, Body: string(respBody),
		Headers: headers, URL: a.URL, Latency: time.Since(start),
	}
}
