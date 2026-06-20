package contract

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestContract_PrioritiesTolerantShapes reproduces a real parse failure: a
// non-Claude LLM returned priorities as {"check":"level"} (map[string]string)
// but Contract.Priorities was map[string][]string, so unmarshaling the coverage
// contract failed and Scout dropped the coverage gate. Both shapes must decode.
func TestContract_PrioritiesTolerantShapes(t *testing.T) {
	t.Run("string values (GLM-style)", func(t *testing.T) {
		raw := `{"health_check": "critical", "root_response": "high"}`
		var p Priorities
		require.NoError(t, json.Unmarshal([]byte(raw), &p))
		assert.Equal(t, []string{"critical"}, p["health_check"])
		assert.Equal(t, []string{"high"}, p["root_response"])
	})

	t.Run("string-slice values (documented shape)", func(t *testing.T) {
		raw := `{"high": ["go/build", "cmd/cerberus"]}`
		var p Priorities
		require.NoError(t, json.Unmarshal([]byte(raw), &p))
		assert.Equal(t, []string{"go/build", "cmd/cerberus"}, p["high"])
	})

	t.Run("full coverage contract from a fenced LLM response", func(t *testing.T) {
		// Exact shape observed in a real run (the cause of the parse failure).
		llm := `{
  "depth": "standard",
  "scope": ["GET /"],
  "path_types": ["happy", "alternative"],
  "error_scope": ["4xx", "validation"],
  "boundaries": ["empty", "zero", "max", "invalid"],
  "priorities": {"health_check": "critical", "root_response": "high"},
  "coverage_gate": {"module": "build_selftest", "line_threshold": 100.0}
}`
		var c Contract
		require.NoError(t, json.Unmarshal([]byte(llm), &c), "contract must decode the LLM response")
		assert.Equal(t, "standard", c.Depth)
		assert.Equal(t, "build_selftest", c.CoverageGate.Module)
		assert.InDelta(t, 100.0, c.CoverageGate.LineThreshold, 0.001)
		assert.Equal(t, []string{"critical"}, c.Priorities["health_check"])
	})
}
