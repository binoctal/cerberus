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

// TestWebToBridgeRouting dogfoods the DO's Web→Bridge routing for every routed
// type in room.ts:224-252. Per row: connect web+bridge, web sends with
// payload.deviceId, bridge must receive the same type. Hard-assert: the routed
// frame reaches the bridge.
func TestWebToBridgeRouting(t *testing.T) {
	f := setupOpenAgents(t, false)
	rows := []string{
		"session:send", "session:stop", "session:resize", "chat:send",
		"permission:response", "control:takeover", "config:sync", "rules:sync",
		"storage:sync", "prompts:sync", "mcp:sync", "mcp:list",
		"multiagent:start_job", "multiagent:pause_job", "multiagent:cancel_job",
		"multiagent:start_task", "multiagent:task_assign", "acp:query_status",
	}
	for _, typ := range rows {
		t.Run(typ, func(t *testing.T) {
			tc := &TestCase{
				ID:     "tc-w2b-" + typ,
				Target: "ws://localhost:8989/ws/" + f.userId,
				Steps: []TestStep{
					{Action: "ws_connect", Role: "web", ConnectionID: "c-web"},
					{Action: "ws_connect", Role: "bridge", ConnectionID: "c-bridge"},
					{Action: "ws_receive", ConnectionID: "c-web", Type: "device:online", Timeout: 3},
					{Action: "ws_send", ConnectionID: "c-web",
						Message: fmt.Sprintf(`{"type":%q,"payload":{"deviceId":%q}}`, typ, f.deviceId)},
					{Action: "ws_receive", ConnectionID: "c-bridge", Type: typ, Timeout: 3},
				},
			}
			se := newStepExecutionWithIdx(t, tc, f.wsIdx)
			result := se.runSteps()
			for _, ev := range result.Evidence {
				t.Logf("%s", ev.Content)
			}
			require.Equal(t, StepPassed, result.Status,
				"web→bridge routing for %q did not pass (dogfood finding)", typ)
		})
	}
}

// TestSessionStartRoundTrip proves the Web→Bridge→Web chain end-to-end: web
// sends session:start with payload.deviceId, bridge receives it, bridge replies
// session:created, web receives session:created. Closes the original Finding 4.
func TestSessionStartRoundTrip(t *testing.T) {
	f := setupOpenAgents(t, false)
	tc := &TestCase{
		ID:     "tc-session-roundtrip",
		Target: "ws://localhost:8989/ws/" + f.userId,
		Steps: []TestStep{
			{Action: "ws_connect", Role: "web", ConnectionID: "c-web"},
			{Action: "ws_connect", Role: "bridge", ConnectionID: "c-bridge"},
			{Action: "ws_receive", ConnectionID: "c-web", Type: "device:online", Timeout: 3},
			{Action: "ws_send", ConnectionID: "c-web",
				Message: fmt.Sprintf(`{"type":"session:start","payload":{"deviceId":%q}}`, f.deviceId)},
			{Action: "ws_receive", ConnectionID: "c-bridge", Type: "session:start", Timeout: 3},
			{Action: "ws_send", ConnectionID: "c-bridge", Message: `{"type":"session:created"}`},
			{Action: "ws_receive", ConnectionID: "c-web", Type: "session:created", Timeout: 3},
		},
	}
	se := newStepExecutionWithIdx(t, tc, f.wsIdx)
	result := se.runSteps()
	for _, ev := range result.Evidence {
		t.Logf("%s", ev.Content)
	}
	require.Equal(t, StepPassed, result.Status, "session:start round trip did not complete")
}

// TestLifecycleSignals covers three DO lifecycle paths: device:offline on
// bridge disconnect (room.ts:154-160), sendToBridge silent-drop on an unknown
// deviceId (room.ts:295), and broadcastToWeb fan-out to two web clients
// (room.ts:269).
func TestLifecycleSignals(t *testing.T) {
	f := setupOpenAgents(t, false)
	t.Run("device_offline_on_disconnect", func(t *testing.T) {
		tc := &TestCase{
			ID:     "tc-offline",
			Target: "ws://localhost:8989/ws/" + f.userId,
			Steps: []TestStep{
				{Action: "ws_connect", Role: "web", ConnectionID: "c-web"},
				{Action: "ws_connect", Role: "bridge", ConnectionID: "c-bridge"},
				{Action: "ws_receive", ConnectionID: "c-web", Type: "device:online", Timeout: 3},
				{Action: "ws_disconnect", ConnectionID: "c-bridge"},
				{Action: "ws_receive", ConnectionID: "c-web", Type: "device:offline", Timeout: 3},
			},
		}
		se := newStepExecutionWithIdx(t, tc, f.wsIdx)
		result := se.runSteps()
		require.Equal(t, StepPassed, result.Status, "device:offline not relayed on bridge disconnect")
	})

	t.Run("sendToBridge_miss_silent_drop", func(t *testing.T) {
		// Web sends a routed type with an UNKNOWN deviceId; sendToBridge finds no
		// socket and drops silently. Assert the bridge receives nothing: the case
		// FAILS on the receive step (no frame within timeout) — that failure IS the
		// proof of the silent drop, so we invert the assertion.
		tc := &TestCase{
			ID:     "tc-miss",
			Target: "ws://localhost:8989/ws/" + f.userId,
			Steps: []TestStep{
				{Action: "ws_connect", Role: "web", ConnectionID: "c-web"},
				{Action: "ws_connect", Role: "bridge", ConnectionID: "c-bridge"},
				{Action: "ws_receive", ConnectionID: "c-web", Type: "device:online", Timeout: 3},
				{Action: "ws_send", ConnectionID: "c-web",
					Message: `{"type":"session:send","payload":{"deviceId":"device_does_not_exist"}}`},
				{Action: "ws_receive", ConnectionID: "c-bridge", Type: "session:send", Timeout: 1},
			},
		}
		se := newStepExecutionWithIdx(t, tc, f.wsIdx)
		result := se.runSteps()
		require.NotEqual(t, StepPassed, result.Status,
			"bridge unexpectedly received a routed frame for an unknown deviceId (drop did not happen)")
	})

	t.Run("fanout_two_web_clients", func(t *testing.T) {
		tc := &TestCase{
			ID:     "tc-fanout",
			Target: "ws://localhost:8989/ws/" + f.userId,
			Steps: []TestStep{
				{Action: "ws_connect", Role: "web", ConnectionID: "c-web-1"},
				{Action: "ws_connect", Role: "web", ConnectionID: "c-web-2"},
				{Action: "ws_connect", Role: "bridge", ConnectionID: "c-bridge"},
				// device:online is broadcast to BOTH web clients.
				{Action: "ws_receive", ConnectionID: "c-web-1", Type: "device:online", Timeout: 3},
				{Action: "ws_receive", ConnectionID: "c-web-2", Type: "device:online", Timeout: 3},
			},
		}
		se := newStepExecutionWithIdx(t, tc, f.wsIdx)
		result := se.runSteps()
		require.Equal(t, StepPassed, result.Status, "broadcastToWeb did not reach both web clients")
	})
}
