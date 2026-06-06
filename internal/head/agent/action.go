package agent

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
)

// ActionExecutor executes an Action and returns an Observation.
type ActionExecutor interface {
	Execute(ctx context.Context, action Action) Observation
}

// HTTPActionExecutor executes HTTP-based actions using net/http.
type HTTPActionExecutor struct {
	client  *http.Client
	baseURL string
	logger  *zap.Logger
}

// NewHTTPActionExecutor creates an executor for HTTP-based test actions.
func NewHTTPActionExecutor(baseURL string, logger *zap.Logger) *HTTPActionExecutor {
	return &HTTPActionExecutor{
		client:  &http.Client{Timeout: 30 * time.Second},
		baseURL: strings.TrimRight(baseURL, "/"),
		logger:  logger,
	}
}

// Execute dispatches based on action type.
func (e *HTTPActionExecutor) Execute(ctx context.Context, action Action) Observation {
	start := time.Now()

	var obs Observation
	switch action.Type {
	case ActionAPIRequest:
		obs = e.doHTTPRequest(ctx, action)
	case ActionNavigate:
		obs = e.doHTTPRequest(ctx, Action{
			Type:   ActionAPIRequest,
			Target: e.resolveTarget(action.Target),
			Method: "GET",
		})
	case ActionWait:
		obs = e.doWait(ctx, action)
	default:
		obs = Observation{
			Success: false,
			Error:   fmt.Sprintf("action type %q not supported in HTTP mode", action.Type),
		}
	}

	obs.Duration = time.Since(start)
	return obs
}

func (e *HTTPActionExecutor) doHTTPRequest(ctx context.Context, action Action) Observation {
	target := e.resolveTarget(action.Target)
	method := action.Method
	if method == "" {
		method = "GET"
	}

	var body io.Reader
	if action.Value != "" && (method == "POST" || method == "PUT" || method == "PATCH") {
		body = strings.NewReader(action.Value)
	}

	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return Observation{Error: fmt.Sprintf("build request: %v", err)}
	}

	for k, v := range action.Headers {
		req.Header.Set(k, v)
	}
	if body != nil && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return Observation{Error: fmt.Sprintf("http request: %v", err)}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB limit
	if err != nil {
		return Observation{
			StatusCode: resp.StatusCode,
			Error:      fmt.Sprintf("read response: %v", err),
		}
	}

	headers := make(map[string]string)
	for k, v := range resp.Header {
		if len(v) > 0 {
			headers[k] = v[0]
		}
	}

	success := resp.StatusCode >= 200 && resp.StatusCode < 300
	return Observation{
		Success:    success,
		StatusCode: resp.StatusCode,
		Body:       string(respBody),
		Headers:    headers,
		URL:        resp.Request.URL.String(),
	}
}

func (e *HTTPActionExecutor) doWait(ctx context.Context, action Action) Observation {
	d := 1 * time.Second
	if action.Value != "" {
		if parsed, err := time.ParseDuration(action.Value); err == nil {
			d = parsed
		} else if ms, err := strconv.Atoi(action.Value); err == nil {
			d = time.Duration(ms) * time.Millisecond
		}
	}

	select {
	case <-time.After(d):
		return Observation{Success: true}
	case <-ctx.Done():
		return Observation{Error: "wait cancelled"}
	}
}

func (e *HTTPActionExecutor) resolveTarget(target string) string {
	if strings.Contains(target, "://") {
		return target
	}
	return e.baseURL + target
}

// Assert interfaces compile.
var _ ActionExecutor = (*HTTPActionExecutor)(nil)
