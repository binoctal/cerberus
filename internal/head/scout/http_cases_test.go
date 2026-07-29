package scout

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
)

// TestAssemblePlan_RecordsHTTPCovering: a test_http_endpoint call records its
// (service, path) in httpCovering carrying the LLM case ID, so HTTPCasesCovered
// (Task 2) can emit a smoke fallback bound to it. Two cases on the same path
// dedupe to one entry.
func TestAssemblePlan_RecordsHTTPCovering(t *testing.T) {
	cfg := &project.Config{Services: []project.Service{{Name: "api", URL: "http://localhost:3000"}}}

	calls := []llm.ToolCall{
		{Name: "test_http_endpoint", Input: map[string]any{"method": "GET", "path": "/users", "service": "api"}},
	}
	plan, _, _, httpCovering := assemblePlan(calls, "goal", "http://localhost:3000", cfg.Services)
	require.NotEmpty(t, plan.Cases)
	primaryID := plan.Cases[0].ID
	assert.Equal(t, primaryID, httpCovering["api"]["/users"], "HTTP case recorded as /users's coverer")

	// Dedup: two cases on the same (service, path) → one covering entry (first wins).
	calls2 := []llm.ToolCall{
		{Name: "test_http_endpoint", Input: map[string]any{"method": "GET", "path": "/users", "service": "api"}},
		{Name: "test_http_endpoint", Input: map[string]any{"method": "POST", "path": "/users", "service": "api"}},
	}
	plan2, _, _, covering2 := assemblePlan(calls2, "goal", "http://localhost:3000", cfg.Services)
	require.Len(t, plan2.Cases, 2)
	assert.Equal(t, plan2.Cases[0].ID, covering2["api"]["/users"], "first /users case is the coverer")

	// No HTTP calls → empty httpCovering.
	plan3, _, _, covering3 := assemblePlan(nil, "goal", "http://localhost:3000", cfg.Services)
	assert.Empty(t, covering3["api"], "no HTTP cases → no covering")
	_ = plan3
}

// TestHTTPCasesCovered_EmitsSmokeFallback: each covered endpoint emits exactly
// one lazy GET-smoke fallback (FallbackFor bound, Priority<0, Method=GET,
// matching Target); deduped; nothing for an empty map.
func TestHTTPCasesCovered_EmitsSmokeFallback(t *testing.T) {
	cfg := &project.Config{Services: []project.Service{
		{Name: "api", URL: "http://localhost:3000"},
		{Name: "web", URL: "http://localhost:3001"},
	}}
	httpCovering := map[string]map[string]string{
		"api": {"/users": "tc-lm-1", "/posts": "tc-lm-2"},
		"web": {"/": "tc-lm-3"},
	}

	cases := HTTPCasesCovered(cfg, httpCovering)
	require.Len(t, cases, 3, "one smoke per covered endpoint")

	byTarget := map[string]agent.TestCase{}
	for _, c := range cases {
		byTarget[c.Service+c.Target] = c
	}
	u := byTarget["api/users"]
	assert.Equal(t, "tc-lm-1", u.FallbackFor, "bound to covering LLM case")
	assert.Equal(t, "GET", u.Method)
	assert.Less(t, u.Priority, 0.0, "lazy: deprioritized")
	assert.Contains(t, u.Expectation, "non-5xx")

	// Empty map → no cases.
	assert.Empty(t, HTTPCasesCovered(cfg, nil))
	// A service with no entries → no cases for it.
	assert.Empty(t, HTTPCasesCovered(cfg, map[string]map[string]string{"api": {}}))
}
