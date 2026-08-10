//go:build integration

package agent

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/project"
)

// exercisedEdgesMirror mirrors session.exercisedEdges' RECEIVE-DRIVEN
// attribution over a TestCase's observed evidence: a matched, non-ExpectAbsent
// ws_receive of type T by the recipient role Rr ⇒ the declared edge
// (From→Rr, T) is exercised. It builds the connection-id → role map from
// tc.Steps (ws_connect steps carry Role) and attributes receives to required
// edges via (ToRole, Type).
//
// The path-coverage logic lives in internal/session (exercisedEdges), which
// already imports internal/head/agent, so an agent-package test cannot import
// session (import cycle). This mirror lets agent-package live tests verify the
// REAL evidence a relay produces is counted. The attribution model itself is
// unit-locked by session's TestExercisedEdges_PushProtocolReceiveDriven.
func exercisedEdgesMirror(tc *TestCase, evidence []Evidence, required []project.VocabEdge) map[string]bool {
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
	for _, ev := range evidence {
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
	return exercised
}

// TestPathCoverage_LiveOpenAgentsRelay proves end-to-end against a LIVE
// open-agents server that a real bridge→web relay — the device:online frame the
// server pushes to web when a bridge joins — yields a >0 message-edge path
// coverage under the receive-driven attribution model.
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

	exercised := exercisedEdgesMirror(tc, result.Evidence, required)
	pct := float64(len(exercised)) / float64(len(required))
	t.Logf("live relay: evidence=%d exercised=%v path_coverage=%.3f", len(result.Evidence), exercised, pct)

	require.Contains(t, exercised, "bridge|web|device:online",
		"real relay must exercise bridge->web device:online under receive-driven attribution; evidence=%v", result.Evidence)
	require.Greater(t, pct, 0.0,
		"path coverage must be >0 from the live relay (a send-side model yields 0 for this push protocol)")
}

// TestPathCoverage_LiveTwoRoleExchange proves a REAL open-agents two-role
// message_handled exchange (web sends session:start, bridge receives and replies
// session:created, web receives it) exercises a message_handled edge under the
// receive-driven attribution ⇒ pathCoverage > 0. The web's session:start needs
// the bridge's deviceId, so the send carries f.deviceId.
func TestPathCoverage_LiveTwoRoleExchange(t *testing.T) {
	f := setupOpenAgents(t, false)
	tc := &TestCase{
		ID:     "tc-tworoletexchange",
		Target: "ws://localhost:8989/ws/" + f.userId,
		Steps: []TestStep{
			{Action: "ws_connect", Role: "web", ConnectionID: "web"},
			{Action: "ws_connect", Role: "bridge", ConnectionID: "bridge"},
			{Action: "ws_receive", ConnectionID: "web", Type: "device:online", Timeout: 3},
			{Action: "ws_send", ConnectionID: "web", Message: fmt.Sprintf(`{"type":"session:start","payload":{"deviceId":%q}}`, f.deviceId)},
			{Action: "ws_receive", ConnectionID: "bridge", Type: "session:start", Timeout: 3},
			{Action: "ws_send", ConnectionID: "bridge", Message: `{"type":"session:created"}`},
			{Action: "ws_receive", ConnectionID: "web", Type: "session:created", Timeout: 3},
		},
	}
	se := newStepExecutionWithIdx(t, tc, f.wsIdx)
	result := se.runSteps()
	require.Equal(t, StepPassed, result.Status, "two-role exchange must complete; evidence=%v", result.Evidence)

	// Representative required surface including both message_handled edges.
	required := []project.VocabEdge{
		{FromRole: "web", ToRole: "bridge", Type: "session:start", Trigger: "message_handled"},
		{FromRole: "bridge", ToRole: "web", Type: "session:created", Trigger: "message_handled"},
		{FromRole: "bridge", ToRole: "web", Type: "device:online", Trigger: "message_handled"},
	}
	exercised := exercisedEdgesMirror(tc, result.Evidence, required)
	pct := float64(len(exercised)) / float64(len(required))
	t.Logf("two-role exchange: exercised=%v path_coverage=%.3f", exercised, pct)
	// session:start (web→bridge) is exercised (bridge received it); session:created
	// (bridge→web) is exercised (web received it). device:online may or may not be
	// in the real required surface; assert the two message_handled exchange edges.
	require.Contains(t, exercised, "web|bridge|session:start")
	require.Contains(t, exercised, "bridge|web|session:created")
	require.Greater(t, pct, 0.0)
}

// TestPathCoverage_LiveSendBodyTemplating proves the send-time placeholder
// resolver: a ws_send body carrying the literal {{bridge.deviceId}} placeholder
// is resolved against the bridge actor's provisioned path params at send time,
// so open-agents' DO relays session:start to the bridge (room.ts gates on
// payload.deviceId) and BOTH exchange directions complete. Distinct from
// TestPathCoverage_LiveTwoRoleExchange, which hand-injects the deviceId.
func TestPathCoverage_LiveSendBodyTemplating(t *testing.T) {
	f := setupOpenAgents(t, false)
	// setupOpenAgents populates ActorTokens but NOT ActorPathParams; the resolver
	// reads ActorPathParams, so seed the bridge's provisioned device id there.
	f.wsIdx.ActorPathParams = map[string]map[string]string{
		"web-actor":    {"userId": f.userId},
		"bridge-actor": {"deviceId": f.deviceId, "userId": f.userId},
	}
	tc := &TestCase{
		ID:     "tc-sendbody-template",
		Target: "ws://localhost:8989/ws/" + f.userId,
		Steps: []TestStep{
			{Action: "ws_connect", Role: "web", ConnectionID: "web"},
			{Action: "ws_connect", Role: "bridge", ConnectionID: "bridge"},
			{Action: "ws_receive", ConnectionID: "web", Type: "device:online", Timeout: 3},
			// Literal placeholder in the body; resolved to f.deviceId at send time.
			{Action: "ws_send", ConnectionID: "web", Message: `{"type":"session:start","payload":{"deviceId":"{{bridge.deviceId}}"}}`},
			{Action: "ws_receive", ConnectionID: "bridge", Type: "session:start", Timeout: 3},
			{Action: "ws_send", ConnectionID: "bridge", Message: `{"type":"session:created"}`},
			{Action: "ws_receive", ConnectionID: "web", Type: "session:created", Timeout: 3},
		},
	}
	se := newStepExecutionWithIdx(t, tc, f.wsIdx)
	result := se.runSteps()
	require.Equal(t, StepPassed, result.Status, "templated send must resolve and the exchange must complete; evidence=%v", result.Evidence)

	required := []project.VocabEdge{
		{FromRole: "web", ToRole: "bridge", Type: "session:start", Trigger: "message_handled"},
		{FromRole: "bridge", ToRole: "web", Type: "session:created", Trigger: "message_handled"},
	}
	exercised := exercisedEdgesMirror(tc, result.Evidence, required)
	require.Contains(t, exercised, "web|bridge|session:start", "deviceId resolved ⇒ DO relayed session:start to bridge")
	require.Contains(t, exercised, "bridge|web|session:created", "bridge replied and web received it")
}
