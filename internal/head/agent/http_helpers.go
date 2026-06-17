package agent

import (
	"context"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/binoctal/cerberus/internal/types"
)

// prepareHTTPRequest creates an HTTP request with body and headers
func prepareHTTPRequest(ctx context.Context, a types.HTTPAction) (*http.Request, error) {
	var body io.Reader
	if a.Body != "" {
		body = strings.NewReader(a.Body)
	}

	req, err := http.NewRequestWithContext(ctx, a.Method, a.URL, body)
	if err != nil {
		return nil, err
	}

	// Set user-provided headers
	for k, v := range a.Headers {
		req.Header.Set(k, v)
	}

	// Set default Content-Type for requests with body
	if body != nil && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}

	return req, nil
}

// executeHTTPRequest executes the HTTP request and returns the response
func executeHTTPRequest(client *http.Client, req *http.Request) (*http.Response, error) {
	return client.Do(req)
}

// readResponseBody reads the response body with size limit
func readResponseBody(resp *http.Response) ([]byte, error) {
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB limit
}

// extractResponseHeaders extracts the first value from each response header
func extractResponseHeaders(resp *http.Response) map[string]string {
	headers := make(map[string]string)
	for k, v := range resp.Header {
		if len(v) > 0 {
			headers[k] = v[0]
		}
	}
	return headers
}

// buildHTTPSuccessResult creates a success result from HTTP response
func buildHTTPSuccessResult(resp *http.Response, body []byte, url string, start time.Time) types.HTTPResult {
	headers := extractResponseHeaders(resp)
	ok := resp.StatusCode >= 200 && resp.StatusCode < 400
	return types.HTTPResult{
		OK:         ok,
		StatusCode: resp.StatusCode,
		Body:       string(body),
		Headers:    headers,
		URL:        url,
		Latency:    time.Since(start),
	}
}

// buildHTTPErrorResult creates an error result from HTTP error
func buildHTTPErrorResult(err error, url string, start time.Time) types.HTTPResult {
	return types.HTTPResult{
		OK:      false,
		URL:     url,
		Err:     err.Error(),
		Latency: time.Since(start),
	}
}

// buildHTTPErrorWithStatusCode creates an error result with status code
func buildHTTPErrorWithStatusCode(err error, statusCode int, url string, start time.Time) types.HTTPResult {
	return types.HTTPResult{
		OK:         false,
		StatusCode: statusCode,
		URL:        url,
		Err:        err.Error(),
		Latency:    time.Since(start),
	}
}
