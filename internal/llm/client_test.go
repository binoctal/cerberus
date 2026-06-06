package llm

import (
	"context"
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
