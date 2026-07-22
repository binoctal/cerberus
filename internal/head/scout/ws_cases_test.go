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
