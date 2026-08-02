//go:build integration

package agent

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestRunStepsMultiConnectionOpenAgents dogfoods cerberus's multi-connection
// orchestration against a live open-agents target. Build-tagged out of `make
// test`. To run:
//
//	fnm use 22 && cd ../open-agents/apps/api && npm run dev   # serves :8989
//	go test -tags integration -run TestRunStepsMultiConnectionOpenAgents ./internal/head/agent/
//
// Hard asserts are capability-level: cerberus must establish two real sockets
// (web + bridge) to the SAME /ws/<userId> DO (both connects succeed). Exact
// protocol matching (devices:sync push, session:start->session:created relay)
// is BEST-EFFORT: open-agents' relay vocabulary is discovered at run time, so a
// mismatch is a dogfood finding, not a cerberus regression (the deterministic
// TestRunStepsMultiConnection is the mechanical proof).
func TestRunStepsMultiConnectionOpenAgents(t *testing.T) {
	f := setupOpenAgents(t, false)

	tc := &TestCase{
		ID:     "tc-openagents-relay",
		Target: "ws://localhost:8989/ws/" + f.userId,
		Steps: []TestStep{
			{Action: "ws_connect", Role: "web", ConnectionID: "c-web"},
			{Action: "ws_connect", Role: "bridge", ConnectionID: "c-bridge"},
			{Action: "ws_receive", ConnectionID: "c-web", Type: "device:online", Timeout: 3},
			{Action: "ws_send", ConnectionID: "c-web", Message: `{"type":"session:start"}`},
			{Action: "ws_receive", ConnectionID: "c-web", Type: "session:created", Timeout: 3},
		},
	}

	se := newStepExecutionWithIdx(t, tc, f.wsIdx)
	result := se.runSteps()
	for _, ev := range result.Evidence {
		t.Logf("step evidence: %s", ev.Content)
	}
	require.GreaterOrEqual(t, len(result.Evidence), 3,
		"both connects must succeed (web + bridge); evidence=%d", len(result.Evidence))
	if result.Status == StepPassed {
		t.Logf("relay case fully passed: status=%s", result.Status)
	} else {
		t.Logf("relay case did not fully pass (dogfood finding): status=%s", result.Status)
	}
}

// TestBridgeToWebRelay dogfoods the DO's Bridge→Web broadcast for every relay
// type in room.ts:178-220. Per row: connect web+bridge, bridge sends, web must
// receive the same type. Hard-assert: the relayed frame arrives.
func TestBridgeToWebRelay(t *testing.T) {
	f := setupOpenAgents(t, false)
	rows := []string{
		"encrypted", "session:created", "session:started", "session:output",
		"session:stopped", "session:error", "session:message", "session:status",
		"chat:response", "chat:thought", "chat:permission", "permission:request",
		"acp:status", "acp:output", "acp:tool_call", "acp:tool_result",
		"agent:status", "tool:call", "session:usage",
		"multiagent:task_started", "multiagent:task_completed",
		"multiagent:task_failed", "multiagent:job_completed",
		// task_progress/task_result/task_error are Bridge→Web relays AND trigger
		// notifyOrchestrator (gap E). Listed here so their web relay is covered;
		// with API_BASE_URL unset the fetch is a no-op (room.ts:329), and with it
		// set the fetch misses (no capture server in gap A) and .catch swallows it.
		"multiagent:task_progress", "multiagent:task_result", "multiagent:task_error",
		"prompts:synced", "mcp:synced", "mcp:list_response",
		"config:synced", "rules:synced", "storage:synced",
	}
	for _, typ := range rows {
		t.Run(typ, func(t *testing.T) {
			tc := &TestCase{
				ID:     "tc-b2w-" + typ,
				Target: "ws://localhost:8989/ws/" + f.userId,
				Steps: []TestStep{
					{Action: "ws_connect", Role: "web", ConnectionID: "c-web"},
					{Action: "ws_connect", Role: "bridge", ConnectionID: "c-bridge"},
					{Action: "ws_receive", ConnectionID: "c-web", Type: "device:online", Timeout: 3},
					{Action: "ws_send", ConnectionID: "c-bridge", Message: fmt.Sprintf(`{"type":%q}`, typ)},
					{Action: "ws_receive", ConnectionID: "c-web", Type: typ, Timeout: 3},
				},
			}
			se := newStepExecutionWithIdx(t, tc, f.wsIdx)
			result := se.runSteps()
			for _, ev := range result.Evidence {
				t.Logf("%s", ev.Content)
			}
			require.Equal(t, StepPassed, result.Status,
				"bridge→web relay for %q did not pass (dogfood finding)", typ)
		})
	}
}
