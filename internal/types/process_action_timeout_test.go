package types

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestProcessExecAction_TimeoutTolerantTypes reproduces a real failure: a
// non-Claude model returned {"timeout": 30} (number) but ProcessExecAction
// .Timeout is a string. Timeout must accept both, like WaitAction.Duration.
// This also covers BuildAction, which embeds ProcessExecAction.
func TestProcessExecAction_TimeoutTolerantTypes(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"string timeout", `{"command":"go","timeout":"30s"}`, "30s"},
		{"integer timeout", `{"command":"go","timeout":30}`, "30s"},
		{"float timeout", `{"command":"go","timeout":1.5}`, "1.5s"},
		{"omitted timeout", `{"command":"go"}`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var a ProcessExecAction
			require.NoError(t, json.Unmarshal([]byte(tt.raw), &a), "input: %s", tt.raw)
			assert.Equal(t, tt.want, a.Timeout)
			require.NoError(t, a.Validate())
		})
	}

	// Args and other fields still decode through the alias path.
	t.Run("other fields preserved", func(t *testing.T) {
		var a ProcessExecAction
		require.NoError(t, json.Unmarshal([]byte(`{"command":"go","args":["build","./..."],"work_dir":".","timeout":20}`), &a))
		assert.Equal(t, "go", a.Command)
		assert.Equal(t, []string{"build", "./..."}, a.Args)
		assert.Equal(t, ".", a.WorkDir)
		assert.Equal(t, "20s", a.Timeout)
	})

	// NOTE: BuildAction embeds ProcessExecAction but is never unmarshaled from
	// LLM JSON in practice — process_build is not in the steer prompt's action
	// list and build cases are constructed in Go (rules). ProcessExecAction is
	// the only real LLM->JSON path, so it is the one covered here.
}
