package llm

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestClaudeStream_MockServer tests Claude streaming with a mock SSE server.
func TestClaudeStream_MockServer(t *testing.T) {
	sseResponse := "" +
		"event: message_start\n" +
		"data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}\n\n" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"Hello\"}}\n\n" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\" world\"}}\n\n" +
		"event: message_stop\n" +
		"data: {\"type\":\"message_stop\",\"message\":{\"usage\":{\"input_tokens\":10,\"output_tokens\":5}}}\n\n"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sseResponse)
	}))
	defer server.Close()

	client := &ClaudeClient{apiKey: "test", model: "test-model", serverURL: server.URL}
	events, err := client.Stream(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)

	var content strings.Builder
	var gotDone bool
	for evt := range events {
		switch evt.Type {
		case StreamDelta:
			content.WriteString(evt.Content)
		case StreamDone:
			gotDone = true
		case StreamError:
			t.Fatalf("stream error: %v", evt.Err)
		}
	}
	assert.True(t, gotDone)
	assert.Equal(t, "Hello world", content.String())
}

// TestOpenAIStream_MockServer tests OpenAI streaming with a mock SSE server.
func TestOpenAIStream_MockServer(t *testing.T) {
	sseResponse := "" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"Hi\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\" there\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":3,\"total_tokens\":8}}\n\n" +
		"data: [DONE]\n\n"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sseResponse)
	}))
	defer server.Close()

	client := &OpenAIClient{apiKey: "test", model: "test-model", serverURL: server.URL}
	events, err := client.Stream(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "hello"}},
	})
	require.NoError(t, err)

	var content strings.Builder
	var gotDone bool
	for evt := range events {
		switch evt.Type {
		case StreamDelta:
			content.WriteString(evt.Content)
		case StreamDone:
			gotDone = true
			if evt.Usage != nil {
				assert.Equal(t, 8, evt.Usage.TotalTokens)
			}
		case StreamError:
			t.Fatalf("stream error: %v", evt.Err)
		}
	}
	assert.True(t, gotDone)
	assert.Equal(t, "Hi there", content.String())
}

// TestClaudeStream_Non200 tests error handling on non-200 status.
func TestClaudeStream_Non200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"error":"rate limited"}`)
	}))
	defer server.Close()

	client := &ClaudeClient{apiKey: "test", model: "test-model", serverURL: server.URL}
	_, err := client.Stream(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "429")
}

// TestOpenAIStream_Non200 tests error handling on non-200 status.
func TestOpenAIStream_Non200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"error":"internal"}`)
	}))
	defer server.Close()

	client := &OpenAIClient{apiKey: "test", model: "test-model", serverURL: server.URL}
	_, err := client.Stream(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

// TestGeminiStream_MockServer tests Gemini streaming with a mock SSE server.
func TestGeminiStream_MockServer(t *testing.T) {
	sseResponse := "" +
		"data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"Goo\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":5,\"candidatesTokenCount\":2,\"totalTokenCount\":7}}\n\n"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sseResponse)
	}))
	defer server.Close()

	client := &GeminiClient{apiKey: "test", model: "test-model", serverURL: server.URL}
	events, err := client.Stream(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)

	var content strings.Builder
	var gotDone bool
	for evt := range events {
		switch evt.Type {
		case StreamDelta:
			content.WriteString(evt.Content)
		case StreamDone:
			gotDone = true
			if evt.Usage != nil {
				assert.Equal(t, 7, evt.Usage.TotalTokens)
			}
		case StreamError:
			t.Fatalf("stream error: %v", evt.Err)
		}
	}
	assert.True(t, gotDone)
	assert.Equal(t, "Goo", content.String())
}

// TestGeminiStream_Non200 tests error handling on non-200 status.
func TestGeminiStream_Non200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprint(w, `{"error":"overloaded"}`)
	}))
	defer server.Close()

	client := &GeminiClient{apiKey: "test", model: "test-model", serverURL: server.URL}
	_, err := client.Stream(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "503")
}
