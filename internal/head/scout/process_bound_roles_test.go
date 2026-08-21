package scout

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/project"
)

func pbSvc() project.Service {
	return project.Service{Name: "rt", URL: "http://localhost:8989/ws/u", Protocol: &project.Protocol{
		TypePath: "type",
		Auth:     &project.ProtocolAuth{Strategy: "query", Param: "token", CredentialRef: "web-actor"},
		Roles: map[string]*project.ProtocolRole{
			"web":    {CredentialRef: "web-actor"},
			"bridge": {CredentialRef: "bridge-pty-1", ProcessBound: true},
		},
	}}
}

// The tc-004 dogfood failure (2026-08-19 run 9): the LLM scout authored a
// ws_flow whose bridge-side ws_connect resolves to a real-process actor with
// no injectable token, so the executor failed at injectAuth ("no token for
// actor") on every run. A process-bound role marks that constraint
// machine-readably; cases connecting as such a role are dropped at plan time.
func TestFilterProcessBoundConnects(t *testing.T) {
	cfg := &project.Config{Services: []project.Service{pbSvc()}}
	plan := &agent.TestPlan{Cases: []agent.TestCase{
		{ID: "relay", Action: "ws_flow", Steps: []agent.TestStep{
			{Action: "ws_connect", ConnectionID: "web", Role: "web"},
			{Action: "ws_connect", ConnectionID: "bridge", Role: "bridge"},
			{Action: "ws_send", ConnectionID: "web", Message: "{}"},
		}},
		{ID: "web-only", Action: "ws_flow", Steps: []agent.TestStep{
			{Action: "ws_connect", ConnectionID: "web", Role: "web"},
		}},
		{ID: "rest", Target: "/api/health", Action: "api_request"},
	}}
	filterProcessBoundConnects(plan, cfg)
	assert.Equal(t, []string{"web-only", "rest"}, caseIDs(plan.Cases),
		"only the ws_flow connecting as a process-bound role is dropped")
}

// No process-bound role declared → filter is a byte-identical no-op.
func TestFilterProcessBoundConnects_NoBoundRolesIsNoop(t *testing.T) {
	svc := pbSvc()
	svc.Protocol.Roles = map[string]*project.ProtocolRole{"web": {CredentialRef: "web-actor"}}
	cfg := &project.Config{Services: []project.Service{svc}}
	plan := &agent.TestPlan{Cases: []agent.TestCase{
		{ID: "a", Action: "ws_flow", Steps: []agent.TestStep{{Action: "ws_connect", Role: "web"}}},
	}}
	filterProcessBoundConnects(plan, cfg)
	assert.Equal(t, []string{"a"}, caseIDs(plan.Cases))
}

// No service declares a protocol → nothing to resolve → no-op.
func TestFilterProcessBoundConnects_NoProtocolIsNoop(t *testing.T) {
	cfg := &project.Config{Services: []project.Service{{Name: "api", URL: "http://h/api"}}}
	plan := &agent.TestPlan{Cases: []agent.TestCase{
		{ID: "a", Action: "ws_flow", Steps: []agent.TestStep{{Action: "ws_connect", Role: "bridge"}}},
	}}
	filterProcessBoundConnects(plan, cfg)
	assert.Equal(t, []string{"a"}, caseIDs(plan.Cases))
}

// The 2026-08-21 second run's tc-001: the scout authored GET /ws/user-1/health
// expecting 200; the SUT 426s it (the whole /ws/{userId} template subtree is
// WebSocket-only). Exact-path matching in filterWSEndpointDrift let template
// sub-paths through — they must count as WS-endpoint HTTP drift too.
func TestFilterWSEndpointDrift_TemplateSubpathIsDrift(t *testing.T) {
	cfg := &project.Config{Services: []project.Service{wsSvc("http://localhost:8989/ws/{userId}")}}
	plan := &agent.TestPlan{Cases: []agent.TestCase{
		{ID: "subpath", Target: "/ws/user-1/health", Action: "api_request"},            // template sub-path → drop
		{ID: "subpath2", Target: "/ws/unknown-user-999/health", Action: "api_request"}, // sibling substitution → drop
		{ID: "exact", Target: "/ws/{userId}", Action: "ws_connect"},                    // ws_* → keep
		{ID: "rest", Target: "/health", Action: "api_request"},                         // different tree → keep
	}}
	filterWSEndpointDrift(plan, cfg)
	assert.Equal(t, []string{"exact", "rest"}, caseIDs(plan.Cases),
		"HTTP cases on any /ws/{userId} substitution or sub-path are drift")
}
