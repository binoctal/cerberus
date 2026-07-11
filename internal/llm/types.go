package llm

// Request represents an LLM API request.
type Request struct {
	Model     string    `json:"model"`
	Messages  []Message `json:"messages"`
	MaxTokens int       `json:"max_tokens,omitempty"`
	Tools     []Tool    `json:"tools,omitempty"`
}

// Message represents a single message in a conversation.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Response represents an LLM API response.
type Response struct {
	Content    string     `json:"content"`
	Usage      TokenUsage `json:"usage"`
	StopReason string     `json:"stop_reason,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
}

// Tool describes a function the LLM can call.
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema,omitempty"`
}

// ToolCall represents a single function call requested by the LLM.
type ToolCall struct {
	ID    string         `json:"id"`
	Name  string         `json:"name"`
	Input map[string]any `json:"input"`
}

// TokenUsage represents token count information for an LLM request/response.
type TokenUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

// StreamEventType identifies the type of a streaming event.
type StreamEventType string

const (
	StreamDelta StreamEventType = "delta" // Incremental content chunk
	StreamDone  StreamEventType = "done"  // Stream completed successfully
	StreamError StreamEventType = "error" // Stream encountered an error
)

// StreamEvent represents a single event from a streaming LLM response.
type StreamEvent struct {
	Type    StreamEventType
	Content string
	Usage   *TokenUsage
	Err     error
}

// AuthScheme selects the HTTP authentication header for Anthropic-compatible
// endpoints. It is derived from the credential source: an API key (*_API_KEY)
// uses the native x-api-key header; an auth token (*_AUTH_TOKEN, e.g. an OAuth
// bearer from Claude Code or a proxy) uses Authorization: Bearer.
type AuthScheme string

const (
	// AuthSchemeAPIKey sends the credential via the x-api-key header. This is
	// the default and matches api.anthropic.com's native key auth.
	AuthSchemeAPIKey AuthScheme = "api_key"
	// AuthSchemeBearer sends the credential via Authorization: Bearer, required
	// by OAuth auth tokens and proxies (e.g. ModelSite) that reject x-api-key.
	AuthSchemeBearer AuthScheme = "bearer"
)

// ClientConfig holds options for creating an LLM client.
type ClientConfig struct {
	Model      string
	APIKey     string
	BaseURL    string     // optional: overrides the provider's default API URL
	Provider   string     // optional: "anthropic"|"openai"|"gemini"|"mock"; overrides model-based detection
	AuthScheme AuthScheme // optional: auth header for Anthropic; empty defaults to AuthSchemeAPIKey
}
