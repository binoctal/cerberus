package agent

import (
	"strings"

	"github.com/binoctal/cerberus/internal/types"
)

// contains reports whether sub appears within s. A small hand-rolled
// implementation kept for prompt-string assertions in tests (prompts_test.go).
func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		(len(s) > 0 && strings.Contains(s, sub)))
}

// isDestructiveAction checks if a TypedAction is potentially destructive.
func isDestructiveAction(action types.TypedAction) bool {
	if action == nil {
		return false
	}
	switch a := action.(type) {
	case types.HTTPAction:
		upper := strings.ToUpper(a.Method)
		return upper == "DELETE" || upper == "DROP"
	case types.ProcessExecAction:
		destructive := []string{"rm", "rmdir", "dropdb", "truncate"}
		for _, d := range destructive {
			if a.Command == d {
				return true
			}
		}
	case types.FileWriteAction:
		return true
	}
	return false
}
