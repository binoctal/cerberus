//go:build integration

package agent

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/types"
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
//
// Gap E prerequisite (TestOrchestratorCallback only): open-agents must route its
// DO callback to the capture server. wrangler does NOT read shell env prefixes,
// so use one of:
//   - add API_BASE_URL = "http://127.0.0.1:9099" to apps/api/.dev.vars, or
//   - wrangler dev --var API_BASE_URL:http://127.0.0.1:9099 --port 8989
//
// The test skips (not fails) if port 9099 is unavailable or no callback arrives.
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

// TestAuthErrorPaths asserts the DO/Worker reject bad connects. Each row dials
// a raw URL (Role empty, nil wsIdx) so the fixture's protocol does NOT inject a
// good token. Hard-assert: the connect is rejected (StepFailed). Best-effort:
// the dial error string usually carries the HTTP status (400/401).
func TestAuthErrorPaths(t *testing.T) {
	f := setupOpenAgents(t, false) // provisions userId; its wsIdx is NOT used below.
	cases := []struct {
		name string
		url  string // raw dial URL with the bad param
	}{
		{"invalid_type", fmt.Sprintf("ws://localhost:8989/ws/%s?type=invalid&token=demo_token", f.userId)},
		{"bridge_no_deviceId", fmt.Sprintf("ws://localhost:8989/ws/%s?type=bridge&token=%s", f.userId, "token_"+strings.Repeat("0", 32))},
		{"missing_token", fmt.Sprintf("ws://localhost:8989/ws/%s?type=web", f.userId)},
		{"bad_bridge_token", fmt.Sprintf("ws://localhost:8989/ws/%s?type=bridge&deviceId=%s&token=token_wrong", f.userId, f.deviceId)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tc := &TestCase{
				ID:     "tc-auth-" + c.name,
				Target: c.url,
				Steps: []TestStep{
					{Action: "ws_connect", ConnectionID: "c-bad"}, // no Role, no protocol → raw dial
				},
			}
			// nil wsIdx: resolveProtocol short-circuits, no auth injection.
			se := newStepExecutionWithIdx(t, tc, nil)
			result := se.runSteps()
			require.Equal(t, StepFailed, result.Status, "connect %q unexpectedly succeeded", c.name)
			// Best-effort: surface the dial error (often contains the HTTP status).
			if ws, ok := result.Result.(types.WSResult); ok {
				t.Logf("dial error for %s: %s", c.name, ws.Err)
			}
		})
	}
}

// TestOrchestratorCallback asserts the DO's notifyOrchestrator side effect
// (room.ts:326-338) for the three triggers (room.ts:217): task_progress,
// task_result, task_error. Per row: bridge sends the trigger, then the capture
// server must observe a POST to /api/missions/internal/orchestrator/event.
func TestOrchestratorCallback(t *testing.T) {
	f := setupOpenAgents(t, true) // starts capture server on :9099 (skips if unavailable)
	triggers := []string{"workflow:task_progress", "workflow:task_result", "workflow:task_error"}
	for _, typ := range triggers {
		t.Run(typ, func(t *testing.T) {
			f.capture.reset()
			tc := &TestCase{
				ID:     "tc-cb-" + typ,
				Target: "ws://localhost:8989/ws/" + f.userId,
				Steps: []TestStep{
					{Action: "ws_connect", Role: "web", ConnectionID: "c-web"},
					{Action: "ws_connect", Role: "bridge", ConnectionID: "c-bridge"},
					{Action: "ws_receive", ConnectionID: "c-web", Type: "device:online", Timeout: 3},
					{Action: "ws_send", ConnectionID: "c-bridge",
						Message: fmt.Sprintf(`{"type":%q,"payload":{"deviceId":%q}}`, typ, f.deviceId)},
				},
			}
			se := newStepExecutionWithIdx(t, tc, f.wsIdx)
			res := se.runSteps()
			require.Equal(t, StepPassed, res.Status, "trigger send failed for %s", typ)

			got, ok := f.capture.awaitPOST("/api/missions/internal/orchestrator/event", typ, 3*time.Second)
			if !ok {
				t.Skipf("no orchestrator callback captured for %s within timeout — "+
					"is API_BASE_URL pointed at the capture server? (see gap E prerequisite)", typ)
			}
			t.Logf("captured callback for %s: %s", typ, got.Body)
		})
	}
}

// TestSenderExclusionProbeLive verifies, on the real open-agents server, the
// exact probe step Scout's wsRelayCases now appends for the joining peer: after
// the relay delivers device:online to web, the BRIDGE (the joiner / "sender" of
// the join event) must NOT receive its own join signal. The ExpectAbsent receive
// inverts success — a timeout (no frame) is the probe's PASS. This is the live,
// deterministic proof that the sender-exclusion probe behaves correctly against
// a real service (Task 2 inversion + Task 5 emitted step), complementary to the
// synthetic-evidence Examiner validation.
func TestSenderExclusionProbeLive(t *testing.T) {
	f := setupOpenAgents(t, false)
	tc := &TestCase{
		ID:     "tc-sender-exclusion-probe",
		Target: "ws://localhost:8989/ws/" + f.userId,
		Steps: []TestStep{
			{Action: "ws_connect", Role: "web", ConnectionID: "c-web"},
			{Action: "ws_connect", Role: "bridge", ConnectionID: "c-bridge"},
			// Relay: web receives device:online when bridge joins.
			{Action: "ws_receive", ConnectionID: "c-web", Type: "device:online", Timeout: 3},
			// Probe (the step wsRelayCases appends per joining peer): bridge must
			// NOT receive its own join signal. ExpectAbsent ⇒ timeout is success,
			// an actual echo is failure.
			{Action: "ws_receive", ConnectionID: "c-bridge", Type: "device:online",
				Timeout: 2, ExpectAbsent: true},
		},
	}
	se := newStepExecutionWithIdx(t, tc, f.wsIdx)
	result := se.runSteps()
	for _, ev := range result.Evidence {
		t.Logf("step: %s", ev.Content)
	}
	require.Equal(t, StepPassed, result.Status,
		"relay must deliver device:online to web AND the bridge probe must time out (sender excluded)")
}
