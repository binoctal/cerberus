package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// toolFixtureServer returns a provider-shaped JSON body for every request.
func toolFixtureServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
}

func TestClaudeComplete_ToolUseParse(t *testing.T) {
	body := `{"content":[{"type":"tool_use","id":"call_1","name":"get_weather","input":{"location":"Tokyo"}}],"stop_reason":"tool_use","usage":{"input_tokens":5,"output_tokens":7}}`
	srv := toolFixtureServer(t, body)
	defer srv.Close()
	c, err := NewClientWithConfig(ClientConfig{Provider: "anthropic", BaseURL: srv.URL, APIKey: "k", Model: "m"})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	resp, err := c.Complete(context.Background(), Request{Messages: []Message{{Role: "user", Content: "x"}}})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Name != "get_weather" {
		t.Fatalf("ToolCalls = %+v, want one get_weather", resp.ToolCalls)
	}
	if got := resp.ToolCalls[0].Input["location"]; got != "Tokyo" {
		t.Errorf("input.location = %v, want Tokyo", got)
	}
}

func TestGeminiComplete_FunctionCallParse(t *testing.T) {
	body := `{"candidates":[{"content":{"parts":[{"functionCall":{"name":"get_weather","args":{"location":"Tokyo"}}}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":7,"totalTokenCount":12}}`
	srv := toolFixtureServer(t, body)
	defer srv.Close()
	c, err := NewClientWithConfig(ClientConfig{Provider: "gemini", BaseURL: srv.URL, APIKey: "k", Model: "m"})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	resp, err := c.Complete(context.Background(), Request{Messages: []Message{{Role: "user", Content: "x"}}})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Name != "get_weather" {
		t.Fatalf("ToolCalls = %+v, want one get_weather", resp.ToolCalls)
	}
	if got := resp.ToolCalls[0].Input["location"]; got != "Tokyo" {
		t.Errorf("input.location = %v, want Tokyo", got)
	}
}

func TestOpenAIComplete_ToolCallsParse(t *testing.T) {
	body := `{"choices":[{"message":{"content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"get_weather","arguments":"{\"location\":\"Tokyo\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":5,"completion_tokens":7,"total_tokens":12}}`
	srv := toolFixtureServer(t, body)
	defer srv.Close()
	c, err := NewClientWithConfig(ClientConfig{Provider: "openai", BaseURL: srv.URL, APIKey: "k", Model: "m"})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	resp, err := c.Complete(context.Background(), Request{Messages: []Message{{Role: "user", Content: "x"}}})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Name != "get_weather" {
		t.Fatalf("ToolCalls = %+v, want one get_weather", resp.ToolCalls)
	}
	if got := resp.ToolCalls[0].Input["location"]; got != "Tokyo" {
		t.Errorf("input.location = %v, want Tokyo", got)
	}
}
