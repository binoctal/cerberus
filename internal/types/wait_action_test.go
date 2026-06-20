package types

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWaitAction_DurationTolerantTypes reproduces a real failure seen with a
// non-Claude model that returned {"duration": 2} (number) instead of "2s".
// WaitAction.Duration must accept both string and numeric durations; a bare
// number is interpreted as seconds.
func TestWaitAction_DurationTolerantTypes(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"string seconds", `{"duration": "2s"}`, "2s"},
		{"string millis", `{"duration": "500ms"}`, "500ms"},
		{"integer seconds", `{"duration": 2}`, "2s"},
		{"float seconds", `{"duration": 1.5}`, "1.5s"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var a WaitAction
			require.NoError(t, json.Unmarshal([]byte(tt.raw), &a), "input: %s", tt.raw)
			assert.Equal(t, tt.want, a.Duration)
			require.NoError(t, a.Validate())
		})
	}

	// Non-duration fields still decode through the alias path.
	t.Run("other fields preserved", func(t *testing.T) {
		var a WaitAction
		require.NoError(t, json.Unmarshal([]byte(`{"duration": 1, "selector": "#x", "wait_for_state": "visible"}`), &a))
		assert.Equal(t, "1s", a.Duration)
		assert.Equal(t, "#x", a.Selector)
		assert.Equal(t, "visible", a.WaitForState)
	})
}
