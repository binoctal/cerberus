package scout

import (
	"encoding/json"
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
