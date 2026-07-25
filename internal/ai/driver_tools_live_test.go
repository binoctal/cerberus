//go:build live

package ai_test

import (
	"context"
	"testing"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/config"
	"github.com/binoctal/cerberus/internal/llm"
)

// TestDecideWithTools_LiveGLM is the S1 feasibility gate for the tool-calling
// migration: does the BigModel GLM endpoint (anthropic-compatible, via cerberus's
// claude provider) actually return tool_use when given tools? cerberus's
// DecideWithTools + Request.Tools + Response.ToolCalls + claude tool_use parsing
// all exist, but no head uses them and it has never been live-confirmed against
// GLM. Build-tagged `live` so it never runs in `make test`. Run:
//
//	go test -tags live -run TestDecideWithTools_LiveGLM -v ./internal/ai/
func TestDecideWithTools_LiveGLM(t *testing.T) {
	cfg := config.Load()
	if cfg.LLMAPIKey == "" {
		t.Skip("no LLM API key in settings.json env — live probe needs a real LLM")
	}

	client, err := llm.NewClientWithConfig(llm.ClientConfig{
		Model:      cfg.LLMModel,
		APIKey:     cfg.LLMAPIKey,
		BaseURL:    cfg.LLMBaseURL,
		Provider:   cfg.LLMProvider,
		AuthScheme: cfg.LLMAuthScheme,
	})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	driver := ai.NewDriver(client, ai.NewTokenBudget(10000, 2000))

	tools := []llm.Tool{{
		Name:        "get_weather",
		Description: "Get the current weather for a given location.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"location": map[string]any{"type": "string", "description": "City name"},
			},
			"required": []string{"location"},
		},
	}}

	res, err := driver.DecideWithTools(context.Background(),
		"What is the weather in Tokyo? You MUST use the get_weather tool to answer.", tools)
	if err != nil {
		t.Fatalf("DecideWithTools: %v", err)
	}

	t.Logf("content: %q", res.Content)
	t.Logf("tool_calls: %d", len(res.ToolCalls))
	for i, tc := range res.ToolCalls {
		t.Logf("  call[%d] name=%s input=%v", i, tc.Name, tc.Input)
	}
	if len(res.ToolCalls) == 0 {
		t.Fatalf("GATE FAILED: GLM emitted no tool_use — tool-calling migration (approach A) is NOT viable on this endpoint")
	}
	t.Logf("GATE PASSED: GLM emitted %d tool_use call(s) — approach A is viable", len(res.ToolCalls))
}
