package agent

import (
	"github.com/binoctal/cerberus/internal/types"
)

// matchFileRules matches file operation actions
func matchFileRules(tc TestCase) (types.TypedAction, bool) {
	// Rule 9: file_read/write/exists/glob
	switch tc.Action {
	case "file_read":
		return types.FileReadAction{Path: tc.Target}, true
	case "file_write":
		return types.FileWriteAction{Path: tc.Target}, true
	case "file_exists":
		return types.FileExistsAction{Path: tc.Target}, true
	case "file_glob":
		return types.FileGlobAction{Pattern: tc.Target}, true
	}
	return nil, false
}

// matchWaitRule matches wait action
func matchWaitRule(tc TestCase) (types.TypedAction, bool) {
	// Rule 10: wait — target is the duration string.
	if tc.Action == "wait" {
		return types.WaitAction{Duration: tc.Target}, true
	}
	return nil, false
}

// matchMCPRule matches MCP call action
func matchMCPRule(tc TestCase) (types.TypedAction, bool) {
	// Rule 11: mcp_call
	if tc.Action == "mcp_call" {
		return types.MCPCallAction{
			Server: tc.Target,
			Method: tc.Method,
		}, true
	}
	return nil, false
}
