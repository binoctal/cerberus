package scout

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/llm"
)

// TestAssembleRepair_PairsReplacements: each repair_case emission pairs to its
// originating failure via Replaces (one-to-one, input order); an emission whose
// `replaces` matches no failure is dropped; a failure with no emission yields no
// replacement.
func TestAssembleRepair_PairsReplacements(t *testing.T) {
	failures := []RepairInput{
		{Case: agent.TestCase{ID: "tc-1", Target: "/users", Method: "GET", Service: "api"}, Hint: agent.HintEndpointDrift, Reasoning: "404"},
		{Case: agent.TestCase{ID: "tc-2", Target: "/login", Method: "POST", Service: "api"}, Hint: agent.HintAuth, Reasoning: "401"},
	}
	calls := []llm.ToolCall{
		{Name: "repair_case", Input: map[string]any{"replaces": "tc-1", "method": "GET", "path": "/v2/users", "service": "api", "expectation": "200"}},
		{Name: "repair_case", Input: map[string]any{"replaces": "tc-2", "method": "POST", "path": "/login", "service": "api", "body": "{\"x\":1}", "expectation": "200"}},
		// Unmatched: replaces a non-existent failure -> dropped.
		{Name: "repair_case", Input: map[string]any{"replaces": "tc-9", "method": "GET", "path": "/x", "service": "api"}},
		// Duplicate for tc-1 -> dropped (one replacement per failure).
		{Name: "repair_case", Input: map[string]any{"replaces": "tc-1", "method": "GET", "path": "/v3/users", "service": "api"}},
	}

	out := assembleRepair(calls, failures)
	require.Len(t, out, 2, "one replacement per matched failure")
	assert.Equal(t, "tc-1", out[0].Replaces)
	assert.Equal(t, "/v2/users", out[0].Target)
	assert.Equal(t, "GET", out[0].Method)
	assert.Equal(t, "tc-2", out[1].Replaces)
	assert.NotEmpty(t, out[1].Body, "body carried through")

	// A failure with no matching emission produces nothing.
	out2 := assembleRepair(nil, failures[:1])
	assert.Empty(t, out2)
}
