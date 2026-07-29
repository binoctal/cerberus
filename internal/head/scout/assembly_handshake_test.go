package scout

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
)

// wsProtoHandshake returns a protocol whose role's connect auto-awaits
// "hello" — the self-handshake re-await fixture for the tests below.
func wsProtoHandshake() *project.Protocol {
	return &project.Protocol{Roles: map[string]*project.ProtocolRole{
		"web": {Handshake: &project.RoleHandshake{AwaitType: "hello"}},
	}}
}

// wsHandshakeCalls builds a begin_case + the supplied ws_* calls for the named
// service, the assembly idiom shared by these fixtures.
func wsHandshakeCalls(svc string, ws ...llm.ToolCall) []llm.ToolCall {
	out := []llm.ToolCall{{
		Name:  "begin_case",
		Input: map[string]any{"name": "c", "expectation": "ok", "service": svc},
	}}
	return append(out, ws...)
}

func wsConnect(role string) llm.ToolCall {
	return llm.ToolCall{Name: "ws_connect", Input: map[string]any{"role": role}}
}

func wsRecv(role, typ string, aliases ...string) llm.ToolCall {
	return llm.ToolCall{
		Name:  "ws_receive",
		Input: map[string]any{"role": role, "type": typ, "aliases": anyAliases(aliases)},
	}
}

// anyAliases returns []any so llm.StrSliceField decodes it; nil for none.
func anyAliases(s []string) any {
	if len(s) == 0 {
		return nil
	}
	out := make([]any, len(s))
	for i, v := range s {
		out[i] = v
	}
	return out
}

// TestAssemblePlan_DropsSelfHandshakeReawait: an LLM-authored ws_flow that
// emits ws_connect(web) + ws_receive(hello) — where "hello" is web's
// Handshake.AwaitType — has its redundant receive dropped at assembly time.
// The connect auto-awaits and consumes that frame (websocket.go), so a later
// receive of the same type would time out at runtime. The collapsed
// connect-only case stays sound and still marks the role covered.
func TestAssemblePlan_DropsSelfHandshakeReawait(t *testing.T) {
	svcs := []project.Service{{Name: "ws", Protocol: wsProtoHandshake()}}
	calls := wsHandshakeCalls("ws",
		wsConnect("web"),
		wsRecv("web", "hello"),
	)
	plan, covered, covering, _ := assemblePlan(calls, "verify hello", "", svcs)
	require.Len(t, plan.Cases, 1)
	c := plan.Cases[0]
	assert.Equal(t, "ws_flow", c.Action)
	// Only the connect remains; the redundant handshake receive is dropped.
	require.Len(t, c.Steps, 1, "handshake re-await receive must be dropped")
	assert.Equal(t, "ws_connect", c.Steps[0].Action)
	assert.Equal(t, "web", c.Steps[0].Role)
	// Connect-only is trivially sound, so the role is still covered and bound.
	assert.True(t, covered["ws"]["web"], "connect-only case covers the role")
	assert.Equal(t, c.ID, covering["ws"]["web"], "covering case is bound to the role")
}

// TestAssemblePlan_DropsSelfHandshakeReawait_KeepsOtherReceive: when the
// LLM also emits a valid non-handshake receive, ONLY the handshake re-await is
// dropped; the grounded receive stays.
func TestAssemblePlan_DropsSelfHandshakeReawait_KeepsOtherReceive(t *testing.T) {
	svcs := []project.Service{{Name: "ws", Protocol: wsProtoHandshake()}}
	calls := wsHandshakeCalls("ws",
		wsConnect("web"),
		wsRecv("web", "hello"),
		wsRecv("web", "device:sync"),
	)
	plan, _, _, _ := assemblePlan(calls, "verify device:sync", "", svcs)
	require.Len(t, plan.Cases, 1)
	c := plan.Cases[0]
	require.Len(t, c.Steps, 2, "handshake dropped, device:sync kept")
	assert.Equal(t, "ws_connect", c.Steps[0].Action)
	assert.Equal(t, "ws_receive", c.Steps[1].Action)
	assert.Equal(t, "device:sync", c.Steps[1].Type)
}

// TestAssemblePlan_DropsSelfHandshakeReawait_AliasMatch: a receive whose
// declared type is NOT the handshake type but whose Aliases include it is also
// dropped — the alias would match the consumed handshake frame at runtime.
func TestAssemblePlan_DropsSelfHandshakeReawait_AliasMatch(t *testing.T) {
	svcs := []project.Service{{Name: "ws", Protocol: wsProtoHandshake()}}
	calls := wsHandshakeCalls("ws",
		wsConnect("web"),
		wsRecv("web", "greeting", "hello"),
	)
	plan, _, _, _ := assemblePlan(calls, "verify hello", "", svcs)
	require.Len(t, plan.Cases, 1)
	c := plan.Cases[0]
	require.Len(t, c.Steps, 1, "receive aliased to handshake type must be dropped")
	assert.Equal(t, "ws_connect", c.Steps[0].Action)
}

// TestAssemblePlan_SelfHandshakeSanitize_NoRedundancy_NoOp: a case whose
// receive is NOT the handshake type is unchanged by sanitization.
func TestAssemblePlan_SelfHandshakeSanitize_NoRedundancy_NoOp(t *testing.T) {
	svcs := []project.Service{{Name: "ws", Protocol: wsProtoHandshake()}}
	calls := wsHandshakeCalls("ws",
		wsConnect("web"),
		wsRecv("web", "device:sync"),
	)
	plan, _, _, _ := assemblePlan(calls, "verify device:sync", "", svcs)
	require.Len(t, plan.Cases, 1)
	c := plan.Cases[0]
	require.Len(t, c.Steps, 2, "non-handshake receive must NOT be dropped")
	assert.Equal(t, "ws_connect", c.Steps[0].Action)
	assert.Equal(t, "ws_receive", c.Steps[1].Action)
}

// TestAssemblePlan_SelfHandshakeSanitize_OptionalHandshake_Kept: an OPTIONAL
// handshake's AwaitType is a peer-join signal — the connect's auto-await TIMES
// OUT (the signal arrives later, beyond the handshake window) without consuming
// it, so the later ws_receive(signal) is the decisive assertion and must be
// KEPT. Only mandatory (consumed) handshakes are sanitized. Mirrors the
// deterministic relay, which is built only for optional handshakes
// (ws_cases.go:206 — !a.Handshake.Optional → skip).
func TestAssemblePlan_SelfHandshakeSanitize_OptionalHandshake_Kept(t *testing.T) {
	proto := &project.Protocol{Roles: map[string]*project.ProtocolRole{
		"web": {Handshake: &project.RoleHandshake{AwaitType: "signal", Optional: true, Timeout: 1}},
	}}
	svcs := []project.Service{{Name: "ws", Protocol: proto}}
	calls := wsHandshakeCalls("ws",
		wsConnect("web"),
		wsRecv("web", "signal"),
	)
	plan, _, _, _ := assemblePlan(calls, "verify signal", "", svcs)
	require.Len(t, plan.Cases, 1)
	c := plan.Cases[0]
	require.Len(t, c.Steps, 2, "optional-handshake peer-join receive must NOT be dropped")
	assert.Equal(t, "ws_connect", c.Steps[0].Action)
	assert.Equal(t, "ws_receive", c.Steps[1].Action)
	assert.Equal(t, "signal", c.Steps[1].Type)
}
