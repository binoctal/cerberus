package scout

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/project"
)

func relayProtocol() *project.Protocol {
	return &project.Protocol{TypePath: "type",
		Roles: map[string]*project.ProtocolRole{
			"web":    {Params: map[string]string{"type": "web"}, Handshake: &project.RoleHandshake{AwaitType: "device:online", Optional: true, Timeout: 2}},
			"bridge": {Params: map[string]string{"type": "bridge"}},
		}}
}

// TestExpandWSRelayCases_HappyPath: a well-formed intent expands to a Steps case
// that connects each role (in intent order) then runs the ordered send/receive,
// and reports both roles covered for the service.
func TestExpandWSRelayCases_HappyPath(t *testing.T) {
	cfg := &project.Config{Services: []project.Service{{Name: "rt", URL: "ws://h/ws", Protocol: relayProtocol()}}}
	body, _ := json.Marshal(map[string]any{
		"roles": []string{"web", "bridge"},
		"steps": []map[string]any{
			{"do": "receive", "role": "web", "type": "device:online"},
			{"do": "send", "role": "web", "type": "session:start"},
			{"do": "receive", "role": "web", "type": "session:created", "assert": map[string]any{"payload.ready": true}},
		},
	})
	plan := &agent.TestPlan{Cases: []agent.TestCase{
		{ID: "x", Action: "ws_relay", Service: "rt", Body: string(body), Name: "relay", Expectation: "relay works"},
		{ID: "y", Action: "api_request"}, // non-relay case is untouched
	}}

	covered := expandWSRelayCases(cfg, plan)

	require.Len(t, plan.Cases, 2, "ws_relay replaced in place, api_request kept")
	require.Equal(t, "api_request", plan.Cases[1].Action, "non-relay case untouched")
	got := plan.Cases[0]
	require.Equal(t, "ws-rt-relay-bridge-web", got.ID, "deterministic ID with sorted roles")
	require.Equal(t, "ws://h/ws", got.Target, "target from service URL")
	require.Equal(t, "rt", got.Service)
	require.Equal(t, "relay", got.Name, "LLM name preserved")
	require.NotEmpty(t, got.Steps)
	// connect order == intent roles order (web first).
	require.Equal(t, "ws_connect", got.Steps[0].Action)
	require.Equal(t, "web", got.Steps[0].ConnectionID)
	require.Equal(t, "web", got.Steps[0].Role)
	require.Equal(t, "ws_connect", got.Steps[1].Action)
	require.Equal(t, "bridge", got.Steps[1].ConnectionID)
	// ordered send/receive with role/type/assert.
	require.Equal(t, "ws_receive", got.Steps[2].Action)
	require.Equal(t, "device:online", got.Steps[2].Type)
	require.Equal(t, 2, got.Steps[2].Timeout, "receive timeout from web role handshake")
	require.Equal(t, "ws_send", got.Steps[3].Action)
	require.Equal(t, `{"type":"session:start"}`, got.Steps[3].Message)
	require.Equal(t, "ws_receive", got.Steps[4].Action)
	require.Equal(t, map[string]any{"payload.ready": true}, got.Steps[4].Asserts)
	// covered reported for both roles.
	require.True(t, covered["rt"]["web"])
	require.True(t, covered["rt"]["bridge"])
}

// TestExpandWSRelayCases_DropsInvalid: every malformed intent is dropped (the
// ws_relay case removed) and other cases survive; covered is empty.
func TestExpandWSRelayCases_DropsInvalid(t *testing.T) {
	cfg := &project.Config{Services: []project.Service{{Name: "rt", URL: "ws://h/ws", Protocol: relayProtocol()}}}
	cases := []struct {
		name string
		body string
	}{
		{"bad json", `not json`},
		{"fewer than 2 roles", `{"roles":["web"],"steps":[]}`},
		{"duplicate role", `{"roles":["web","web"],"steps":[]}`},
		{"unknown role", `{"roles":["web","ghost"],"steps":[]}`},
		{"unknown service", `{"roles":["web","bridge"],"steps":[]}`}, // service mismatch handled below
		{"empty type", `{"roles":["web","bridge"],"steps":[{"do":"send","role":"web","type":""}]}`},
		{"step role not in roles", `{"roles":["web","bridge"],"steps":[{"do":"send","role":"ghost","type":"x"}]}`},
		{"bad do", `{"roles":["web","bridge"],"steps":[{"do":"push","role":"web","type":"x"}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := "rt"
			if tc.name == "unknown service" {
				svc = "nope"
			}
			plan := &agent.TestPlan{Cases: []agent.TestCase{
				{ID: "r", Action: "ws_relay", Service: svc, Body: tc.body},
				{ID: "keep", Action: "api_request"},
			}}
			covered := expandWSRelayCases(cfg, plan)
			require.Len(t, plan.Cases, 1, "%s: invalid ws_relay dropped", tc.name)
			require.Equal(t, "keep", plan.Cases[0].ID)
			require.Empty(t, covered, "%s: nothing covered", tc.name)
		})
	}
}

// TestExpandWSRelayCases_NilSafe: nil cfg/plan is a no-op returning empty covered.
func TestExpandWSRelayCases_NilSafe(t *testing.T) {
	require.Empty(t, expandWSRelayCases(nil, &agent.TestPlan{}))
	require.Empty(t, expandWSRelayCases(&project.Config{}, nil))
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
