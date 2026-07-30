package scout

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRepairTools_HasStepsArray: repair_case carries an optional `steps` array
// for WS repair (items mirror agent.TestStep) and keeps `replaces` required.
// Negative: no steps property → assertion fails (RED).
func TestRepairTools_HasStepsArray(t *testing.T) {
	tools := repairTools()
	require.Len(t, tools, 1)
	props := tools[0].InputSchema["properties"].(map[string]any)

	steps, ok := props["steps"].(map[string]any)
	require.True(t, ok, "repair_case must have a `steps` property")
	assert.Equal(t, "array", steps["type"], "steps must be an array")
	items, ok := steps["items"].(map[string]any)
	require.True(t, ok, "steps items must be an object")
	itemProps := items["properties"].(map[string]any)
	for _, f := range []string{"action", "message", "type", "asserts", "match_all", "connection_id"} {
		assert.NotNil(t, itemProps[f], "steps item must expose %q", f)
	}

	required := tools[0].InputSchema["required"].([]any)
	assert.Contains(t, required, "replaces", "replaces stays required")
}
