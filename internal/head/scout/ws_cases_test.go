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

// TestWSCasesSendVerbTokenNotReceive is the behavioral proof for #2: a
// verb-phrased goal must not produce a ws_receive case for a client-sent type.
func TestWSCasesSendVerbTokenNotReceive(t *testing.T) {
	cfg := &project.Config{Services: []project.Service{{
		Name: "rt", URL: "http://x",
		Protocol: &project.Protocol{TypePath: "type", Roles: map[string]*project.ProtocolRole{
			"web": {Handshake: &project.RoleHandshake{AwaitType: "devices:sync", Timeout: 5}},
		}},
	}}}
	cases := WSCases(cfg, "send device:command, verify device:ack")
	types := bodyTypes(filterAction(cases, "ws_receive"))
	// Exactly two receives: the handshake await_type devices:sync and the
	// goal-named device:ack — device:command must not add a third.
	require.Len(t, types, 2,
		"exactly devices:sync + device:ack receives; no spurious client-sent case")
	// device:command is client-sent (send-verb) -> NOT a receive target.
	assert.NotContains(t, types, "device:command",
		"a client-sent type (send device:command) must not become a ws_receive case")
	// device:ack (verify) and devices:sync (handshake await_type) remain.
	assert.Contains(t, types, "device:ack")
	assert.Contains(t, types, "devices:sync")
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
