package llm

import (
	"fmt"
	"net/http"
)

// NewGeminiClient creates a new Gemini client
func NewGeminiClient(apiKey, model, baseURL string) *GeminiClient {
	return &GeminiClient{apiKey: apiKey, model: model, serverURL: baseURL}
}

// baseURL returns the API endpoint for non-streaming requests
func (c *GeminiClient) baseURL() string {
	if c.serverURL != "" {
		return c.serverURL
	}
	return fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent", c.model)
}

// streamURL returns the API endpoint for streaming requests
func (c *GeminiClient) streamURL() string {
	if c.serverURL != "" {
		return c.serverURL
	}
	return fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:streamGenerateContent?alt=sse", c.model)
}

// client returns the HTTP client (or default if not set)
func (c *GeminiClient) client() *http.Client {
	if c.httpClient != nil {
		return c.httpClient
	}
	return http.DefaultClient
}
