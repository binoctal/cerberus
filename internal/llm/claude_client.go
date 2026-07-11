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
	authScheme AuthScheme   // empty defaults to AuthSchemeAPIKey
}

// NewClaudeClient creates a new Claude API client. Auth defaults to the native
// x-api-key header; the factory overrides authScheme when the credential is an
// auth token (see NewClientWithConfig).
func NewClaudeClient(apiKey, model, baseURL string) *ClaudeClient {
	return &ClaudeClient{apiKey: apiKey, model: model, serverURL: baseURL}
}

// setAuthHeader applies the credential using the client's auth scheme. Auth
// tokens (bearer) use Authorization: Bearer; everything else uses x-api-key,
// matching api.anthropic.com's native key auth.
func (c *ClaudeClient) setAuthHeader(h http.Header) {
	if c.authScheme == AuthSchemeBearer {
		h.Set("Authorization", "Bearer "+c.apiKey)
		return
	}
	h.Set("x-api-key", c.apiKey)
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
