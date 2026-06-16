package llm

import "net/http"

// NewOpenAIClient creates a new OpenAI client
func NewOpenAIClient(apiKey, model, baseURL string) *OpenAIClient {
	return &OpenAIClient{apiKey: apiKey, model: model, serverURL: baseURL}
}

// baseURL returns the API endpoint
func (c *OpenAIClient) baseURL() string {
	if c.serverURL != "" {
		return c.serverURL
	}
	return "https://api.openai.com/v1/chat/completions"
}

// client returns the HTTP client (or default if not set)
func (c *OpenAIClient) client() *http.Client {
	if c.httpClient != nil {
		return c.httpClient
	}
	return http.DefaultClient
}
