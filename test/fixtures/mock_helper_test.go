package fixtures

import (
	"context"
	"encoding/json"

	"github.com/binoctal/cerberus/internal/llm"
)

// MockClient returns a mock llm.Client that satisfies every head inside
// Session.Run for fixture tests. The same client drives:
//   - Scout.Plan via DecideWithTools (tool-call preset)
//   - Scout.BuildCoverageContract / Agent.Steer / Examiner.Judge via Decide (content JSON)
//
// Under the S2 tool-calling migration, Scout.Plan no longer parses a PlanOutput
// JSON "cases" field — it consumes tool calls. The combined client emits both
// on every Complete call so a single mock covers the cross-head flow.
func MockClient(targetFile string) llm.Client {
	return &fixtureMockClient{
		content:   contractJSONFor(targetFile),
		toolCalls: []llm.ToolCall{{Name: "check_file", Input: map[string]any{"action": "read", "path": targetFile}}},
	}
}

// contractJSONFor returns the JSON content consumed by BuildCoverageContract
// (depth/scope/coverage_gate). The legacy "cases" field is intentionally
// omitted — Scout.Plan now consumes tool calls, not JSON cases.
func contractJSONFor(targetFile string) string {
	response := map[string]any{
		"depth":       "standard",
		"scope":       []string{targetFile},
		"path_types":  []string{"happy"},
		"error_scope": []string{"4xx"},
		"boundaries":  []string{"empty"},
		"priorities":  map[string]any{},
		"coverage_gate": map[string]any{
			"module":         targetFile,
			"line_threshold": 0.5,
		},
	}
	b, _ := json.Marshal(response)
	return string(b)
}

// fixtureMockClient mirrors session.combinedMockClient — duplicated here to
// keep the fixtures package self-contained (it cannot import internal/session
// test helpers without an internal test stub).
type fixtureMockClient struct {
	content   string
	toolCalls []llm.ToolCall
}

func (m *fixtureMockClient) Complete(ctx context.Context, req llm.Request) (*llm.Response, error) {
	inputTokens := len(req.Messages) * 10
	outputTokens := len(m.content) / 4
	return &llm.Response{
		Content:    m.content,
		ToolCalls:  m.toolCalls,
		StopReason: "tool_use",
		Usage:      llm.TokenUsage{InputTokens: inputTokens, OutputTokens: outputTokens, TotalTokens: inputTokens + outputTokens},
	}, nil
}

func (m *fixtureMockClient) CompleteWithVision(ctx context.Context, prompt string, images [][]byte) (*llm.Response, error) {
	return m.Complete(ctx, llm.Request{Messages: []llm.Message{{Role: "user", Content: prompt}}})
}

func (m *fixtureMockClient) Stream(ctx context.Context, req llm.Request) (<-chan llm.StreamEvent, error) {
	resp, err := m.Complete(ctx, req)
	if err != nil {
		ch := make(chan llm.StreamEvent, 1)
		ch <- llm.StreamEvent{Type: llm.StreamError, Err: err}
		close(ch)
		return ch, nil
	}
	ch := make(chan llm.StreamEvent, 2)
	ch <- llm.StreamEvent{Type: llm.StreamDelta, Content: resp.Content, Usage: &resp.Usage}
	ch <- llm.StreamEvent{Type: llm.StreamDone, Usage: &resp.Usage}
	close(ch)
	return ch, nil
}
