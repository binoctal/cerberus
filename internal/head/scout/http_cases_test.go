package scout

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
