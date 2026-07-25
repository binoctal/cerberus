package scout

import (
	"testing"

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
		WSCasesCovered(cfg, "send device:command receive device:ack", nil))

	// Covered role is skipped.
	got := WSCasesCovered(cfg, "send device:command receive device:ack",
		map[string]map[string]bool{"rt": {"web": true}})
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

	plan, covered := assemblePlan(calls, "goal", "ws://h/ws", cfg.Services)

	require.Len(t, plan.Cases, 1, "single ws_flow case from begin_case+ws_*")
	require.NotEmpty(t, plan.Cases[0].Steps)
	require.Equal(t, "ws_connect", plan.Cases[0].Steps[0].Action)
	require.Equal(t, "web", plan.Cases[0].Steps[0].ConnectionID)
	require.True(t, covered["rt"]["web"] && covered["rt"]["bridge"])

	// WSCasesCovered with the real covered map emits no web/bridge cases.
	for _, c := range WSCasesCovered(cfg, "receive devices:sync", covered) {
		require.NotContains(t, c.ID, "-web-")
		require.NotContains(t, c.ID, "-bridge-")
	}
}
