package scout

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/project"
)

func TestWSCasesNoneWhenNoProtocols(t *testing.T) {
	cfg := &project.Config{Services: []project.Service{{Name: "api", URL: "http://x"}}}
	assert.Nil(t, WSCases(cfg, "test it"))
}

func TestWSCasesEmitsConnectAndDecisiveReceives(t *testing.T) {
	cfg := &project.Config{Services: []project.Service{{
		Name: "rt", URL: "http://x",
		Protocol: &project.Protocol{
			TypePath: "type",
			Roles: map[string]*project.ProtocolRole{
				"bridge": {
					Params:    map[string]string{"type": "bridge"},
					Handshake: &project.RoleHandshake{AwaitType: "devices:sync", Timeout: 5},
				},
			},
		},
	}}}
	cases := WSCases(cfg, "bridge receives permission:response with approved=true")
	// No-exchange goal (no send verb) -> ONE ws_flow Steps case whose connect
	// and receives share one connection_id (one caseID -> one namespace, so the
	// receives reach the connect's socket). The connect step carries Role, so
	// the executor auto-awaits the handshake await_type; the receive steps
	// therefore EXCLUDE "devices:sync" (consumed by connect) and keep only the
	// goal-named "permission:response".
	require.Len(t, cases, 1, "no-exchange role emits a single Steps case")
	c := cases[0]
	assert.Equal(t, "ws_flow", c.Action)
	assert.Equal(t, "ws-rt-bridge-connect", c.ID)
	assert.Equal(t, "http://x", c.Target, "case must carry the service URL (target_validate)")
	assert.Empty(t, c.DependsOn, "steps are in-case; no cross-case DependsOn")

	require.Len(t, c.Steps, 2, "connect step + one goal-type receive step")
	// Step 0: connect carries the role (handshake auto-awaited + consumed).
	assert.Equal(t, "ws_connect", c.Steps[0].Action)
	assert.Equal(t, "bridge", c.Steps[0].Role)
	connID := c.Steps[0].ConnectionID
	require.Equal(t, "bridge", connID, "connection_id is the role name")
	// Step 1: the goal-named receive, sharing the connect's connection_id.
	assert.Equal(t, "ws_receive", c.Steps[1].Action)
	assert.Equal(t, connID, c.Steps[1].ConnectionID, "receive must share the connect's connection_id")
	// The handshake await_type is NOT a receive step (awaited by connect).
	assert.ElementsMatch(t, []string{"permission:response"}, stepReceiveTypes(c))
}

func TestWSCasesNoGoalMatchJustHandshake(t *testing.T) {
	cfg := &project.Config{Services: []project.Service{{
		Name: "rt", URL: "http://x",
		Protocol: &project.Protocol{Roles: map[string]*project.ProtocolRole{
			"web": {Handshake: &project.RoleHandshake{AwaitType: "ready", Timeout: 5}},
		}},
	}}}
	cases := WSCases(cfg, "unrelated goal")
	// The only decisive type is the handshake await_type "ready", which the
	// connect step auto-awaits and consumes. No receive step is emitted (it
	// would re-await an already-consumed message) -> a connect-only Steps case.
	require.Len(t, cases, 1)
	c := cases[0]
	assert.Equal(t, "ws_flow", c.Action)
	require.Len(t, c.Steps, 1, "handshake-only role -> connect-only Steps case")
	assert.Equal(t, "ws_connect", c.Steps[0].Action)
	assert.Equal(t, "web", c.Steps[0].Role)
	assert.Empty(t, stepReceiveTypes(c), "handshake await_type is not a receive step")
}

// TestWSCasesGoalNamesHandshakeAwaitStillConnectOnly is the regression guard
// for the handshake-exclusion nuance: when the goal text explicitly names the
// role's handshake await_type, that type must NOT become a separate receive
// step. The connect step auto-awaits (and consumes) the handshake frame, so a
// receive for it would re-await an already-consumed message. The case stays
// connect-only.
func TestWSCasesGoalNamesHandshakeAwaitStillConnectOnly(t *testing.T) {
	cfg := &project.Config{Services: []project.Service{{
		Name: "rt", URL: "http://x",
		Protocol: &project.Protocol{Roles: map[string]*project.ProtocolRole{
			"web": {Handshake: &project.RoleHandshake{AwaitType: "session:ready", Timeout: 5}},
		}},
	}}}
	cases := WSCases(cfg, "verify session:ready")
	require.Len(t, cases, 1)
	c := cases[0]
	assert.Equal(t, "ws_flow", c.Action)
	require.Len(t, c.Steps, 1, "goal naming the handshake type still yields connect-only")
	assert.Equal(t, "ws_connect", c.Steps[0].Action)
	assert.Equal(t, "web", c.Steps[0].Role)
	assert.Empty(t, stepReceiveTypes(c), "the handshake type is consumed by connect, not a receive step")
}

// TestWSCasesMultiRoleDeterministicOrder is the regression guard for the
// role-iteration sort: with multiple roles on one service, the per-role
// ws_flow cases must appear in sorted-role-name order so the returned slice
// is deterministic run-to-run (Go randomizes map iteration order).
func TestWSCasesMultiRoleDeterministicOrder(t *testing.T) {
	cfg := &project.Config{Services: []project.Service{{
		Name: "rt", URL: "http://x",
		Protocol: &project.Protocol{Roles: map[string]*project.ProtocolRole{
			"web":    {Handshake: &project.RoleHandshake{AwaitType: "ready", Timeout: 5}},
			"bridge": {Handshake: &project.RoleHandshake{AwaitType: "devices:sync", Timeout: 5}},
		}},
	}}}
	// Run many times — map iteration order varies, so an unsorted loop would
	// eventually surface a non-deterministic failure here.
	var want []string
	for i := 0; i < 100; i++ {
		cases := WSCases(cfg, "")
		// No-exchange path now emits one ws_flow Steps case per role.
		flows := filterAction(cases, "ws_flow")
		got := make([]string, len(flows))
		for j, c := range flows {
			got[j] = c.ID
		}
		if want == nil {
			want = got
		}
		assert.Equal(t, want, got, "ws_flow case order must be stable across calls (iteration %d)", i)
	}
	// Sorted-role-name order: bridge before web.
	assert.Equal(t,
		[]string{"ws-rt-bridge-connect", "ws-rt-web-connect"}, want,
		"ws_flow cases must appear in sorted role-name order",
	)
}

// TestWSCasesIDFormat pins the exact case-ID format of the no-exchange Steps
// case: ws-<service>-<role>-connect, Action ws_flow, no DependsOn (connect +
// receives are steps in one case, sharing one connection namespace). With an
// empty goal the role is handshake-only, so the case is connect-only.
func TestWSCasesIDFormat(t *testing.T) {
	cfg := &project.Config{Services: []project.Service{{
		Name: "rt", URL: "http://x",
		Protocol: &project.Protocol{Roles: map[string]*project.ProtocolRole{
			"bridge": {Handshake: &project.RoleHandshake{AwaitType: "devices:sync", Timeout: 5}},
		}},
	}}}
	cases := WSCases(cfg, "")

	require.Len(t, cases, 1, "handshake-only role emits one Steps case")
	c := cases[0]
	assert.Equal(t, "ws_flow", c.Action)
	assert.Equal(t, "ws-rt-bridge-connect", c.ID,
		"case ID must be ws-<service>-<role>-connect")
	assert.Empty(t, c.DependsOn,
		"no cross-case DependsOn; connect + receives are in-case Steps")
	// Handshake-only (empty goal): connect step only, no receive step.
	require.Len(t, c.Steps, 1)
	assert.Equal(t, "ws_connect", c.Steps[0].Action)
	assert.Equal(t, "bridge", c.Steps[0].Role)
}

// TestWSCasesTargetSetAndGoalTemplateBraces covers two 2026-07-22 verify-run
// findings: (1) WS cases carry Target = the service URL so they survive
// target_validate (empty target was deprioritized -> skipped -> never run);
// (2) the goal-token heuristic strips braces, so "{type: device:command}" yields
// the routing type "device:command" (not "device:command}") and does not emit a
// spurious "type:" case.
func TestWSCasesTargetSetAndGoalTemplateBraces(t *testing.T) {
	cfg := &project.Config{Services: []project.Service{{
		Name: "rt", URL: "http://localhost:8787",
		Protocol: &project.Protocol{TypePath: "type", Roles: map[string]*project.ProtocolRole{
			"web": {Handshake: &project.RoleHandshake{AwaitType: "devices:sync", Timeout: 5}},
		}},
	}}}
	cases := WSCases(cfg, "send a {type: device:command} message")

	// One ws_flow Steps case targets the service URL (so it is not deprioritized).
	require.Len(t, cases, 1)
	c := cases[0]
	assert.Equal(t, "http://localhost:8787", c.Target, "case must carry the service URL")

	// Brace handling: the goal template yields the routing type "device:command"
	// (not "device:command}") as the receive step's Type, and no spurious
	// "type:" receive. The handshake await_type is excluded (awaited by connect).
	types := stepReceiveTypes(c)
	assert.Contains(t, types, "device:command", "brace-stripped goal type must be device:command")
	for _, ty := range types {
		assert.NotEqual(t, "type:", ty, "the default routing-key field name must not become a receive type")
		assert.NotContains(t, ty, "}", "goal type must not carry a stray brace")
	}
}

func filterAction(cs []agent.TestCase, action string) []agent.TestCase {
	var out []agent.TestCase
	for _, c := range cs {
		if c.Action == action {
			out = append(out, c)
		}
	}
	return out
}

// stepReceiveTypes collects the ws_receive step Types within a single Steps
// case. The no-exchange path now folds connect + receives into one ws_flow
// Steps case (sharing one connection_id) rather than emitting separate
// ws_connect/ws_receive cases, so receives are asserted as Steps, not cases.
func stepReceiveTypes(c agent.TestCase) []string {
	var out []string
	for _, s := range c.Steps {
		if s.Action == "ws_receive" {
			out = append(out, s.Type)
		}
	}
	return out
}

// TestWsTypesNamedInGoalDirection pins the send-verb direction heuristic: a
// colon token immediately preceded by a send-verb is client-sent and excluded;
// tokens without a send-verb context default to receive (included).
func TestWsTypesNamedInGoalDirection(t *testing.T) {
	tests := []struct {
		name string
		goal string
		want []string
	}{
		{"send verb excludes following token", "send device:command, verify device:ack", []string{"device:ack"}},
		{"verify keeps token", "verify device:ack", []string{"device:ack"}},
		{"no verb defaults to include", "device:command", []string{"device:command"}},
		{"brace template no verb includes", "{type: device:command}", []string{"device:command"}},
		{"mixed send and verify", "send devices:sync and verify device:ack", []string{"device:ack"}},
		{"emit verb excludes", "emit status:update then verify status:ack", []string{"status:ack"}},
		{"publishes inflection excludes", "publishes status:update then verify status:ok", []string{"status:ok"}},
		{"parenthesized send verb excludes", "(send device:command) then verify device:ack", []string{"device:ack"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.ElementsMatch(t, tc.want, wsTypesNamedInGoal(tc.goal))
		})
	}
}

// TestWSCasesSendVerbTokenNotReceive is the behavioral proof for the direction
// heuristic: a verb-phrased goal pairs the send type with the following receive
// type as ONE deterministic Steps case. The client-sent type (send device:command)
// must NOT become a ws_receive target — it is the ws_send step's message type.
// The handshake await_type is auto-awaited on the connect step (via the role),
// so it does not add a separate ws_receive case.
func TestWSCasesSendVerbTokenNotReceive(t *testing.T) {
	cfg := &project.Config{Services: []project.Service{{
		Name: "rt", URL: "http://x",
		Protocol: &project.Protocol{TypePath: "type", Roles: map[string]*project.ProtocolRole{
			"web": {Handshake: &project.RoleHandshake{AwaitType: "devices:sync", Timeout: 5}},
		}},
	}}}
	cases := WSCases(cfg, "send device:command, verify device:ack")
	// Exchange: ONE Steps case, no separate ws_connect/ws_receive cases.
	require.Len(t, cases, 1, "exchange should produce exactly one Steps case")
	c := cases[0]
	assert.Equal(t, "ws_flow", c.Action)
	require.Len(t, c.Steps, 3, "exchange must be connect/send/receive")

	// device:command is the ws_send step's message type, NOT a receive target.
	assert.Equal(t, "ws_send", c.Steps[1].Action)
	var sendMsg map[string]string
	require.NoError(t, json.Unmarshal([]byte(c.Steps[1].Message), &sendMsg))
	assert.Equal(t, "device:command", sendMsg["type"],
		"device:command is the client-sent type carried on ws_send")

	// Exactly one receive — the goal-named device:ack — and it lives INSIDE the
	// Steps case. device:command must not appear as a receive type.
	assert.Equal(t, "ws_receive", c.Steps[2].Action)
	assert.Equal(t, "device:ack", c.Steps[2].Type,
		"device:ack is the receive type")
	assert.NotEqual(t, "device:command", c.Steps[2].Type,
		"a client-sent type (send device:command) must not become a ws_receive target")
	assert.Empty(t, filterAction(cases, "ws_receive"),
		"exchange has no separate ws_receive cases — the receive is a Step")
}

// TestWSCasesEmitsStepsForExchange pins the canonical send→receive exchange
// shape: goal "send device:command, verify device:ack approved=true" yields
// exactly ONE ws_flow case with connect/send/receive steps sharing one
// connection_id, Target = svc.URL, and Asserts derived from "approved=true"
// as {"payload.approved": true}. The role's handshake await_type is read on
// the connect step (no separate handshake case).
func TestWSCasesEmitsStepsForExchange(t *testing.T) {
	cfg := &project.Config{Services: []project.Service{{
		Name: "rt", URL: "http://x",
		Protocol: &project.Protocol{TypePath: "type", Roles: map[string]*project.ProtocolRole{
			"web": {Handshake: &project.RoleHandshake{AwaitType: "devices:sync", Timeout: 5}},
		}},
	}}}
	cases := WSCases(cfg, "send device:command, verify device:ack approved=true")
	require.Len(t, cases, 1, "exchange should produce exactly one Steps case")
	c := cases[0]

	assert.Equal(t, "ws_flow", c.Action)
	assert.Equal(t, "rt", c.Service)
	assert.Equal(t, "http://x", c.Target, "case must carry the service URL")
	assert.NotEmpty(t, c.ID)

	require.Len(t, c.Steps, 3, "exchange must be connect/send/receive")

	// Step 1: connect with the role (handshake await_type auto-awaited).
	assert.Equal(t, "ws_connect", c.Steps[0].Action)
	assert.Equal(t, "web", c.Steps[0].Role)
	connID := c.Steps[0].ConnectionID
	require.NotEmpty(t, connID)

	// Step 2: send the client-sent type, sharing the connect's connection.
	assert.Equal(t, "ws_send", c.Steps[1].Action)
	assert.Equal(t, connID, c.Steps[1].ConnectionID, "send must share connect's connection_id")
	var msg map[string]string
	require.NoError(t, json.Unmarshal([]byte(c.Steps[1].Message), &msg))
	assert.Equal(t, "device:command", msg["type"], "send message carries the client-sent type")

	// Step 3: receive the server-reply type with derived Asserts.
	assert.Equal(t, "ws_receive", c.Steps[2].Action)
	assert.Equal(t, connID, c.Steps[2].ConnectionID, "receive must share connect's connection_id")
	assert.Equal(t, "device:ack", c.Steps[2].Type)
	assert.Equal(t, map[string]any{"payload.approved": true}, c.Steps[2].Asserts,
		`approved=true -> {"payload.approved": true}`)

	// No legacy separate-case shape leaks through.
	assert.Empty(t, filterAction(cases, "ws_connect"), "no separate ws_connect case (folded into Steps)")
	assert.Empty(t, filterAction(cases, "ws_receive"), "no separate ws_receive case (folded into Steps)")
}

// TestWSCasesNoExchangeEmitsStepsCase pins the no-exchange rule: a goal without
// a send-verb → receive-type pair yields ONE ws_flow Steps case (connect +
// decisive receives sharing one connection_id), not separate connect/ws_receive
// cases. The handshake await_type "ready" is excluded (auto-awaited by the
// connect step); the goal-named "status:ok" is a receive step. Connect/handshake
// coverage is preserved and runs deterministically via runSteps.
func TestWSCasesNoExchangeEmitsStepsCase(t *testing.T) {
	cfg := &project.Config{Services: []project.Service{{
		Name: "rt", URL: "http://x",
		Protocol: &project.Protocol{TypePath: "type", Roles: map[string]*project.ProtocolRole{
			"web": {Handshake: &project.RoleHandshake{AwaitType: "ready", Timeout: 5}},
		}},
	}}}
	// Receive-only goal: no send verb -> no exchange -> one Steps case.
	cases := WSCases(cfg, "verify status:ok")

	require.Len(t, cases, 1)
	c := cases[0]
	assert.Equal(t, "ws_flow", c.Action)
	assert.Equal(t, "http://x", c.Target)

	// Connect step + exactly one receive step (status:ok); the handshake
	// await_type "ready" is NOT a receive step (awaited by connect).
	require.Len(t, c.Steps, 2)
	assert.Equal(t, "ws_connect", c.Steps[0].Action)
	assert.Equal(t, "web", c.Steps[0].Role)
	assert.Equal(t, "ws_receive", c.Steps[1].Action)
	assert.Equal(t, "status:ok", c.Steps[1].Type)
	assert.ElementsMatch(t, []string{"status:ok"}, stepReceiveTypes(c))

	// No legacy separate-case shape leaks through.
	assert.Empty(t, filterAction(cases, "ws_connect"), "connect folded into Steps")
	assert.Empty(t, filterAction(cases, "ws_receive"), "receive folded into Steps")
}

// TestWSExchangeFromGoal is the unit test for the exchange detector: a
// send-verb immediately preceding a colon token marks the send type; the next
// non-send colon token is the receive type; trailing key=value tokens become
// Asserts (key prefixed with "payload." when it has no dot). Deterministic.
func TestWSExchangeFromGoal(t *testing.T) {
	tests := []struct {
		name    string
		goal    string
		ok      bool
		send    string
		recv    string
		asserts map[string]any
	}{
		{
			name:    "exchange with assert",
			goal:    "send device:command, verify device:ack approved=true",
			ok:      true,
			send:    "device:command",
			recv:    "device:ack",
			asserts: map[string]any{"payload.approved": true},
		},
		{
			name: "exchange no assert",
			goal: "send device:command, verify device:ack",
			ok:   true,
			send: "device:command",
			recv: "device:ack",
			// asserts is nil (arrival-only).
		},
		{
			name:    "emit verb exchange",
			goal:    "emit status:update then verify status:ack count=2",
			ok:      true,
			send:    "status:update",
			recv:    "status:ack",
			asserts: map[string]any{"payload.count": 2},
		},
		{
			name: "publishes verb exchange",
			goal: "publishes sync:tick verify sync:tock",
			ok:   true,
			send: "sync:tick",
			recv: "sync:tock",
		},
		{
			name: "no send verb no exchange",
			goal: "verify device:ack",
			ok:   false,
		},
		{
			name: "send verb without receive pair no exchange",
			goal: "send device:command only",
			ok:   false,
		},
		{
			name: "brace template send not exchange (no following receive)",
			goal: "send a {type: device:command} message",
			ok:   false,
		},
		{
			name:    "string assert value",
			goal:    "send a:b verify c:d name=\"hello\"",
			ok:      true,
			send:    "a:b",
			recv:    "c:d",
			asserts: map[string]any{"payload.name": "hello"},
		},
		{
			name:    "already-dotted assert key not prefixed",
			goal:    "send a:b verify c:d meta.flag=true",
			ok:      true,
			send:    "a:b",
			recv:    "c:d",
			asserts: map[string]any{"meta.flag": true},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ex, ok := wsExchangeFromGoal(tc.goal)
			require.Equal(t, tc.ok, ok, "ok mismatch")
			if !ok {
				return
			}
			assert.Equal(t, tc.send, ex.sendType)
			assert.Equal(t, tc.recv, ex.recvType)
			if tc.asserts == nil {
				assert.Nil(t, ex.asserts, "arrival-only receive must have nil Asserts")
			} else {
				assert.Equal(t, tc.asserts, ex.asserts)
			}
		})
	}
}

// TestShouldMatchAllBatch pins the rule that decides whether a wsStepsCase
// receive carries MatchAll=true. Match-all is emitted only when the receive type
// is a declared batch's item_type (the server may send a batch the pump
// decomposes), AND no assert references that batch's items_path (a batch-level
// assert applied per item would false-fail). See ws_cases.go.
func TestShouldMatchAllBatch(t *testing.T) {
	proto := func() *project.Protocol {
		return &project.Protocol{TypePath: "type", Batches: map[string]*project.ProtocolBatch{
			"device:ack-batch": {ItemType: "device:ack", ItemsPath: "payload.items"},
		}}
	}
	tests := []struct {
		name     string
		proto    *project.Protocol
		recvType string
		asserts  map[string]any
		want     bool
	}{
		{
			name:     "batch item type with per-item assert -> match all",
			proto:    proto(),
			recvType: "device:ack",
			asserts:  map[string]any{"payload.approved": true},
			want:     true,
		},
		{
			name:     "batch item type arrival-only -> match all",
			proto:    proto(),
			recvType: "device:ack",
			asserts:  nil,
			want:     true,
		},
		{
			name:     "non-batch receive type -> first match",
			proto:    proto(),
			recvType: "device:nack",
			asserts:  map[string]any{"payload.approved": true},
			want:     false,
		},
		{
			name:     "assert equals items path -> batch-level, decline match all",
			proto:    proto(),
			recvType: "device:ack",
			asserts:  map[string]any{"payload.items": "x"},
			want:     false,
		},
		{
			name:     "assert under items path -> batch-level, decline match all",
			proto:    proto(),
			recvType: "device:ack",
			asserts:  map[string]any{"payload.items.0.id": "x"},
			want:     false,
		},
		{
			name:     "nil protocol -> first match",
			proto:    nil,
			recvType: "device:ack",
			want:     false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldMatchAllBatch(tc.proto, tc.recvType, tc.asserts)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestWSCasesEmitsMatchAllForBatchItemType is the end-to-end pin: when the goal's
// receive type is a declared batch item_type, the emitted ws_flow receive step
// carries MatchAll=true (so the executor asserts every decomposed item). The
// per-item assert "payload.approved" does not reference the items path, so the
// guard passes. Verified RED by removing the shouldMatchAllBatch call.
func TestWSCasesEmitsMatchAllForBatchItemType(t *testing.T) {
	cfg := &project.Config{Services: []project.Service{{
		Name: "rt", URL: "http://x",
		Protocol: &project.Protocol{TypePath: "type", Roles: map[string]*project.ProtocolRole{
			"web": {Handshake: &project.RoleHandshake{AwaitType: "devices:sync", Timeout: 5}},
		}, Batches: map[string]*project.ProtocolBatch{
			"device:ack-batch": {ItemType: "device:ack", ItemsPath: "payload.items"},
		}},
	}}}
	cases := WSCases(cfg, "send device:command, verify device:ack approved=true")
	require.Len(t, cases, 1, "exchange should produce exactly one Steps case")
	require.Len(t, cases[0].Steps, 3)
	recv := cases[0].Steps[2]
	assert.Equal(t, "ws_receive", recv.Action)
	assert.Equal(t, "device:ack", recv.Type, "receive type is the batch item_type")
	assert.True(t, recv.MatchAll, "receive of a batch item_type must carry MatchAll so every item is asserted")
}

// TestWSCasesDeclinesMatchAllForBatchLevelAssert pins the guard: when the only
// assert references the batch's items_path, match-all is declined (the assert is
// batch-level) and the receive falls back to first-match.
func TestWSCasesDeclinesMatchAllForBatchLevelAssert(t *testing.T) {
	cfg := &project.Config{Services: []project.Service{{
		Name: "rt", URL: "http://x",
		Protocol: &project.Protocol{TypePath: "type", Roles: map[string]*project.ProtocolRole{
			"web": {},
		}, Batches: map[string]*project.ProtocolBatch{
			"device:ack-batch": {ItemType: "device:ack", ItemsPath: "payload.items"},
		}},
	}}}
	// "payload.items" is the batch items_path -> batch-level assert.
	cases := WSCases(cfg, "send device:command, verify device:ack payload.items=x")
	require.Len(t, cases, 1)
	recv := cases[0].Steps[2]
	assert.Equal(t, "device:ack", recv.Type)
	assert.False(t, recv.MatchAll, "a batch-level (items_path) assert must decline match_all")
}

// TestWSCasesCollidingTypesDedupToOneCase pins the cross-source ID-collision
// behavior in the new Steps shape: two decisive types that sanitize to the same
// ID (a handshake await_type "devices-sync" and a goal-named "devices:sync",
// both -> "devices-sync") dedup to a single entry in wsDecisiveTypes. That entry
// is the handshake spelling (added first), which the connect step auto-awaits
// and consumes — so no separate receive step is emitted (it would re-await an
// already-consumed message). The case is connect-only.
func TestWSCasesCollidingTypesDedupToOneCase(t *testing.T) {
	cfg := &project.Config{Services: []project.Service{{
		Name: "rt", URL: "http://x",
		Protocol: &project.Protocol{TypePath: "type", Roles: map[string]*project.ProtocolRole{
			"web": {Handshake: &project.RoleHandshake{AwaitType: "devices-sync", Timeout: 5}},
		}},
	}}}
	// Goal names the same routing type with a colon; sanitizeTypeID collapses
	// "devices-sync" and "devices:sync" to one ID.
	cases := WSCases(cfg, "verify devices:sync")
	require.Len(t, cases, 1)
	c := cases[0]
	assert.Equal(t, "ws_flow", c.Action)
	// The collision collapses to the handshake entry, consumed by connect.
	require.Len(t, c.Steps, 1, "colliding type collapses into the handshake -> connect-only")
	assert.Equal(t, "ws_connect", c.Steps[0].Action)
	assert.Empty(t, stepReceiveTypes(c), "no duplicate receive for the colliding type")
}

// wsRelayCases emits a relay case for an optional-handshake role in a ≥2-role
// protocol; connect the receiver first, then peers, then receive the signal.
func TestWSRelayCases(t *testing.T) {
	p := &project.Protocol{TypePath: "type", Roles: map[string]*project.ProtocolRole{
		"web":    {Handshake: &project.RoleHandshake{AwaitType: "device:online", Optional: true, Timeout: 2}},
		"bridge": {},
	}}
	svc := project.Service{Name: "rt", URL: "ws://h/ws", Protocol: p}

	cases, covered, connected := wsRelayCases(svc)
	require.Len(t, cases, 1, "one relay case for web's optional handshake")
	c := cases[0]
	require.Equal(t, "ws-rt-relay-web-signal-device-online", c.ID)
	require.Equal(t, "ws://h/ws", c.Target)
	require.Equal(t, "ws_flow", c.Action)
	require.Len(t, c.Steps, 4)
	require.Equal(t, "ws_connect", c.Steps[0].Action) // web (receiver) first
	require.Equal(t, "web", c.Steps[0].ConnectionID)
	require.Equal(t, "ws_connect", c.Steps[1].Action) // bridge (peer)
	require.Equal(t, "bridge", c.Steps[1].ConnectionID)
	require.Equal(t, "ws_receive", c.Steps[2].Action)
	require.Equal(t, "device:online", c.Steps[2].Type)
	require.Equal(t, "web", c.Steps[2].ConnectionID)
	require.Equal(t, 2, c.Steps[2].Timeout)
	// Sender-exclusion probe: bridge (the joining peer) must not receive its own
	// join signal. ExpectAbsent inverts the receive; a short timeout bounds cost.
	require.Equal(t, "ws_receive", c.Steps[3].Action)
	require.Equal(t, "bridge", c.Steps[3].ConnectionID)
	require.Equal(t, "device:online", c.Steps[3].Type)
	require.True(t, c.Steps[3].ExpectAbsent, "final step is the sender-exclusion probe")
	require.Greater(t, c.Steps[3].Timeout, 0)
	require.True(t, covered["web"]["device:online"])
	// Finding-2: the relay case connects the receiver (web) AND its peer (bridge).
	require.True(t, connected["web"], "receiver is connected by the relay")
	require.True(t, connected["bridge"], "peer is connected by the relay")
}

// ≥3 roles: the receiver and ALL peers land in connectedRoles.
func TestWSRelayCases_MultiPeer(t *testing.T) {
	p := &project.Protocol{TypePath: "type", Roles: map[string]*project.ProtocolRole{
		"web":    {Handshake: &project.RoleHandshake{AwaitType: "device:online", Optional: true, Timeout: 2}},
		"bridge": {},
		"app":    {},
	}}
	_, _, connected := wsRelayCases(project.Service{Name: "rt", Protocol: p})
	for _, role := range []string{"web", "bridge", "app"} {
		require.True(t, connected[role], "%s connected by the relay", role)
	}
}

// No emission when the trigger is absent.
func TestWSRelayCases_NoTrigger(t *testing.T) {
	for _, name := range []string{"single role", "mandatory handshake", "no handshake"} {
		t.Run(name, func(t *testing.T) {
			var p *project.Protocol
			switch name {
			case "single role":
				p = &project.Protocol{Roles: map[string]*project.ProtocolRole{"web": {Handshake: &project.RoleHandshake{AwaitType: "x", Optional: true}}}}
			case "mandatory handshake":
				p = &project.Protocol{Roles: map[string]*project.ProtocolRole{"web": {Handshake: &project.RoleHandshake{AwaitType: "x"}}, "bridge": {}}}
			case "no handshake":
				p = &project.Protocol{Roles: map[string]*project.ProtocolRole{"web": {}, "bridge": {}}}
			}
			cases, covered, connected := wsRelayCases(project.Service{Name: "rt", Protocol: p})
			require.Empty(t, cases)
			require.Empty(t, covered)
			require.Empty(t, connected)
		})
	}
}

// WSCasesCovered suppresses the redundant single-conn receive of a covered signal.
func TestWSCasesCovered_RelaySuppressesReceive(t *testing.T) {
	p := &project.Protocol{TypePath: "type", Roles: map[string]*project.ProtocolRole{
		"web":    {Handshake: &project.RoleHandshake{AwaitType: "device:online", Optional: true, Timeout: 2}},
		"bridge": {},
	}}
	cfg := &project.Config{Services: []project.Service{{Name: "rt", URL: "ws://h/ws", Protocol: p}}}
	got := WSCases(cfg, "verify web receives device:online")
	// A relay case is present.
	var relay *agent.TestCase
	for i := range got {
		if got[i].Action == "ws_flow" && len(got[i].Steps) > 1 {
			relay = &got[i]
		}
	}
	require.NotNil(t, relay, "deterministic relay case emitted")
	// No separate single-conn ws_receive device:online case (it is covered by the relay).
	for _, c := range got {
		if c.Action == "ws_receive" {
			require.NotContains(t, c.ID, "device-online", "redundant single-conn signal receive suppressed")
		}
	}
}

// Finding-2: when the deterministic relay case connects a role (receiver or
// peer), WSCasesCovered suppresses the redundant single-conn connect case for
// it — the connect runs inside the relay case's Steps, and the lone single-conn
// form would route through Steer and fail. goal-exchange wsStepsCase is still
// emitted (it precedes the connectedRoles check).
func TestWSCasesCovered_RelaySuppressesConnect(t *testing.T) {
	p := &project.Protocol{TypePath: "type", Roles: map[string]*project.ProtocolRole{
		"web":    {Handshake: &project.RoleHandshake{AwaitType: "device:online", Optional: true, Timeout: 2}},
		"bridge": {},
	}}
	cfg := &project.Config{Services: []project.Service{{Name: "rt", URL: "ws://h/ws", Protocol: p}}}
	cases := WSCases(cfg, "verify web receives device:online")

	// The relay case is present.
	var hasRelay bool
	for _, c := range cases {
		if c.Action == "ws_flow" && len(c.Steps) > 1 {
			hasRelay = true
		}
	}
	require.True(t, hasRelay, "deterministic relay case emitted")

	// No single-conn connect case for the roles the relay connects (web+bridge).
	connects := filterAction(cases, "ws_connect")
	require.Empty(t, connects, "relay covers web+bridge; no redundant single-conn connect")
}

// Finding-2 follow-up (opus-flagged coverage gap): a relay-connected role still
// gets its goal-exchange wsStepsCase — the goal-exchange branch precedes the
// connectedRoles check, so a send→receive exchange is NOT suppressed by the
// relay. Pins the check order against a regression that moves connectedRoles
// before the exchange branch.
func TestWSCasesCovered_RelayConnectedRoleKeepsExchange(t *testing.T) {
	p := &project.Protocol{TypePath: "type", Roles: map[string]*project.ProtocolRole{
		"web":    {Handshake: &project.RoleHandshake{AwaitType: "device:online", Optional: true, Timeout: 2}},
		"bridge": {},
	}}
	cfg := &project.Config{Services: []project.Service{{Name: "rt", URL: "ws://h/ws", Protocol: p}}}
	// Goal has a send->receive exchange; web+bridge are relay-connected.
	cases := WSCases(cfg, "send session:start and receive session:created")

	// A goal-exchange wsStepsCase (ws_flow with a ws_send step) is present
	// despite web being relay-connected.
	var exchangeCases int
	for _, c := range cases {
		if c.Action != "ws_flow" || len(c.Steps) != 3 {
			continue
		}
		for _, st := range c.Steps {
			if st.Action == "ws_send" {
				exchangeCases++
				break
			}
		}
	}
	require.GreaterOrEqual(t, exchangeCases, 1,
		"goal-exchange wsStepsCase preserved for a relay-connected role")
}

// TestWSCasesCovered_RelayDroppedWhenLLMCoversReceiver: when an LLM ws_relay
// already covers the receiver role (the covered map), the deterministic relay
// case for that role is dropped (no double-coverage) and its signal is not
// suppressed for the per-role loop. Deviation #2 (coexistence with A1).
func TestWSCasesCovered_RelayDroppedWhenLLMCoversReceiver(t *testing.T) {
	p := &project.Protocol{TypePath: "type", Roles: map[string]*project.ProtocolRole{
		"web":    {Handshake: &project.RoleHandshake{AwaitType: "device:online", Optional: true, Timeout: 2}},
		"bridge": {},
	}}
	cfg := &project.Config{Services: []project.Service{{Name: "rt", URL: "ws://h/ws", Protocol: p}}}
	got := WSCasesCovered(cfg, "verify web receives device:online",
		map[string]map[string]bool{"rt": {"web": true}}, nil)
	// No deterministic relay case (web is covered by an LLM ws_relay).
	for _, c := range got {
		require.NotContains(t, c.ID, "relay-web-signal", "deterministic relay dropped when LLM covers the receiver")
	}
	// web is covered at the role level → no web cases at all; bridge still
	// connects via its own ws_flow Steps case (connect is a step, not a case).
	var sawBridgeCase bool
	for _, c := range got {
		require.NotContains(t, c.ID, "-web-", "web fully skipped (LLM-covered)")
		if c.Action == "ws_flow" && strings.Contains(c.ID, "-bridge-") {
			sawBridgeCase = true
		}
	}
	require.True(t, sawBridgeCase, "uncovered bridge role still gets a connect Steps case")
}
