package llm

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMockClient_Stream(t *testing.T) {
	mock := NewMockClient(map[string]string{
		"default": `{"status":"pass","confidence":0.9}`,
	})

	events, err := mock.Stream(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "test"}},
	})
	require.NoError(t, err)

	var collected strings.Builder
	var gotDone bool
	for evt := range events {
		switch evt.Type {
		case StreamDelta:
			collected.WriteString(evt.Content)
		case StreamDone:
			gotDone = true
			assert.NotNil(t, evt.Usage)
		case StreamError:
			t.Fatalf("unexpected error: %v", evt.Err)
		}
	}

	assert.True(t, gotDone, "should receive done event")
	assert.Equal(t, `{"status":"pass","confidence":0.9}`, collected.String())
}

func TestMockClient_Stream_DefaultResponse(t *testing.T) {
	mock := NewMockClient(nil)

	events, err := mock.Stream(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "test"}},
	})
	require.NoError(t, err)

	var gotDone bool
	for evt := range events {
		if evt.Type == StreamDone {
			gotDone = true
		}
	}
	assert.True(t, gotDone)
}

func TestMockClient_SetToolResponse(t *testing.T) {
	mock := NewMockClient(nil)
	mock.SetToolResponse("plan a relay", []ToolCall{
		{ID: "call_1", Name: "ws_relay", Input: map[string]any{"roles": []any{"web", "bridge"}}},
	})
	resp, err := mock.Complete(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "plan a relay"}},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Content != "" {
		t.Errorf("Content: got %q, want empty (pure tool_use turn)", resp.Content)
	}
	if resp.StopReason != "tool_use" {
		t.Errorf("StopReason: got %q, want tool_use", resp.StopReason)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Name != "ws_relay" {
		t.Fatalf("ToolCalls: got %+v, want one ws_relay call", resp.ToolCalls)
	}
}

// matchKey must consult toolResponses too — a key present only there must NOT
// hash to sha256[:8] (which would make SetToolResponse silently never match).
func TestMockClient_MatchKeyConsultsToolResponses(t *testing.T) {
	mock := NewMockClient(nil)
	mock.SetToolResponse("only-in-tool-responses", []ToolCall{{Name: "f"}})
	resp, err := mock.Complete(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "only-in-tool-responses"}},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("toolResponses-only key must match directly (no hash); got ToolCalls=%+v", resp.ToolCalls)
	}
}
