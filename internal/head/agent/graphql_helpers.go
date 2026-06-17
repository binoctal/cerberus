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
)

// buildGraphQLRequest creates the request body for a GraphQL query
func buildGraphQLRequest(a types.GraphQLQueryAction) (*http.Request, error) {
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
		return nil, err
	}

	req, err := http.NewRequestWithContext(context.Background(), "POST", a.URL, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	for k, v := range a.Headers {
		req.Header.Set(k, v)
	}

	return req, nil
}

// executeGraphQLRequest sends the GraphQL request and returns the response
func executeGraphQLRequest(client *http.Client, req *http.Request, url string, start time.Time) (*http.Response, error) {
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// readGraphQLResponse reads and validates the GraphQL response
func readGraphQLResponse(resp *http.Response, url string, start time.Time) ([]byte, error) {
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

// parseGraphQLResponse parses the GraphQL JSON response
type graphQLResponse struct {
	Data   map[string]any `json:"data"`
	Errors []any          `json:"errors"`
}

func parseGraphQLResponse(respBody []byte) (*graphQLResponse, error) {
	var result graphQLResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("parse response: %v", err)
	}
	return &result, nil
}

// buildGraphQLResult creates the executor result from GraphQL response
func buildGraphQLResult(resp *graphQLResponse, url string, start time.Time, parseErr error) types.GraphQLResult {
	if parseErr != nil {
		return types.GraphQLResult{
			OK:      false,
			URL:     url,
			Err:     fmt.Sprintf("parse response: %v", parseErr),
			Latency: time.Since(start),
		}
	}

	ok := len(resp.Errors) == 0
	return types.GraphQLResult{
		OK:      ok,
		URL:     url,
		Data:    resp.Data,
		Errors:  resp.Errors,
		Latency: time.Since(start),
	}
}
