package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMockClient(t *testing.T) {
	mock := NewMockClient(map[string]string{
		"default": `{"status":"pass","confidence":0.9}`,
	})

	resp, err := mock.Complete(context.Background(), Request{
		Model:    "mock",
		Messages: []Message{{Role: "user", Content: "test"}},
	})
	require.NoError(t, err)
	assert.Equal(t, `{"status":"pass","confidence":0.9}`, resp.Content)
	assert.Greater(t, resp.Usage.TotalTokens, 0)
}

func TestMockClientWithVision(t *testing.T) {
	mock := NewMockClient(map[string]string{
		"default": `{"status":"pass"}`,
	})

	resp, err := mock.CompleteWithVision(context.Background(),
		"describe this", [][]byte{{1, 2, 3}})
	require.NoError(t, err)
	assert.Equal(t, `{"status":"pass"}`, resp.Content)
}

func TestAutoDetectProvider(t *testing.T) {
	tests := []struct {
		model    string
		provider string
	}{
		{"claude-sonnet-4-6", "anthropic"},
		{"claude-opus-4-8", "anthropic"},
		{"gpt-4.1-2025-04-14", "openai"},
		{"gpt-4o-mini", "openai"},
		{"gemini-3-flash-preview", "gemini"},
		{"gemini-2.5-pro", "gemini"},
		{"mock", "mock"},
	}
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			assert.Equal(t, tt.provider, detectProvider(tt.model))
		})
	}
}

func TestNewClientAutoDetection(t *testing.T) {
	client, err := NewClient("mock", "")
	require.NoError(t, err)
	assert.NotNil(t, client)
	_, ok := client.(*MockClient)
	assert.True(t, ok)
}

func TestClaudeRequestConstruction(t *testing.T) {
	client := NewClaudeClient("test-key", "claude-sonnet-4-6")
	assert.Equal(t, "test-key", client.apiKey)
	assert.Equal(t, "claude-sonnet-4-6", client.model)
	assert.Equal(t, "https://api.anthropic.com/v1/messages", client.baseURL())
}

func TestOpenAIRequestConstruction(t *testing.T) {
	client := NewOpenAIClient("test-key", "gpt-4.1-2025-04-14")
	assert.Equal(t, "test-key", client.apiKey)
	assert.Equal(t, "https://api.openai.com/v1/chat/completions", client.baseURL())
}

func TestGeminiRequestConstruction(t *testing.T) {
	client := NewGeminiClient("test-key", "gemini-3-flash-preview")
	assert.Equal(t, "test-key", client.apiKey)
	assert.Contains(t, client.baseURL(), "generativelanguage.googleapis.com")
}

func TestClaudeClient_Complete(t *testing.T) {
	var receivedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify headers.
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Equal(t, "test-api-key", r.Header.Get("x-api-key"))
		assert.Equal(t, "2023-06-01", r.Header.Get("anthropic-version"))
		assert.Equal(t, "POST", r.Method)

		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedBody)

		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{
			"content": [{"text": "hello from claude"}],
			"usage": {"input_tokens": 10, "output_tokens": 5},
			"stop_reason": "end_turn"
		}`))
	}))
	defer server.Close()

	client := &ClaudeClient{apiKey: "test-api-key", model: "claude-sonnet-4-6", httpClient: server.Client(), serverURL: server.URL}
	resp, err := client.Complete(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	assert.Equal(t, "hello from claude", resp.Content)
	assert.Equal(t, 15, resp.Usage.TotalTokens)
	assert.Equal(t, "end_turn", resp.StopReason)
	assert.Equal(t, "claude-sonnet-4-6", receivedBody["model"])
}

func TestClaudeClient_Complete_Non200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		_, _ = w.Write([]byte(`{"error": "rate limited"}`))
	}))
	defer server.Close()

	client := &ClaudeClient{apiKey: "key", model: "claude-sonnet-4-6", httpClient: server.Client(), serverURL: server.URL}
	_, err := client.Complete(context.Background(), Request{Messages: []Message{{Role: "user", Content: "hi"}}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "anthropic api error 429")
}

func TestClaudeClient_CompleteWithVision(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)

		// Verify messages contain image content block.
		msgs := body["messages"].([]any)
		msg := msgs[0].(map[string]any)
		content := msg["content"].([]any)
		assert.Len(t, content, 2) // text + image

		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{
			"content": [{"text": "I see an image"}],
			"usage": {"input_tokens": 20, "output_tokens": 10},
			"stop_reason": "end_turn"
		}`))
	}))
	defer server.Close()

	client := &ClaudeClient{apiKey: "key", model: "claude-sonnet-4-6", httpClient: server.Client(), serverURL: server.URL}
	resp, err := client.CompleteWithVision(context.Background(), "describe", [][]byte{{1, 2, 3}})
	require.NoError(t, err)
	assert.Equal(t, "I see an image", resp.Content)
	assert.Equal(t, 30, resp.Usage.TotalTokens)
}

func TestOpenAIClient_Complete(t *testing.T) {
	var receivedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		assert.Equal(t, "POST", r.Method)

		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedBody)

		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{
			"choices": [{"message": {"content": "gpt response"}, "finish_reason": "stop"}],
			"usage": {"prompt_tokens": 8, "completion_tokens": 4, "total_tokens": 12}
		}`))
	}))
	defer server.Close()

	client := &OpenAIClient{apiKey: "test-key", model: "gpt-4.1", httpClient: server.Client(), serverURL: server.URL}
	resp, err := client.Complete(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	assert.Equal(t, "gpt response", resp.Content)
	assert.Equal(t, 12, resp.Usage.TotalTokens)
	assert.Equal(t, "stop", resp.StopReason)
	assert.Equal(t, 0.1, receivedBody["temperature"])
}

func TestOpenAIClient_Complete_Non200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		_, _ = w.Write([]byte(`{"error": "internal"}`))
	}))
	defer server.Close()

	client := &OpenAIClient{apiKey: "key", model: "gpt-4.1", httpClient: server.Client(), serverURL: server.URL}
	_, err := client.Complete(context.Background(), Request{Messages: []Message{{Role: "user", Content: "hi"}}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "openai api error 500")
}

func TestOpenAIClient_CompleteWithVision(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)

		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{
			"choices": [{"message": {"content": "image described"}}],
			"usage": {"total_tokens": 50}
		}`))
	}))
	defer server.Close()

	client := &OpenAIClient{apiKey: "key", model: "gpt-4.1", httpClient: server.Client(), serverURL: server.URL}
	resp, err := client.CompleteWithVision(context.Background(), "describe", [][]byte{{0xFF}})
	require.NoError(t, err)
	assert.Equal(t, "image described", resp.Content)
	assert.Equal(t, 50, resp.Usage.TotalTokens)
}

func TestGeminiClient_Complete(t *testing.T) {
	var receivedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "test-gemini-key", r.Header.Get("x-goog-api-key"))

		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedBody)

		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{
			"candidates": [{"content": {"parts": [{"text": "gemini says hi"}]}, "finishReason": "STOP"}],
			"usageMetadata": {"promptTokenCount": 5, "candidatesTokenCount": 3, "totalTokenCount": 8}
		}`))
	}))
	defer server.Close()

	client := &GeminiClient{apiKey: "test-gemini-key", model: "gemini-3-flash", httpClient: server.Client(), serverURL: server.URL}
	resp, err := client.Complete(context.Background(), Request{
		Messages: []Message{
			{Role: "user", Content: "hi"},
			{Role: "assistant", Content: "hello"},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "gemini says hi", resp.Content)
	assert.Equal(t, 8, resp.Usage.TotalTokens)
	assert.Equal(t, "STOP", resp.StopReason)

	// Verify role mapping: assistant -> model.
	contents := receivedBody["contents"].([]any)
	assert.Equal(t, "user", contents[0].(map[string]any)["role"])
	assert.Equal(t, "model", contents[1].(map[string]any)["role"])
}

func TestGeminiClient_Complete_Non200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		_, _ = w.Write([]byte(`{"error": "forbidden"}`))
	}))
	defer server.Close()

	client := &GeminiClient{apiKey: "key", model: "gemini-3-flash", httpClient: server.Client(), serverURL: server.URL}
	_, err := client.Complete(context.Background(), Request{Messages: []Message{{Role: "user", Content: "hi"}}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "gemini api error 403")
}

func TestGeminiClient_CompleteWithVision(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{
			"candidates": [{"content": {"parts": [{"text": "I see it"}]}}],
			"usageMetadata": {"totalTokenCount": 40}
		}`))
	}))
	defer server.Close()

	client := &GeminiClient{apiKey: "key", model: "gemini-3-flash", httpClient: server.Client(), serverURL: server.URL}
	resp, err := client.CompleteWithVision(context.Background(), "describe", [][]byte{{1}})
	require.NoError(t, err)
	assert.Equal(t, "I see it", resp.Content)
	assert.Equal(t, 40, resp.Usage.TotalTokens)
}

func TestMockClient_MatchKey(t *testing.T) {
	mock := NewMockClient(map[string]string{
		"default": `{"ok": true}`,
	})

	// Default key always works.
	resp, err := mock.Complete(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "anything"}},
	})
	require.NoError(t, err)
	assert.Equal(t, `{"ok": true}`, resp.Content)

	// Exact content match.
	mock2 := NewMockClient(map[string]string{
		"hello": `{"matched": true}`,
	})
	resp2, err := mock2.Complete(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "hello"}},
	})
	require.NoError(t, err)
	assert.Equal(t, `{"matched": true}`, resp2.Content)
}
