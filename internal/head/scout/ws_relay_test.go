package scout

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

	plan, covered, _ := assemblePlan(calls, "goal", "ws://h/ws", cfg.Services)

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
	_, coveredSound, _ := assemblePlan(sound, "goal", "ws://h/ws", cfg.Services)
	assert.True(t, coveredSound["rt"]["web"], "grounded receive -> web covered")

	unsound := []llm.ToolCall{
		{Name: "begin_case", Input: map[string]any{"name": "relay", "expectation": "ok", "service": "rt"}},
		{Name: "ws_connect", Input: map[string]any{"role": "web"}},
		{Name: "ws_receive", Input: map[string]any{"role": "web", "type": "message"}}, // invented
	}
	planUnsound, coveredUnsound, _ := assemblePlan(unsound, "goal", "ws://h/ws", cfg.Services)
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
	plan, _, covering := assemblePlan(sound, "goal", "ws://h/ws", cfg.Services)
	require.NotEmpty(t, plan.Cases)
	primaryID := plan.Cases[0].ID
	assert.Equal(t, primaryID, covering["rt"]["web"], "sound case ID recorded as web's coverer")

	unsound := []llm.ToolCall{
		{Name: "begin_case", Input: map[string]any{"name": "relay", "expectation": "ok", "service": "rt"}},
		{Name: "ws_connect", Input: map[string]any{"role": "web"}},
		{Name: "ws_receive", Input: map[string]any{"role": "web", "type": "message"}}, // invented
	}
	_, _, coveringUnsound := assemblePlan(unsound, "goal", "ws://h/ws", cfg.Services)
	assert.Empty(t, coveringUnsound["rt"]["web"], "unsound case records no coverer")
}
