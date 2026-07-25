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
	// One connect setup + one handshake-await decisive receive + one goal-named receive.
	connects := filterAction(cases, "ws_connect")
	require.Len(t, connects, 1)
	assert.Equal(t, "rt", connects[0].Service)
	assert.True(t, connects[0].Background)
	assertBodyRole(t, connects[0].Body, "bridge")
	// Target must be the service URL so target_validate does not deprioritize
	// the case (empty target -> skipped, never executed — 2026-07-22 verify run).
	assert.Equal(t, "http://x", connects[0].Target)

	receives := filterAction(cases, "ws_receive")
	// handshake await_type "devices:sync" + goal-named "permission:response"
	types := bodyTypes(receives)
	assert.ElementsMatch(t, []string{"devices:sync", "permission:response"}, types)
	// Every receive depends on the connect.
	for _, r := range receives {
		assert.Contains(t, []string(r.DependsOn), connects[0].ID)
	}
}

func TestWSCasesNoGoalMatchJustHandshake(t *testing.T) {
	cfg := &project.Config{Services: []project.Service{{
		Name: "rt", URL: "http://x",
		Protocol: &project.Protocol{Roles: map[string]*project.ProtocolRole{
			"web": {Handshake: &project.RoleHandshake{AwaitType: "ready", Timeout: 5}},
		}},
	}}}
	cases := WSCases(cfg, "unrelated goal")
	receives := filterAction(cases, "ws_receive")
	assert.Equal(t, []string{"ready"}, bodyTypes(receives))
}

// TestWSCasesMultiRoleDeterministicOrder is the regression guard for the
// role-iteration sort: with multiple roles on one service, the ws_connect
// cases must appear in sorted-role-name order so the returned slice is
// deterministic run-to-run (Go randomizes map iteration order).
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
		connects := filterAction(cases, "ws_connect")
		got := make([]string, len(connects))
		for j, c := range connects {
			got[j] = c.ID
		}
		if want == nil {
			want = got
		}
		assert.Equal(t, want, got, "ws_connect case order must be stable across calls (iteration %d)", i)
	}
	// Sorted-role-name order: bridge before web.
	assert.Equal(t,
		[]string{"ws-rt-bridge-connect", "ws-rt-web-connect"}, want,
		"ws_connect cases must appear in sorted role-name order",
	)
}

// TestWSCasesIDFormat pins the exact case-ID format so downstream
// DependsOn matching stays stable: connect = ws-<service>-<role>-connect,
// receive = ws-<service>-<role>-<sanitized-type> with ':' collapsed to '-'.
func TestWSCasesIDFormat(t *testing.T) {
	cfg := &project.Config{Services: []project.Service{{
		Name: "rt", URL: "http://x",
		Protocol: &project.Protocol{Roles: map[string]*project.ProtocolRole{
			"bridge": {Handshake: &project.RoleHandshake{AwaitType: "devices:sync", Timeout: 5}},
		}},
	}}}
	cases := WSCases(cfg, "")

	connects := filterAction(cases, "ws_connect")
	require.Len(t, connects, 1, "expected exactly one ws_connect case")
	connect := connects[0]
	assert.Equal(t, "ws-rt-bridge-connect", connect.ID,
		"connect ID must be ws-<service>-<role>-connect")

	// Find the handshake-await receive case and pin its sanitized ID +
	// DependsOn wiring. "devices:sync" -> "devices-sync".
	for _, c := range cases {
		if c.Action != "ws_receive" {
			continue
		}
		if strings.Contains(c.ID, "devices-sync") {
			assert.Equal(t, "ws-rt-bridge-devices-sync", c.ID,
				"receive ID must be ws-<service>-<role>-<sanitized type>; ':' -> '-'")
			assert.Equal(t, agent.Deps{connect.ID}, c.DependsOn,
				"receive must depend on its role's connect ID")
			return
		}
	}
	t.Fatal("no ws_receive case with sanitized devices-sync ID found")
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

	// Every generated case targets the service URL (so it is not deprioritized).
	for _, c := range cases {
		assert.Equal(t, "http://localhost:8787", c.Target, "case %q missing service URL target", c.ID)
	}

	// Brace handling: the goal template yields the routing type "device:command",
	// not "device:command}", and no spurious "type:" receive.
	receives := filterAction(cases, "ws_receive")
	types := bodyTypes(receives)
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

func bodyTypes(cs []agent.TestCase) []string {
	var out []string
	for _, c := range cs {
		var b map[string]string
		if json.Unmarshal([]byte(c.Body), &b) == nil {
			out = append(out, b["type"])
		}
	}
	return out
}

func assertBodyRole(t *testing.T, body, want string) {
	t.Helper()
	var b map[string]string
	require.NoError(t, json.Unmarshal([]byte(body), &b))
	assert.Equal(t, want, b["role"])
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

// TestWSCasesConnectOnlyWhenNoExchange pins the chosen no-exchange rule: a goal
// without a send-verb → receive-type pair keeps today's separate connect +
// per-type ws_receive case form (connect/handshake coverage preserved). This is
// the documented fallback for receive-only, handshake-only, or unrelated goals.
func TestWSCasesConnectOnlyWhenNoExchange(t *testing.T) {
	cfg := &project.Config{Services: []project.Service{{
		Name: "rt", URL: "http://x",
		Protocol: &project.Protocol{TypePath: "type", Roles: map[string]*project.ProtocolRole{
			"web": {Handshake: &project.RoleHandshake{AwaitType: "ready", Timeout: 5}},
		}},
	}}}
	// Receive-only goal: no send verb -> no exchange -> today's form.
	cases := WSCases(cfg, "verify status:ok")

	assert.Empty(t, filterAction(cases, "ws_flow"),
		"no exchange -> no Steps case (today's connect+receive form is used)")

	connects := filterAction(cases, "ws_connect")
	require.Len(t, connects, 1)
	assert.Equal(t, "http://x", connects[0].Target)

	receives := filterAction(cases, "ws_receive")
	// Handshake await_type "ready" + goal-named "status:ok".
	assert.ElementsMatch(t, []string{"ready", "status:ok"}, bodyTypes(receives))
	for _, r := range receives {
		assert.Contains(t, []string(r.DependsOn), connects[0].ID,
			"receive case must depend on the connect case")
	}
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

// TestWSCasesCollidingTypesDedupToOneCase pins the cross-source ID-collision
// fix: two decisive types that sanitize to the same case ID (a handshake
// await_type "devices-sync" and a goal-named "devices:sync" both -> "devices-sync")
// must dedup to a single receive case rather than emitting two cases with one ID
// (which would corrupt the DependsOn graph). The handshake spelling wins because
// it is added first.
func TestWSCasesCollidingTypesDedupToOneCase(t *testing.T) {
	cfg := &project.Config{Services: []project.Service{{
		Name: "rt", URL: "http://x",
		Protocol: &project.Protocol{TypePath: "type", Roles: map[string]*project.ProtocolRole{
			"web": {Handshake: &project.RoleHandshake{AwaitType: "devices-sync", Timeout: 5}},
		}},
	}}}
	// Goal names the same routing type with a colon; sanitizeTypeID collapses
	// "devices-sync" and "devices:sync" to one case ID.
	cases := WSCases(cfg, "verify devices:sync")
	receives := filterAction(cases, "ws_receive")
	require.Len(t, receives, 1,
		"colliding types must dedup to one case, not duplicate the sanitized ID")
	assert.Equal(t, []string{"devices-sync"}, bodyTypes(receives),
		"handshake spelling (added first) wins on collision")
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
	require.Len(t, c.Steps, 3)
	require.Equal(t, "ws_connect", c.Steps[0].Action) // web (receiver) first
	require.Equal(t, "web", c.Steps[0].ConnectionID)
	require.Equal(t, "ws_connect", c.Steps[1].Action) // bridge (peer)
	require.Equal(t, "bridge", c.Steps[1].ConnectionID)
	require.Equal(t, "ws_receive", c.Steps[2].Action)
	require.Equal(t, "device:online", c.Steps[2].Type)
	require.Equal(t, "web", c.Steps[2].ConnectionID)
	require.Equal(t, 2, c.Steps[2].Timeout)
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
		map[string]map[string]bool{"rt": {"web": true}})
	// No deterministic relay case (web is covered by an LLM ws_relay).
	for _, c := range got {
		require.NotContains(t, c.ID, "relay-web-signal", "deterministic relay dropped when LLM covers the receiver")
	}
	// web is covered at the role level → no web cases at all; bridge still connects.
	var sawBridgeConnect bool
	for _, c := range got {
		require.NotContains(t, c.ID, "-web-", "web fully skipped (LLM-covered)")
		if c.Action == "ws_connect" && strings.Contains(c.ID, "-bridge-") {
			sawBridgeConnect = true
		}
	}
	require.True(t, sawBridgeConnect, "uncovered bridge role still connects")
}
