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
