//go:build integration

package agent

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/project"
)

// TestPathCoverage_LiveOpenAgentsRelay proves end-to-end against a LIVE
// open-agents server that a real bridge→web relay — the device:online frame the
// server pushes to web when a bridge joins — yields a >0 message-edge path
// coverage under the receive-driven attribution model.
//
// NOTE on the inline attribution: the path-coverage logic lives in
// internal/session (exercisedEdges), which already imports internal/head/agent,
// so an agent-package test cannot import session (import cycle). This test
// mirrors session.exercisedEdges' RECEIVE-DRIVEN attribution (a matched
// ws_receive of T by role Rr ⇒ the declared edge (From→Rr, T) is exercised) to
// verify the REAL evidence the relay produces is counted. The attribution model
// itself is unit-locked by session's TestExercisedEdges_PushProtocolReceiveDriven;
// this test confirms it sees real >0 coverage — not the constant 0 a send-side
// correlation model yields for this push protocol.
func TestPathCoverage_LiveOpenAgentsRelay(t *testing.T) {
	f := setupOpenAgents(t, false)

	// Minimal bridge-join relay: web + bridge connect to the same /ws/<userId>
	// DO; the bridge joining makes the server push device:online to web.
	tc := &TestCase{
		ID:     "tc-pathcov-relay",
		Target: "ws://localhost:8989/ws/" + f.userId,
		Steps: []TestStep{
			{Action: "ws_connect", Role: "web", ConnectionID: "c-web"},
			{Action: "ws_connect", Role: "bridge", ConnectionID: "c-bridge"},
			{Action: "ws_receive", ConnectionID: "c-web", Type: "device:online", Timeout: 3},
		},
	}
	se := newStepExecutionWithIdx(t, tc, f.wsIdx)
	result := se.runSteps()

	// Representative slice of the real (~64-edge) message surface.
	required := []project.VocabEdge{
		{FromRole: "bridge", ToRole: "web", Type: "device:online", Trigger: "message_handled"},
		{FromRole: "web", ToRole: "bridge", Type: "session:send", Trigger: "message_handled"},
	}

	// --- mirror of session.exercisedEdges (receive-driven); see NOTE above ---
	connRole := map[string]string{}
	for _, s := range tc.Steps {
		if s.Action == "ws_connect" && s.Role != "" {
			connRole[s.ConnectionID] = s.Role
		}
	}
	byToType := map[string][]string{}
	for _, e := range required {
		k := e.FromRole + "|" + e.ToRole + "|" + e.Type
		byToType[e.ToRole+"|"+e.Type] = append(byToType[e.ToRole+"|"+e.Type], k)
	}
	exercised := map[string]bool{}
	for _, ev := range result.Evidence {
		if ev.Action != "ws_receive" || ev.ExpectAbsent || !ev.Matched || ev.MatchedType == "" {
			continue
		}
		recipient := connRole[ev.ConnectionID]
		if recipient == "" {
			continue
		}
		for _, k := range byToType[recipient+"|"+ev.MatchedType] {
			exercised[k] = true
		}
	}
	// --- end mirror ---

	pct := float64(len(exercised)) / float64(len(required))
	t.Logf("live relay: evidence=%d exercised=%v path_coverage=%.3f", len(result.Evidence), exercised, pct)

	require.Contains(t, exercised, "bridge|web|device:online",
		"real relay must exercise bridge->web device:online under receive-driven attribution; evidence=%v", result.Evidence)
	require.Greater(t, pct, 0.0,
		"path coverage must be >0 from the live relay (a send-side model yields 0 for this push protocol)")
}
