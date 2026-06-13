package types

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Validate() happy-path tests ---

func TestValidate_HappyPath(t *testing.T) {
	tests := []struct {
		name   string
		action TypedAction
	}{
		{"HTTPAction", HTTPAction{Method: "GET", URL: "http://example.com"}},
		{"NavigateAction", NavigateAction{URL: "http://example.com"}},
		{"WaitAction empty", WaitAction{}},
		{"WaitAction with duration", WaitAction{Duration: "5s"}},
		{"ProcessExecAction", ProcessExecAction{Command: "go test"}},
		{"ProcessExecAction with timeout", ProcessExecAction{Command: "go test", Timeout: "30s"}},
		{"BuildAction", BuildAction{ProcessExecAction: ProcessExecAction{Command: "go build"}}},
		{"FileReadAction", FileReadAction{Path: "/tmp/file"}},
		{"FileWriteAction", FileWriteAction{Path: "/tmp/file", Content: "hello"}},
		{"FileExistsAction", FileExistsAction{Path: "/tmp/file"}},
		{"FileGlobAction", FileGlobAction{Pattern: "*.go"}},
		{"MCPCallAction", MCPCallAction{Server: "fs", Method: "read"}},
		{"CodeAnalyzeAction", CodeAnalyzeAction{TargetPath: "./src"}},
		{"CodeLintAction", CodeLintAction{TargetPath: "./src"}},
		{"CodeSymbolsAction", CodeSymbolsAction{TargetPath: "./src"}},
		{"BrowserGotoAction", BrowserGotoAction{URL: "http://example.com"}},
		{"BrowserClickAction", BrowserClickAction{Selector: "#btn"}},
		{"BrowserFillAction", BrowserFillAction{Selector: "#input", Value: "hello"}},
		{"BrowserEvalAction", BrowserEvalAction{Expression: "document.title"}},
		{"DBQueryAction", DBQueryAction{Driver: "sqlite", Query: "SELECT 1"}},
		{"DBAssertAction", DBAssertAction{Driver: "sqlite", Query: "SELECT 1", Assertion: "count == 1"}},
		{"GraphQLQueryAction", GraphQLQueryAction{URL: "http://api/graphql", Query: "{ users { id } }"}},
		{"WSConnectAction", WSConnectAction{URL: "ws://example.com/ws"}},
		{"WSSendAction", WSSendAction{URL: "ws://example.com/ws", Message: "hello"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NoError(t, tt.action.Validate())
		})
	}
}

// --- Validate() error-path tests ---

func TestValidate_Errors(t *testing.T) {
	tests := []struct {
		name        string
		action      TypedAction
		errContains string
	}{
		{"HTTPAction missing url", HTTPAction{}, "url is required"},
		{"NavigateAction missing url", NavigateAction{}, "url is required"},
		{"WaitAction invalid duration", WaitAction{Duration: "abc"}, "invalid timeout"},
		{"ProcessExecAction missing command", ProcessExecAction{}, "command is required"},
		{"ProcessExecAction invalid timeout", ProcessExecAction{Command: "go", Timeout: "xyz"}, "invalid timeout"},
		{"FileReadAction missing path", FileReadAction{}, "path is required"},
		{"FileWriteAction missing path", FileWriteAction{Content: "x"}, "path is required"},
		{"FileExistsAction missing path", FileExistsAction{}, "path is required"},
		{"FileGlobAction missing pattern", FileGlobAction{}, "pattern is required"},
		{"MCPCallAction missing method", MCPCallAction{Server: "fs"}, "method is required"},
		{"CodeAnalyzeAction missing path", CodeAnalyzeAction{}, "target_path is required"},
		{"CodeLintAction missing path", CodeLintAction{}, "target_path is required"},
		{"CodeSymbolsAction missing path", CodeSymbolsAction{}, "target_path is required"},
		{"BrowserGotoAction missing url", BrowserGotoAction{}, "url is required"},
		{"BrowserClickAction missing selector", BrowserClickAction{}, "selector is required"},
		{"BrowserFillAction missing selector", BrowserFillAction{Value: "x"}, "selector is required"},
		{"BrowserEvalAction missing expression", BrowserEvalAction{}, "expression is required"},
		{"DBQueryAction missing query", DBQueryAction{Driver: "sqlite"}, "query is required"},
		{"DBQueryAction missing driver", DBQueryAction{Query: "SELECT 1"}, "driver is required"},
		{"DBAssertAction missing query", DBAssertAction{Driver: "sqlite", Assertion: "count == 1"}, "query is required"},
		{"DBAssertAction missing assertion", DBAssertAction{Driver: "sqlite", Query: "SELECT 1"}, "assertion is required"},
		{"DBAssertAction missing driver", DBAssertAction{Query: "SELECT 1", Assertion: "count == 1"}, "driver is required"},
		{"GraphQLQueryAction missing url", GraphQLQueryAction{Query: "{ }"}, "url is required"},
		{"GraphQLQueryAction missing query", GraphQLQueryAction{URL: "http://x"}, "query is required"},
		{"WSConnectAction missing url", WSConnectAction{}, "url is required"},
		{"WSSendAction missing url", WSSendAction{Message: "hi"}, "url is required"},
		{"WSSendAction missing message", WSSendAction{URL: "ws://x"}, "message is required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.action.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errContains)
		})
	}
}

// --- GetActionType() tests ---

func TestGetActionType(t *testing.T) {
	tests := []struct {
		action   TypedAction
		expected ActionType
	}{
		{HTTPAction{}, ActionAPIRequest},
		{NavigateAction{}, ActionNavigate},
		{WaitAction{}, ActionWait},
		{ProcessExecAction{}, ActionProcessExec},
		{BuildAction{}, ActionProcessBuild},
		{FileReadAction{}, ActionFileRead},
		{FileWriteAction{}, ActionFileWrite},
		{FileExistsAction{}, ActionFileExists},
		{FileGlobAction{}, ActionFileGlob},
		{MCPCallAction{}, ActionMCPCall},
		{CodeAnalyzeAction{}, ActionCodeAnalyze},
		{CodeLintAction{}, ActionCodeLint},
		{CodeSymbolsAction{}, ActionCodeSymbols},
		{BrowserGotoAction{}, ActionBrowserGoto},
		{BrowserClickAction{}, ActionBrowserClick},
		{BrowserFillAction{}, ActionBrowserFill},
		{BrowserEvalAction{}, ActionBrowserEval},
		{DBQueryAction{}, ActionDBQuery},
		{DBAssertAction{}, ActionDBAssert},
		{GraphQLQueryAction{}, ActionGraphQLQuery},
		{WSConnectAction{}, ActionWSConnect},
		{WSSendAction{}, ActionWSSend},
	}

	for _, tt := range tests {
		t.Run(string(tt.expected), func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.action.GetActionType())
		})
	}
}

// --- Target() tests ---

func TestTarget(t *testing.T) {
	assert.Equal(t, "http://x", HTTPAction{URL: "http://x"}.Target())
	assert.Equal(t, "http://x", NavigateAction{URL: "http://x"}.Target())
	assert.Equal(t, "", WaitAction{}.Target())
	assert.Equal(t, "go test", ProcessExecAction{Command: "go test"}.Target())
	assert.Equal(t, "/tmp/f", FileReadAction{Path: "/tmp/f"}.Target())
	assert.Equal(t, "*.go", FileGlobAction{Pattern: "*.go"}.Target())
	assert.Equal(t, "fs/read", MCPCallAction{Server: "fs", Method: "read"}.Target())
	assert.Equal(t, "//method", MCPCallAction{Method: "/method"}.Target()) // empty server
	assert.Equal(t, "#btn", BrowserClickAction{Selector: "#btn"}.Target())
	assert.Equal(t, "document.title", BrowserEvalAction{Expression: "document.title"}.Target())
}

// --- MarshalAction / UnmarshalAction round-trip ---

func TestMarshalUnmarshalRoundTrip(t *testing.T) {
	actions := []TypedAction{
		HTTPAction{Method: "POST", URL: "http://api/users", Body: `{"name":"test"}`},
		NavigateAction{URL: "http://example.com/page"},
		WaitAction{Duration: "3s"},
		ProcessExecAction{Command: "go test", Args: []string{"-v"}},
		FileReadAction{Path: "/tmp/file.txt"},
		FileWriteAction{Path: "/tmp/out.txt", Content: "data"},
		FileExistsAction{Path: "/tmp/check"},
		FileGlobAction{Pattern: "**/*.go"},
		MCPCallAction{Server: "fs", Method: "read", Params: map[string]any{"path": "/tmp"}},
		CodeAnalyzeAction{TargetPath: "./src", Language: "go"},
		CodeLintAction{TargetPath: "./src", Rules: []string{"unused"}},
		CodeSymbolsAction{TargetPath: "./src"},
		BrowserGotoAction{URL: "http://example.com", WaitUntil: "networkidle"},
		BrowserClickAction{Selector: "#submit", Text: "Go"},
		BrowserFillAction{Selector: "#email", Value: "test@test.com"},
		BrowserEvalAction{Expression: "document.querySelectorAll('.item').length"},
		DBQueryAction{Driver: "sqlite", DSN: ":memory:", Query: "SELECT 1"},
		DBAssertAction{Driver: "postgres", Query: "SELECT count(*) FROM users", Assertion: "count > 0"},
		GraphQLQueryAction{URL: "http://api/gql", Query: "{ users { id } }"},
		WSConnectAction{URL: "ws://example.com/ws"},
		WSSendAction{URL: "ws://example.com/ws", Message: `{"type":"ping"}`},
	}

	for _, original := range actions {
		t.Run(string(original.GetActionType()), func(t *testing.T) {
			envelope, err := MarshalAction(original)
			require.NoError(t, err)
			assert.Equal(t, original.GetActionType(), envelope.Type)
			assert.NotEmpty(t, envelope.Raw)

			got, err := UnmarshalAction(envelope)
			require.NoError(t, err)
			assert.Equal(t, original, got)
		})
	}
}

// --- UnmarshalAction error paths ---

func TestUnmarshalAction_UnknownType(t *testing.T) {
	envelope := ActionEnvelope{
		Type: "unknown_action",
		Raw:  json.RawMessage(`{}`),
	}
	_, err := UnmarshalAction(envelope)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown action type")
}

func TestUnmarshalAction_InvalidJSON(t *testing.T) {
	envelope := ActionEnvelope{
		Type: ActionAPIRequest,
		Raw:  json.RawMessage(`{invalid}`),
	}
	_, err := UnmarshalAction(envelope)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unmarshal")
}

func TestUnmarshalAction_ValidationFails(t *testing.T) {
	envelope := ActionEnvelope{
		Type: ActionAPIRequest,
		Raw:  json.RawMessage(`{"method":"GET"}`), // no URL
	}
	_, err := UnmarshalAction(envelope)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "url is required")
}

// --- BuildAction unwrap ---

func TestBuildAction_Unwrap(t *testing.T) {
	inner := ProcessExecAction{Command: "go build", Args: []string{"-o", "bin/app"}}
	b := BuildAction{ProcessExecAction: inner}
	assert.Equal(t, ActionProcessBuild, b.GetActionType())
	assert.Equal(t, inner, b.Unwrap())
	assert.Equal(t, "go build", b.Target())
}

func TestToolDefinitions(t *testing.T) {
	defs := ToolDefinitions()
	assert.NotEmpty(t, defs, "should return tool definitions")

	// Check that core tools are present.
	names := make(map[string]bool)
	for _, d := range defs {
		names[d.Name] = true
		assert.NotEmpty(t, d.Description, "%s should have description", d.Name)
		assert.NotNil(t, d.InputSchema, "%s should have input schema", d.Name)
	}

	for _, expected := range []string{"api_request", "file_read", "file_write", "process_exec", "browser_goto", "db_query", "code_analyze", "wait", "mcp_call"} {
		assert.True(t, names[expected], "should include tool: %s", expected)
	}
}
