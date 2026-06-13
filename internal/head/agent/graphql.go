package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/binoctal/cerberus/internal/types"
	"go.uber.org/zap"
)

// GraphQLExecutor handles GraphQL query and mutation actions.
type GraphQLExecutor struct {
	client *http.Client
	logger *zap.Logger
}

// NewGraphQLExecutor creates a GraphQL executor.
func NewGraphQLExecutor(logger *zap.Logger) *GraphQLExecutor {
	return &GraphQLExecutor{
		client: &http.Client{Timeout: 30 * time.Second},
		logger: logger,
	}
}

// Execute dispatches GraphQL actions.
func (e *GraphQLExecutor) Execute(ctx context.Context, action types.TypedAction) types.ExecutorResult {
	start := time.Now()
	switch a := action.(type) {
	case types.GraphQLQueryAction:
		return e.doQuery(ctx, a, start)
	default:
		return types.ErrorResult{Err: fmt.Sprintf("graphql executor: unsupported action %T", action)}
	}
}

func (e *GraphQLExecutor) doQuery(ctx context.Context, a types.GraphQLQueryAction, start time.Time) types.ExecutorResult {
	body := map[string]any{
		"query": a.Query,
	}
	if len(a.Variables) > 0 {
		body["variables"] = a.Variables
	}
	if a.OperationName != "" {
		body["operationName"] = a.OperationName
	}

	b, err := json.Marshal(body)
	if err != nil {
		return types.GraphQLResult{OK: false, URL: a.URL, Err: err.Error(), Latency: time.Since(start)}
	}

	req, err := http.NewRequestWithContext(ctx, "POST", a.URL, bytes.NewReader(b))
	if err != nil {
		return types.GraphQLResult{OK: false, URL: a.URL, Err: err.Error(), Latency: time.Since(start)}
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range a.Headers {
		req.Header.Set(k, v)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return types.GraphQLResult{OK: false, URL: a.URL, Err: err.Error(), Latency: time.Since(start)}
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return types.GraphQLResult{OK: false, URL: a.URL, Err: err.Error(), Latency: time.Since(start)}
	}

	if resp.StatusCode != 200 {
		return types.GraphQLResult{
			OK: false, URL: a.URL,
			Err:     fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(respBody)),
			Latency: time.Since(start),
		}
	}

	var result struct {
		Data   map[string]any `json:"data"`
		Errors []any          `json:"errors"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return types.GraphQLResult{OK: false, URL: a.URL, Err: fmt.Sprintf("parse response: %v", err), Latency: time.Since(start)}
	}

	ok := len(result.Errors) == 0
	return types.GraphQLResult{
		OK: ok, URL: a.URL,
		Data:    result.Data,
		Errors:  result.Errors,
		Latency: time.Since(start),
	}
}
