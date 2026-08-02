//go:build integration

package agent

import (
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
