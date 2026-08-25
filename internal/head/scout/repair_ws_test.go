package scout

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
)

// TestRepairTools_HasStepsArray: repair_case carries an optional `steps` array
// for WS repair (items mirror agent.TestStep) and keeps `replaces` required.
// Negative: no steps property → assertion fails (RED).
func TestRepairTools_HasStepsArray(t *testing.T) {
	tools := repairTools()
	require.Len(t, tools, 1)
	props := tools[0].InputSchema["properties"].(map[string]any)

	steps, ok := props["steps"].(map[string]any)
	require.True(t, ok, "repair_case must have a `steps` property")
	assert.Equal(t, "array", steps["type"], "steps must be an array")
	items, ok := steps["items"].(map[string]any)
	require.True(t, ok, "steps items must be an object")
	itemProps := items["properties"].(map[string]any)
	for _, f := range []string{"action", "message", "type", "asserts", "match_all", "connection_id"} {
		assert.NotNil(t, itemProps[f], "steps item must expose %q", f)
	}

	required := tools[0].InputSchema["required"].([]any)
	assert.Contains(t, required, "replaces", "replaces stays required")
}

// TestAssembleRepair_WSFlowSteps: a repair_case with `steps` builds a WS
// replacement TestCase (Action=ws_flow, Steps carried — action/message/type/
// asserts/match_all), Target from the first connect url, Replaces bound. Without
// the steps branch the case would be HTTP-shaped (no Steps) — the assertion RED.
func TestAssembleRepair_WSFlowSteps(t *testing.T) {
	failures := []RepairInput{
		{Case: agent.TestCase{ID: "ws-1", Action: "ws_flow", Target: "wss://x", Service: "ws",
			Steps: []agent.TestStep{{Action: "ws_connect", ConnectionID: "web"}}},
			Hint: agent.HintWsMatch, Reasoning: "receive matched nothing"},
	}
	calls := []llm.ToolCall{
		{Name: "repair_case", Input: map[string]any{
			"replaces":    "ws-1",
			"service":     "ws",
			"expectation": "receives hello",
			"steps": []any{
				map[string]any{"action": "ws_connect", "connection_id": "web", "url": "wss://x"},
				map[string]any{"action": "ws_send", "connection_id": "web", "message": `{"subscribe":"all"}`},
				map[string]any{"action": "ws_receive", "connection_id": "web", "type": "hello",
					"match_all": true, "asserts": map[string]any{"payload.approved": true}},
			},
		}},
	}
	out := assembleRepair(calls, failures)
	require.Len(t, out, 1)
	c := out[0]
	assert.Equal(t, "ws-1", c.Replaces)
	assert.Equal(t, "ws_flow", c.Action)
	require.Len(t, c.Steps, 3)
	assert.Equal(t, "ws_connect", c.Steps[0].Action)
	assert.Equal(t, "wss://x", c.Steps[0].URL)
	assert.Equal(t, "ws_send", c.Steps[1].Action)
	assert.Equal(t, `{"subscribe":"all"}`, c.Steps[1].Message)
	assert.Equal(t, "ws_receive", c.Steps[2].Action)
	assert.Equal(t, "hello", c.Steps[2].Type)
	assert.True(t, c.Steps[2].MatchAll)
	assert.Equal(t, true, c.Steps[2].Asserts["payload.approved"])
	assert.Equal(t, "wss://x", c.Target, "Target derived from the first connect url")
	assert.Equal(t, "receives hello", c.Expectation)
}

// TestAssembleRepair_HTTPUnchangedByStepsBranch: an HTTP repair_case (no steps)
// still produces an HTTP TestCase with Target/Method/Body and NO Steps — the WS
// branch must not regress HTTP repair.
func TestAssembleRepair_HTTPUnchangedByStepsBranch(t *testing.T) {
	failures := []RepairInput{
		{Case: agent.TestCase{ID: "h-1", Target: "/u", Method: "GET", Service: "api"},
			Hint: agent.HintEndpointDrift, Reasoning: "404"},
	}
	calls := []llm.ToolCall{
		{Name: "repair_case", Input: map[string]any{
			"replaces": "h-1", "method": "GET", "path": "/v2/u", "service": "api", "body": `{"b":1}`,
		}},
	}
	out := assembleRepair(calls, failures)
	require.Len(t, out, 1)
	c := out[0]
	assert.Equal(t, "/v2/u", c.Target)
	assert.Equal(t, "GET", c.Method)
	assert.Equal(t, `{"b":1}`, c.Body)
	assert.Empty(t, c.Steps, "HTTP repair has no Steps")
}

// TestBuildRepairPrompt_WSFailureIncludesSteps: for a WS failure (case with
// Steps), the repair prompt renders the failed flow and tells the LLM to emit
// `steps`. Negative: omitting the WS branch omits the steps from the prompt (RED).
func TestBuildRepairPrompt_WSFailureIncludesSteps(t *testing.T) {
	prompt := (&Scout{}).buildRepairPrompt("g", []RepairInput{
		{Case: agent.TestCase{ID: "ws-1", Action: "ws_flow", Target: "wss://x", Service: "ws",
			Expectation: "receives hello",
			Steps: []agent.TestStep{
				{Action: "ws_connect", ConnectionID: "web", URL: "wss://x"},
				{Action: "ws_receive", ConnectionID: "web", Type: "hello"},
			}},
			Hint: agent.HintWsMatch, Reasoning: "decisive receive matched nothing"},
	})
	assert.Contains(t, prompt, "ws-1")
	assert.Contains(t, prompt, "ws_receive", "the failed flow's steps are shown")
	assert.Contains(t, prompt, "steps", "prompt instructs emitting steps for a WS repair")
}

// TestRepairPlan_WSFailure_EndToEnd: the full Scout.RepairPlan path for a WS
// failure. The mock LLM emits a repair_case with corrected `steps`; RepairPlan
// returns a ws_flow TestCase (Steps carried, Replaces bound). This ties the
// prompt, the repair_case tool, and assembleRepair together end-to-end.
func TestRepairPlan_WSFailure_EndToEnd(t *testing.T) {
	mock := llm.NewMockClient(nil)
	mock.SetToolResponse("repairing failed test cases", []llm.ToolCall{
		{Name: "repair_case", Input: map[string]any{
			"replaces":    "ws-1",
			"service":     "ws",
			"expectation": "receives hello",
			"steps": []any{
				map[string]any{"action": "ws_connect", "connection_id": "web", "url": "wss://x"},
				map[string]any{"action": "ws_send", "connection_id": "web", "message": `{"join":"room1"}`},
				map[string]any{"action": "ws_receive", "connection_id": "web", "type": "hello"},
			},
		}},
	})
	driver := ai.NewDriver(mock, ai.NewTokenBudget(200000, 10000))
	sct := NewScout(driver, setupTestStore(t), &project.Config{
		Project: project.ProjectMeta{Name: "ws-repair"},
	}, zap.NewNop())

	failures := []RepairInput{
		{Case: agent.TestCase{ID: "ws-1", Action: "ws_flow", Target: "wss://x", Service: "ws",
			Steps: []agent.TestStep{
				{Action: "ws_connect", ConnectionID: "web"},
				{Action: "ws_receive", ConnectionID: "web", Type: "wrong_type"},
			}},
			Hint: agent.HintWsMatch, Reasoning: "decisive receive matched nothing"},
	}
	out, err := sct.RepairPlan(context.Background(), "g", failures)
	require.NoError(t, err)
	require.Len(t, out, 1)

	c := out[0]
	assert.Equal(t, "ws-1", c.Replaces)
	assert.Equal(t, "ws_flow", c.Action)
	require.Len(t, c.Steps, 3)
	assert.Equal(t, "ws_receive", c.Steps[2].Action)
	assert.Equal(t, "hello", c.Steps[2].Type, "the corrected receive type is carried")
}

// TestAssembleRepair_EmptyStepsFallsBackToHTTP: a repair_case with an empty
// steps array (degenerate emission) does NOT build a ws_flow case — it falls
// back to the HTTP shape. Guards the len(steps) > 0 boundary so a 0-step
// emission never yields an empty-flow WS case.
func TestAssembleRepair_EmptyStepsFallsBackToHTTP(t *testing.T) {
	failures := []RepairInput{
		{Case: agent.TestCase{ID: "x", Target: "/u", Method: "GET"}, Hint: agent.HintShape, Reasoning: "r"},
	}
	calls := []llm.ToolCall{
		{Name: "repair_case", Input: map[string]any{
			"replaces": "x", "method": "GET", "path": "/u", "steps": []any{},
		}},
	}
	out := assembleRepair(calls, failures)
	require.Len(t, out, 1)
	assert.Empty(t, out[0].Steps, "empty steps → HTTP shape, not a ws_flow")
	assert.Equal(t, "GET", out[0].Method)
	assert.NotEqual(t, "ws_flow", out[0].Action)
}

// TestAssembleRepair_DropsMalformedWSSteps: a repair_case whose steps contain
// an action outside {ws_connect, ws_send, ws_receive, ws_disconnect} is dropped
// rather than producing a broken ws_flow. Valid steps are still assembled.
func TestAssembleRepair_DropsMalformedWSSteps(t *testing.T) {
	failures := []RepairInput{
		{Case: agent.TestCase{ID: "ws-1", Action: "ws_flow",
			Steps: []agent.TestStep{{Action: "ws_connect"}}}, Hint: agent.HintWsMatch},
	}
	// Valid steps → replacement produced.
	valid := []llm.ToolCall{{Name: "repair_case", Input: map[string]any{
		"replaces": "ws-1",
		"steps": []any{
			map[string]any{"action": "ws_connect", "connection_id": "web"},
			map[string]any{"action": "ws_receive", "connection_id": "web", "type": "hello"},
		},
	}}}
	out := assembleRepair(valid, failures)
	require.Len(t, out, 1, "valid steps → replacement")

	// One malformed action (typo) → whole emission dropped.
	malformed := []llm.ToolCall{{Name: "repair_case", Input: map[string]any{
		"replaces": "ws-1",
		"steps": []any{
			map[string]any{"action": "ws_connect", "connection_id": "web"},
			map[string]any{"action": "ws_sendx"}, // not a real WS verb
		},
	}}}
	out2 := assembleRepair(malformed, failures)
	assert.Empty(t, out2, "malformed WS step action → emission dropped, not a broken flow")
}

// TestAssembleRepair_InheritsOmittedFields: a repair_case emission that omits
// `service` (and path/method on the HTTP shape) must inherit them from the
// ORIGINAL case — a missing Service detaches the replacement from its protocol
// roles and the executor dies on 'unknown role' (live-observed 2026-08-22 on
// repair-ws-realtime-wf-mission-seed).
func TestAssembleRepair_InheritsOmittedFields(t *testing.T) {
	failures := []RepairInput{
		{Case: agent.TestCase{ID: "ws-mission", Action: "ws_flow",
			Target: "http://localhost:8989/ws/{userId}", Service: "realtime",
			Steps: []agent.TestStep{{Action: "ws_connect", ConnectionID: "web", Role: "web"}}},
			Hint: agent.HintWsMatch, Reasoning: "no progress frame"},
	}
	calls := []llm.ToolCall{
		{Name: "repair_case", Input: map[string]any{
			"replaces":    "ws-mission",
			"expectation": "progress arrives",
			"steps": []any{
				map[string]any{"action": "ws_connect", "connection_id": "web", "role": "web"},
				map[string]any{"action": "ws_receive", "connection_id": "web", "type": "workflow:task_progress"},
			},
		}},
	}
	out := assembleRepair(calls, failures)
	require.Len(t, out, 1)
	assert.Equal(t, "realtime", out[0].Service, "service inherited from the replaced case")

	// HTTP shape: path/method omitted inherit too.
	httpFailures := []RepairInput{
		{Case: agent.TestCase{ID: "http-1", Target: "/api/things", Method: "POST", Service: "api"},
			Hint: agent.HintShape, Reasoning: "404"},
	}
	httpCalls := []llm.ToolCall{
		{Name: "repair_case", Input: map[string]any{"replaces": "http-1", "expectation": "201"}},
	}
	out = assembleRepair(httpCalls, httpFailures)
	require.Len(t, out, 1)
	assert.Equal(t, "api", out[0].Service)
	assert.Equal(t, "/api/things", out[0].Target)
	assert.Equal(t, "POST", out[0].Method)
}

// A WS repair emission whose steps omit every URL (the LLM describes the
// flow but not the dial target) must inherit the ORIGINAL case's target —
// an empty target makes resolveProtocol fail and the replacement dies on
// 'ws connect: unknown role' (live-observed runs 21/23: repair-mission-seed
// verdicts carried target "unknown").
func TestAssembleRepair_WSStepsWithoutURLInheritOriginalTarget(t *testing.T) {
	calls := []llm.ToolCall{{
		Name: "repair_case",
		Input: map[string]any{
			"replaces": "ws-rt-wf-mission-seed",
			// steps deliberately carry no url field anywhere
			"steps": []any{
				map[string]any{"action": "ws_connect", "role": "web", "connection_id": "web"},
				map[string]any{"action": "ws_receive", "type": "workflow:task_failed", "timeout": 600},
			},
			"expectation": "task_failed arrives",
		},
	}}
	original := agent.TestCase{
		ID: "ws-rt-wf-mission-seed", Action: "ws_flow",
		Target: "ws://localhost:8989/ws/{userId}", Service: "realtime",
		Steps: []agent.TestStep{{Action: "ws_connect"}},
	}
	got := assembleRepair(calls, []RepairInput{{Case: original, Hint: agent.HintWsMatch}})
	if len(got) != 1 {
		t.Fatalf("want 1 replacement, got %d", len(got))
	}
	if got[0].Target != original.Target {
		t.Fatalf("replacement target = %q, want inherited %q", got[0].Target, original.Target)
	}
}
