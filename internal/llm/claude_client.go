package llm

import (
	"net/http"
)

// ClaudeClient implements the Anthropic Claude API client
type ClaudeClient struct {
	apiKey     string
	model      string
	httpClient *http.Client // nil defaults to http.DefaultClient
	serverURL  string       // overrides baseURL if set (for testing)
}

// NewClaudeClient creates a new Claude API client
func NewClaudeClient(apiKey, model, baseURL string) *ClaudeClient {
	return &ClaudeClient{apiKey: apiKey, model: model, serverURL: baseURL}
}

// baseURL returns the API endpoint for messages
func (c *ClaudeClient) baseURL() string {
	const messagesPath = "/v1/messages"
	if c.serverURL == "" {
		return "https://api.anthropic.com" + messagesPath
	}
	// serverURL is a base prefix (e.g. Claude Code's ANTHROPIC_BASE_URL);
	// append the messages path unless it is already a full endpoint.
	return joinBaseURL(c.serverURL, messagesPath)
}

// client returns the HTTP client, defaulting to http.DefaultClient
func (c *ClaudeClient) client() *http.Client {
	if c.httpClient != nil {
		return c.httpClient
	}
	return http.DefaultClient
}
