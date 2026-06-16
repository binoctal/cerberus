package llm

import "net/http"

// GeminiClient implements LLMClient for Google Gemini API
type GeminiClient struct {
	apiKey     string
	model      string
	httpClient *http.Client // nil defaults to http.DefaultClient
	serverURL  string       // overrides baseURL if set (for testing)
}
