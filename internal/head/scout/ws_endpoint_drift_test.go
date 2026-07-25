package scout

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/project"
)

func wsSvc(url string) project.Service {
	return project.Service{Name: "rt", URL: url, Protocol: &project.Protocol{TypePath: "type"}}
}

func caseIDs(cases []agent.TestCase) []string {
	ids := make([]string, 0, len(cases))
	for _, c := range cases {
		ids = append(ids, c.ID)
	}
	return ids
}

// Finding-3: a non-ws_* action on a WS endpoint is HTTP drift (426) → dropped.
// HTTP REST exploration (different path) and ws_* attempts on the WS endpoint
// are kept. Deterministic WS cases (ws_* action) are kept.
func TestFilterWSEndpointDrift(t *testing.T) {
	cfg := &project.Config{Services: []project.Service{wsSvc("http://localhost:8989/ws/user_x")}}
	plan := &agent.TestPlan{Cases: []agent.TestCase{
		{ID: "drift", Target: "/ws/user_x", Action: "api_request"},                            // WS endpoint + HTTP → drop
		{ID: "rest", Target: "/api/v1/health", Action: "api_request"},                         // HTTP REST → keep
		{ID: "ws-conn", Target: "/ws/user_x", Action: "ws_connect"},                           // ws_* on WS → keep
		{ID: "ws-flow", Target: "/ws/user_x", Action: "ws_flow", Steps: []agent.TestStep{{}}}, // deterministic → keep
	}}
	filterWSEndpointDrift(plan, cfg)
	assert.Equal(t, []string{"rest", "ws-conn", "ws-flow"}, caseIDs(plan.Cases),
		"only the WS-endpoint HTTP-drift case is dropped")
}

// No service declares a protocol → filter is a byte-identical no-op.
func TestFilterWSEndpointDrift_NoProtocolIsNoop(t *testing.T) {
	cfg := &project.Config{Services: []project.Service{{Name: "api", URL: "http://h/api"}}}
	before := []agent.TestCase{
		{ID: "a", Target: "/api", Action: "api_request"},
		{ID: "b", Target: "/ws/x", Action: "api_request"}, // no protocol declared → not a WS endpoint
	}
	plan := &agent.TestPlan{Cases: append([]agent.TestCase{}, before...)}
	filterWSEndpointDrift(plan, cfg)
	require.Len(t, plan.Cases, len(before), "no protocol → nothing dropped")
	assert.Equal(t, []string{"a", "b"}, caseIDs(plan.Cases))
}

// An empty or unparseable target never matches a WS path → kept (no false drop).
func TestFilterWSEndpointDrift_EmptyTargetKept(t *testing.T) {
	cfg := &project.Config{Services: []project.Service{wsSvc("http://h/ws/u")}}
	plan := &agent.TestPlan{Cases: []agent.TestCase{
		{ID: "empty", Target: "", Action: "api_request"},
	}}
	filterWSEndpointDrift(plan, cfg)
	assert.Equal(t, []string{"empty"}, caseIDs(plan.Cases))
}

func TestUrlPathOf(t *testing.T) {
	assert.Equal(t, "/ws/user_x", urlPathOf("http://localhost:8989/ws/user_x?token=t"))
	assert.Equal(t, "/ws/user_x", urlPathOf("/ws/user_x"))
	assert.Equal(t, "/ws/user_x", urlPathOf("ws://localhost:8989/ws/user_x"))
	assert.Equal(t, "", urlPathOf(""))                       // empty → no path
	assert.Equal(t, "example.com", urlPathOf("example.com")) // bare host (no scheme/colon) → path-only parse (won't match a WS path)
	assert.Equal(t, "", urlPathOf("ht tp://x"))              // unparseable (space in URL) → ""
}

func TestIsWSAction(t *testing.T) {
	for _, a := range []string{"ws_connect", "ws_send", "ws_receive", "ws_disconnect", "ws_flow"} {
		assert.True(t, isWSAction(a), "%s is a WS action", a)
	}
	for _, a := range []string{"api_request", "process", "", "http_request"} {
		assert.False(t, isWSAction(a), "%q is not a WS action", a)
	}
}
