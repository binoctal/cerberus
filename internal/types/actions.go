// Package types defines shared action and result types for the multi-executor architecture.
// Both agent and policy packages import this to avoid circular dependencies.
package types

// ActionType enumerates all supported action types.
type ActionType string

const (
	// HTTP / API
	ActionAPIRequest ActionType = "api_request"
	ActionNavigate   ActionType = "navigate"
	ActionWait       ActionType = "wait"

	// Process execution
	ActionProcessExec  ActionType = "process_exec"
	ActionProcessBuild ActionType = "process_build"

	// File operations
	ActionFileRead   ActionType = "file_read"
	ActionFileWrite  ActionType = "file_write"
	ActionFileExists ActionType = "file_exists"
	ActionFileGlob   ActionType = "file_glob"

	// MCP calls
	ActionMCPCall ActionType = "mcp_call"

	// Code analysis
	ActionCodeAnalyze ActionType = "code_analyze"
	ActionCodeLint    ActionType = "code_lint"
	ActionCodeSymbols ActionType = "code_symbols"

	// Browser automation
	ActionBrowserGoto  ActionType = "browser_goto"
	ActionBrowserClick ActionType = "browser_click"
	ActionBrowserFill  ActionType = "browser_fill"
	ActionBrowserEval  ActionType = "browser_eval"

	// Database
	ActionDBQuery  ActionType = "db_query"
	ActionDBAssert ActionType = "db_assert"

	// GraphQL
	ActionGraphQLQuery ActionType = "graphql_query"

	// WebSocket
	ActionWSConnect ActionType = "ws_connect"
	ActionWSSend    ActionType = "ws_send"
)
