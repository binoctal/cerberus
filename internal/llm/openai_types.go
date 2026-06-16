package llm

import "net/http"

// OpenAIClient implements LLMClient for OpenAI API
type OpenAIClient struct {
	apiKey     string
	model      string
	httpClient *http.Client // nil defaults to http.DefaultClient
	serverURL  string       // overrides baseURL if set (for testing)
}
