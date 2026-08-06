package scout

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
)

func relayProtocol() *project.Protocol {
	return &project.Protocol{TypePath: "type",
		Roles: map[string]*project.ProtocolRole{
			"web":    {Params: map[string]string{"type": "web"}, Handshake: &project.RoleHandshake{AwaitType: "device:online", Optional: true, Timeout: 2}},
			"bridge": {Params: map[string]string{"type": "bridge"}},
		}}
}

// TestWSCasesCovered_NilEqualsWSCases asserts backwards compatibility:
// covered=nil reproduces the old WSCases output exactly, and a covered role is
// skipped (no cases emitted for it).
func TestWSCasesCovered_NilEqualsWSCases(t *testing.T) {
	cfg := &project.Config{Services: []project.Service{{Name: "rt", URL: "ws://h/ws", Protocol: relayProtocol()}}}
	require.Equal(t, WSCases(cfg, "send device:command receive device:ack"),
		WSCasesCovered(cfg, "send device:command receive device:ack", nil, nil))

	// Covered role is skipped.
	got := WSCasesCovered(cfg, "send device:command receive device:ack",
		map[string]map[string]bool{"rt": {"web": true}}, nil)
	for _, c := range got {
		require.NotContains(t, c.ID, "-web-", "web role covered -> no web cases emitted")
	}
}

// TestAugmentPlanComposition_AssembledRelay verifies the post-migration
// augmentPlan composition (no LLM): assemblePlan turns begin_case + ws_* tool
// calls into a multi-connection Steps case and reports the covered roles, which
// WSCasesCovered then suppresses. This is the deterministic core previously
// validated by expandWSRelayCases; the LLM-emission side is covered by
// TestAssemblePlan_WSRelaySequence and the live relay probe.
func TestAugmentPlanComposition_AssembledRelay(t *testing.T) {
	cfg := &project.Config{Services: []project.Service{{Name: "rt", URL: "ws://h/ws", Protocol: relayProtocol()}}}
	calls := []llm.ToolCall{
		{Name: "begin_case", Input: map[string]any{"name": "relay", "expectation": "relay works", "service": "rt"}},
		{Name: "ws_connect", Input: map[string]any{"role": "web"}},
		{Name: "ws_connect", Input: map[string]any{"role": "bridge"}},
		{Name: "ws_receive", Input: map[string]any{"role": "web", "type": "device:online"}},
	}

	plan, covered, _, _ := assemblePlan(calls, "goal", "ws://h/ws", cfg.Services)

	require.Len(t, plan.Cases, 1, "single ws_flow case from begin_case+ws_*")
	require.NotEmpty(t, plan.Cases[0].Steps)
	require.Equal(t, "ws_connect", plan.Cases[0].Steps[0].Action)
	require.Equal(t, "web", plan.Cases[0].Steps[0].ConnectionID)
	require.True(t, covered["rt"]["web"] && covered["rt"]["bridge"])

	// WSCasesCovered with the real covered map emits no web/bridge cases.
	for _, c := range WSCasesCovered(cfg, "receive devices:sync", covered, nil) {
		require.NotContains(t, c.ID, "-web-")
		require.NotContains(t, c.ID, "-bridge-")
	}
}

// TestAssemblePlan_WSConnectURL verifies the per-step ws_connect url is captured
// onto TestStep.URL so a case can dial peers at different endpoints than the
// begin_case service. A missing url leaves URL empty (stepToAction falls back to
// tc.Target); a present url is surfaced verbatim.
func TestAssemblePlan_WSConnectURL(t *testing.T) {
	cfg := &project.Config{Services: []project.Service{{Name: "rt", URL: "ws://h/ws", Protocol: relayProtocol()}}}
	calls := []llm.ToolCall{
		{Name: "begin_case", Input: map[string]any{"name": "relay", "expectation": "ok", "service": "rt"}},
		{Name: "ws_connect", Input: map[string]any{"role": "web"}},
		{Name: "ws_connect", Input: map[string]any{"role": "bridge", "url": "ws://other-host/ws"}},
	}

	plan, _, _, _ := assemblePlan(calls, "goal", "ws://h/ws", cfg.Services)
	require.Len(t, plan.Cases, 1, "single ws_flow case")
	steps := plan.Cases[0].Steps
	require.Len(t, steps, 2)

	require.Equal(t, "web", steps[0].Role)
	require.Empty(t, steps[0].URL, "omitted url leaves TestStep.URL empty (falls back to tc.Target)")

	require.Equal(t, "bridge", steps[1].Role)
	require.Equal(t, "ws://other-host/ws", steps[1].URL, "emitted url must populate TestStep.URL")
}

// TestAssemblePlan_UnsoundWSFlowDoesNotCover is the A1 residual-risk fix: an
// LLM ws_flow that connects a role but receives an INVENTED (ungrounded) type is
// unsound, so the role is NOT marked covered — WSCasesCovered still emits the
// deterministic fallback for it. A sound case (grounded receive) still covers.
func TestAssemblePlan_UnsoundWSFlowDoesNotCover(t *testing.T) {
	cfg := &project.Config{Services: []project.Service{{Name: "rt", URL: "ws://h/ws", Protocol: relayProtocol()}}}

	sound := []llm.ToolCall{
		{Name: "begin_case", Input: map[string]any{"name": "relay", "expectation": "ok", "service": "rt"}},
		{Name: "ws_connect", Input: map[string]any{"role": "web"}},
		{Name: "ws_receive", Input: map[string]any{"role": "web", "type": "device:online"}}, // grounded (web await_type)
	}
	_, coveredSound, _, _ := assemblePlan(sound, "goal", "ws://h/ws", cfg.Services)
	assert.True(t, coveredSound["rt"]["web"], "grounded receive -> web covered")

	unsound := []llm.ToolCall{
		{Name: "begin_case", Input: map[string]any{"name": "relay", "expectation": "ok", "service": "rt"}},
		{Name: "ws_connect", Input: map[string]any{"role": "web"}},
		{Name: "ws_receive", Input: map[string]any{"role": "web", "type": "message"}}, // invented
	}
	planUnsound, coveredUnsound, _, _ := assemblePlan(unsound, "goal", "ws://h/ws", cfg.Services)
	assert.False(t, coveredUnsound["rt"]["web"], "invented receive -> web NOT covered (unsound)")
	// Policy: the unsound LLM case itself stays in the plan.
	assert.Len(t, planUnsound.Cases, 1, "unsound LLM case is kept, not dropped")

	// Residual-risk proof: unsound coverage keeps web's deterministic fallback.
	// (web has an optional handshake in relayProtocol, so the fallback is the
	// deterministic relay case, which connects web.)
	connectsWeb := func(covered map[string]map[string]bool) bool {
		for _, c := range WSCasesCovered(cfg, "receive devices:sync", covered, nil) {
			for _, st := range c.Steps {
				if st.Action == "ws_connect" && st.Role == "web" {
					return true
				}
			}
		}
		return false
	}
	assert.False(t, connectsWeb(coveredSound), "sound coverage suppresses web fallback")
	assert.True(t, connectsWeb(coveredUnsound), "unsound coverage keeps web fallback (not stranded)")
}

// TestAssemblePlan_RecordsCoveringCase is the A1 Phase 2 producer: when a
// sound LLM ws_flow covers a role, assemblePlan records that case's ID in
// coveringCase, so WSCasesCovered (Task 3) can emit a lazy fallback bound to
// it. An unsound case still records nothing (Phase 1 behavior).
func TestAssemblePlan_RecordsCoveringCase(t *testing.T) {
	cfg := &project.Config{Services: []project.Service{{Name: "rt", URL: "ws://h/ws", Protocol: relayProtocol()}}}

	sound := []llm.ToolCall{
		{Name: "begin_case", Input: map[string]any{"name": "relay", "expectation": "ok", "service": "rt"}},
		{Name: "ws_connect", Input: map[string]any{"role": "web"}},
		{Name: "ws_receive", Input: map[string]any{"role": "web", "type": "device:online"}}, // grounded
	}
	plan, _, covering, _ := assemblePlan(sound, "goal", "ws://h/ws", cfg.Services)
	require.NotEmpty(t, plan.Cases)
	primaryID := plan.Cases[0].ID
	assert.Equal(t, primaryID, covering["rt"]["web"], "sound case ID recorded as web's coverer")

	unsound := []llm.ToolCall{
		{Name: "begin_case", Input: map[string]any{"name": "relay", "expectation": "ok", "service": "rt"}},
		{Name: "ws_connect", Input: map[string]any{"role": "web"}},
		{Name: "ws_receive", Input: map[string]any{"role": "web", "type": "message"}}, // invented
	}
	_, _, coveringUnsound, _ := assemblePlan(unsound, "goal", "ws://h/ws", cfg.Services)
	assert.Empty(t, coveringUnsound["rt"]["web"], "unsound case records no coverer")
}

// TestWSCasesCovered_LazyFallbackForCoveredReceiver is the A1 Phase 2 emitter:
// a relay receiver covered by a sound LLM case gets a lazy deterministic
// fallback bound to that case (FallbackFor set, Priority<0), not a normal
// case and not a drop. An uncovered receiver still emits a normal relay case.
func TestWSCasesCovered_LazyFallbackForCoveredReceiver(t *testing.T) {
	cfg := &project.Config{Services: []project.Service{{Name: "rt", URL: "ws://h/ws", Protocol: relayProtocol()}}}
	covered := map[string]map[string]bool{"rt": {"web": true}}
	coveringCase := map[string]map[string]string{"rt": {"web": "tc-l lm-primary"}}

	cases := WSCasesCovered(cfg, "receive devices:sync", covered, coveringCase)

	// web is the relay receiver (optional handshake await_type device:online in
	// relayProtocol). Find the case whose receiver (first connect step) is web.
	var webRelay *agent.TestCase
	for i := range cases {
		c := &cases[i]
		if len(c.Steps) > 0 && c.Steps[0].Action == "ws_connect" && c.Steps[0].Role == "web" {
			webRelay = c
			break
		}
	}
	require.NotNil(t, webRelay, "web relay case present")
	assert.Equal(t, "tc-l lm-primary", webRelay.FallbackFor, "bound to the covering case")
	assert.Less(t, webRelay.Priority, 0.0, "lazy fallback is deprioritized")
	assert.NotEmpty(t, webRelay.Steps, "fallback carries the deterministic relay steps")

	// Sanity: a receiver with no coverer is emitted as a normal case (no FallbackFor).
	coveringNone := map[string]map[string]string{"rt": {}}
	casesNone := WSCasesCovered(cfg, "receive devices:sync", map[string]map[string]bool{"rt": {}}, coveringNone)
	for i := range casesNone {
		assert.Empty(t, casesNone[i].FallbackFor, "uncovered receiver has no FallbackFor")
		assert.GreaterOrEqual(t, casesNone[i].Priority, 0.0, "normal case is not deprioritized")
	}
}

// TestWsRelayCaseAppendsSenderExclusionProbe verifies the deterministic
// peer-join relay case ends with a negative-receive (ExpectAbsent) probe for
// each joining peer, asserting that peer does not receive its own join signal.
// The examiner turns the probe outcome into a measured Dimension.Excluded.
func TestWsRelayCaseAppendsSenderExclusionProbe(t *testing.T) {
	svc := project.Service{Name: "rt", URL: "ws://h/ws", Protocol: relayProtocol()}
	cases, _, _ := wsRelayCases(svc)
	require.NotEmpty(t, cases, "relay protocol should emit a relay case")

	// web is the receiver (optional handshake await device:online); bridge is the
	// joining peer whose own join signal it must NOT receive.
	var probes []agent.TestStep
	for _, c := range cases {
		for _, s := range c.Steps {
			if s.Action == "ws_receive" && s.ExpectAbsent {
				probes = append(probes, s)
			}
		}
	}
	require.NotEmpty(t, probes, "relay case must include >=1 ExpectAbsent probe step")
	for _, p := range probes {
		assert.Equal(t, "bridge", p.ConnectionID, "probe targets the joining peer")
		assert.Equal(t, "device:online", p.Type, "probe targets the join signal type")
		assert.Greater(t, p.Timeout, 0, "probe timeout must be bounded")
	}
}
