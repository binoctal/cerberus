package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/types"
)

func TestAssembleAction_APIRequest(t *testing.T) {
	c := llm.ToolCall{Name: "api_request", Input: map[string]any{
		"method": "POST", "url": "/api/users", "body": `{"name":"x"}`,
	}}
	a, err := assembleAction(c)
	require.NoError(t, err)
	ha, ok := a.(types.HTTPAction)
	require.True(t, ok)
	assert.Equal(t, "POST", ha.Method)
	assert.Equal(t, "/api/users", ha.URL)
	assert.Equal(t, `{"name":"x"}`, ha.Body)
}

func TestAssembleAction_BrowserClick(t *testing.T) {
	c := llm.ToolCall{Name: "browser_click", Input: map[string]any{"selector": "#go"}}
	a, err := assembleAction(c)
	require.NoError(t, err)
	assert.Equal(t, types.ActionBrowserClick, a.GetActionType())
}

func TestAssembleAction_ProcessExec(t *testing.T) {
	c := llm.ToolCall{Name: "process_exec", Input: map[string]any{"command": "go", "args": []any{"test", "./..."}}}
	a, err := assembleAction(c)
	require.NoError(t, err)
	pe, ok := a.(types.ProcessExecAction)
	require.True(t, ok)
	assert.Equal(t, "go", pe.Command)
	assert.Equal(t, []string{"test", "./..."}, pe.Args)
}

func TestAssembleAction_UnknownToolErrors(t *testing.T) {
	_, err := assembleAction(llm.ToolCall{Name: "nope", Input: map[string]any{}})
	assert.Error(t, err)
}

// skip is a control signal for Recover, not an action — assembleAction must reject it.
func TestAssembleAction_SkipIsNotAnAction(t *testing.T) {
	_, err := assembleAction(llm.ToolCall{Name: "skip", Input: map[string]any{}})
	assert.Error(t, err)
}

// --- per-family assembly cases (one assertion per tool name) ---

func TestAssembleAction_Navigate(t *testing.T) {
	c := llm.ToolCall{Name: "navigate", Input: map[string]any{
		"url": "/x", "wait_selector": "#ready", "wait_for": float64(2),
	}}
	a, err := assembleAction(c)
	require.NoError(t, err)
	na, ok := a.(types.NavigateAction)
	require.True(t, ok)
	assert.Equal(t, "/x", na.URL)
	assert.Equal(t, "#ready", na.WaitSelector)
	assert.Equal(t, 2, na.WaitFor)
	assert.Equal(t, types.ActionNavigate, a.GetActionType())
}

func TestAssembleAction_Wait(t *testing.T) {
	c := llm.ToolCall{Name: "wait", Input: map[string]any{
		"duration": "500ms", "selector": "#x", "wait_for_state": "visible",
	}}
	a, err := assembleAction(c)
	require.NoError(t, err)
	wa, ok := a.(types.WaitAction)
	require.True(t, ok)
	assert.Equal(t, "500ms", wa.Duration)
	assert.Equal(t, "#x", wa.Selector)
	assert.Equal(t, "visible", wa.WaitForState)
	assert.Equal(t, types.ActionWait, a.GetActionType())
}

func TestAssembleAction_FileRead(t *testing.T) {
	c := llm.ToolCall{Name: "file_read", Input: map[string]any{
		"path": "/a/b.go", "offset": float64(10), "limit": float64(20),
	}}
	a, err := assembleAction(c)
	require.NoError(t, err)
	fr, ok := a.(types.FileReadAction)
	require.True(t, ok)
	assert.Equal(t, "/a/b.go", fr.Path)
	assert.Equal(t, 10, fr.Offset)
	assert.Equal(t, 20, fr.Limit)
	assert.Equal(t, types.ActionFileRead, a.GetActionType())
}

func TestAssembleAction_FileWrite(t *testing.T) {
	c := llm.ToolCall{Name: "file_write", Input: map[string]any{
		"path": "/a/b.txt", "content": "hi", "create_parent_dirs": true, "mode": float64(0o644),
	}}
	a, err := assembleAction(c)
	require.NoError(t, err)
	fw, ok := a.(types.FileWriteAction)
	require.True(t, ok)
	assert.Equal(t, "/a/b.txt", fw.Path)
	assert.Equal(t, "hi", fw.Content)
	assert.True(t, fw.CreateParentDirs)
	assert.Equal(t, 0o644, fw.Mode)
	assert.Equal(t, types.ActionFileWrite, a.GetActionType())
}

func TestAssembleAction_FileExists(t *testing.T) {
	c := llm.ToolCall{Name: "file_exists", Input: map[string]any{"path": "/a/b"}}
	a, err := assembleAction(c)
	require.NoError(t, err)
	fe, ok := a.(types.FileExistsAction)
	require.True(t, ok)
	assert.Equal(t, "/a/b", fe.Path)
	assert.Equal(t, types.ActionFileExists, a.GetActionType())
}

func TestAssembleAction_FileGlob(t *testing.T) {
	c := llm.ToolCall{Name: "file_glob", Input: map[string]any{"pattern": "**/*.go", "path": "/root"}}
	a, err := assembleAction(c)
	require.NoError(t, err)
	fg, ok := a.(types.FileGlobAction)
	require.True(t, ok)
	assert.Equal(t, "**/*.go", fg.Pattern)
	assert.Equal(t, "/root", fg.Path)
	assert.Equal(t, types.ActionFileGlob, a.GetActionType())
}

func TestAssembleAction_BrowserGoto(t *testing.T) {
	c := llm.ToolCall{Name: "browser_goto", Input: map[string]any{"url": "/home", "wait_until": "load"}}
	a, err := assembleAction(c)
	require.NoError(t, err)
	bg, ok := a.(types.BrowserGotoAction)
	require.True(t, ok)
	assert.Equal(t, "/home", bg.URL)
	assert.Equal(t, "load", bg.WaitUntil)
	assert.Equal(t, types.ActionBrowserGoto, a.GetActionType())
}

func TestAssembleAction_BrowserFill(t *testing.T) {
	c := llm.ToolCall{Name: "browser_fill", Input: map[string]any{"selector": "#in", "value": "v"}}
	a, err := assembleAction(c)
	require.NoError(t, err)
	bf, ok := a.(types.BrowserFillAction)
	require.True(t, ok)
	assert.Equal(t, "#in", bf.Selector)
	assert.Equal(t, "v", bf.Value)
	assert.Equal(t, types.ActionBrowserFill, a.GetActionType())
}

func TestAssembleAction_BrowserEval(t *testing.T) {
	c := llm.ToolCall{Name: "browser_eval", Input: map[string]any{
		"expression": "document.title", "args": []any{float64(1), "x"},
	}}
	a, err := assembleAction(c)
	require.NoError(t, err)
	be, ok := a.(types.BrowserEvalAction)
	require.True(t, ok)
	assert.Equal(t, "document.title", be.Expression)
	require.Len(t, be.Args, 2)
	assert.Equal(t, float64(1), be.Args[0])
	assert.Equal(t, "x", be.Args[1])
	assert.Equal(t, types.ActionBrowserEval, a.GetActionType())
}

func TestAssembleAction_MCPCall(t *testing.T) {
	c := llm.ToolCall{Name: "mcp_call", Input: map[string]any{
		"server": "S", "method": "M", "params": map[string]any{"k": "v"},
	}}
	a, err := assembleAction(c)
	require.NoError(t, err)
	mc, ok := a.(types.MCPCallAction)
	require.True(t, ok)
	assert.Equal(t, "S", mc.Server)
	assert.Equal(t, "M", mc.Method)
	assert.Equal(t, map[string]any{"k": "v"}, mc.Params)
	assert.Equal(t, types.ActionMCPCall, a.GetActionType())
}

// Optional fields default to zero values when absent (schema-enforced types).
func TestAssembleAction_APIRequest_OptionalsOmitted(t *testing.T) {
	c := llm.ToolCall{Name: "api_request", Input: map[string]any{"method": "GET", "url": "/p"}}
	a, err := assembleAction(c)
	require.NoError(t, err)
	ha, _ := a.(types.HTTPAction)
	assert.Empty(t, ha.Body)
	assert.Nil(t, ha.Headers)
	assert.Zero(t, ha.Timeout)
}

// headers/env map[string]string is preserved as a string-valued map.
func TestAssembleAction_APIRequest_HeadersAndTimeout(t *testing.T) {
	c := llm.ToolCall{Name: "api_request", Input: map[string]any{
		"method": "GET", "url": "/p",
		"headers": map[string]any{"X-Trace": "1"}, "timeout": float64(5),
	}}
	a, err := assembleAction(c)
	require.NoError(t, err)
	ha, _ := a.(types.HTTPAction)
	assert.Equal(t, map[string]string{"X-Trace": "1"}, ha.Headers)
	assert.Equal(t, 5, ha.Timeout)
}

// TestActionTools_Surface pins the LLM-reachable action surface (spec §1).
// Every name in actionTools() must round-trip through assembleAction, and the
// surface must contain exactly the 13 general-purpose actions the rule engine
// does not construct itself — ws_*/code_*/db_*/graphql_query/process_build
// stay excluded. Adding or removing a tool here is a deliberate spec change.
func TestActionTools_Surface(t *testing.T) {
	tools := actionTools()

	// expected name → minimal valid input for round-trip assembly.
	cases := map[string]map[string]any{
		"api_request":   {"method": "GET", "url": "/x"},
		"navigate":      {"url": "/x"},
		"wait":          {"duration": "1s"},
		"process_exec":  {"command": "true"},
		"file_read":     {"path": "/x"},
		"file_write":    {"path": "/x", "content": "y"},
		"file_exists":   {"path": "/x"},
		"file_glob":     {"pattern": "*"},
		"browser_goto":  {"url": "/x"},
		"browser_click": {"selector": "#x"},
		"browser_fill":  {"selector": "#x", "value": "v"},
		"browser_eval":  {"expression": "1"},
		"mcp_call":      {"server": "s", "method": "m"},
	}

	names := make(map[string]bool, len(tools))
	for _, tl := range tools {
		names[tl.Name] = true
	}
	assert.Len(t, tools, len(cases), "action tool surface size changed")
	for name := range cases {
		assert.True(t, names[name], "missing tool %q in actionTools()", name)
	}

	// Every tool on the surface must round-trip through assembleAction.
	for _, tl := range tools {
		input, ok := cases[tl.Name]
		require.True(t, ok, "no fixture for tool %q — add it to cases", tl.Name)
		a, err := assembleAction(llm.ToolCall{Name: tl.Name, Input: input})
		require.NoError(t, err, "tool %q did not assemble", tl.Name)
		assert.NotNil(t, a.GetActionType(), "tool %q yielded nil TypedAction", tl.Name)
	}

	// Tools never on the LLM surface (rule-engine/phase-0 domain).
	for _, excluded := range []string{
		"ws_connect", "ws_send", "ws_receive", "ws_disconnect",
		"code_analyze", "code_lint", "code_symbols",
		"db_query", "db_assert", "graphql_query", "process_build",
		"skip", // control tool — Task 3 adds it when wiring Recovery
	} {
		assert.False(t, names[excluded], "%q must not be on the action surface yet", excluded)
	}
}
