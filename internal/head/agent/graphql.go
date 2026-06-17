package agent

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/types"
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
	// Phase 1: Build HTTP request
	req, err := buildGraphQLRequest(a)
	if err != nil {
		return types.GraphQLResult{OK: false, URL: a.URL, Err: err.Error(), Latency: time.Since(start)}
	}
	req = req.WithContext(ctx)

	// Phase 2: Execute request
	resp, err := executeGraphQLRequest(e.client, req, a.URL, start)
	if err != nil {
		return types.GraphQLResult{OK: false, URL: a.URL, Err: err.Error(), Latency: time.Since(start)}
	}

	// Phase 3: Read response
	respBody, err := readGraphQLResponse(resp, a.URL, start)
	if err != nil {
		return types.GraphQLResult{OK: false, URL: a.URL, Err: err.Error(), Latency: time.Since(start)}
	}

	// Phase 4: Parse response
	parsed, parseErr := parseGraphQLResponse(respBody)

	// Phase 5: Build result
	return buildGraphQLResult(parsed, a.URL, start, parseErr)
}
