package agent

import (
	"strings"

	"github.com/binoctal/cerberus/internal/types"
)

// isParseError checks if the error is from structured output parsing.
// It looks for common patterns that indicate JSON/structured parsing failures.
func isParseError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()

	// Check for structured output parsing errors from AI driver
	if checkParseOutputError(msg) {
		return true
	}

	// Check for JSON unmarshaling errors
	if checkJSONUnmarshalError(msg) {
		return true
	}

	// Check for JSON syntax/format errors
	if checkJSONSyntaxError(msg) {
		return true
	}

	return false
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		(len(s) > 0 && findSubstr(s, sub)))
}

func findSubstr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
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
