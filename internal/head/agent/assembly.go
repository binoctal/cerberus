package agent

import (
	"fmt"

	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/types"
)

// assembleAction maps a single LLM action tool call to its types.TypedAction.
// It is the tool-calling analogue of parsing the legacy ActionEnvelope: a
// switch over call.Name that constructs the same TypedAction value the
// executor and rule engine already dispatch on.
//
// Unknown tool names return an error. `skip` is deliberately NOT handled here:
// `skip` is a control signal for Recovery.Recover (Task 3), not an action the
// executor can run, so this function must reject it. Recover inspects
// `call.Name == "skip"` directly and never delegates skip to assembleAction.
//
// Field coercion is done via the shared helpers in internal/llm/toolfield.go
// (StrField/IntField/NumField/StrSliceField/MapField/MapStringStringField/
// AnySliceField). Each helper returns its zero value when the key is absent,
// matching the optional-field semantics of every action struct.
func assembleAction(call llm.ToolCall) (types.TypedAction, error) {
	switch call.Name {
	case "api_request":
		return types.HTTPAction{
			Method:  llm.StrField(call, "method"),
			URL:     llm.StrField(call, "url"),
			Body:    llm.StrField(call, "body"),
			Headers: llm.MapStringStringField(call, "headers"),
			Timeout: llm.IntField(call, "timeout"),
		}, nil
	case "navigate":
		return types.NavigateAction{
			URL:          llm.StrField(call, "url"),
			WaitSelector: llm.StrField(call, "wait_selector"),
			WaitFor:      llm.IntField(call, "wait_for"),
		}, nil
	case "wait":
		return types.WaitAction{
			Duration:     llm.StrField(call, "duration"),
			Selector:     llm.StrField(call, "selector"),
			WaitForState: llm.StrField(call, "wait_for_state"),
		}, nil
	case "process_exec":
		return types.ProcessExecAction{
			Command: llm.StrField(call, "command"),
			Args:    llm.StrSliceField(call, "args"),
			WorkDir: llm.StrField(call, "work_dir"),
			Env:     llm.MapStringStringField(call, "env"),
			Timeout: llm.StrField(call, "timeout"),
		}, nil
	case "file_read":
		return types.FileReadAction{
			Path:   llm.StrField(call, "path"),
			Offset: llm.IntField(call, "offset"),
			Limit:  llm.IntField(call, "limit"),
		}, nil
	case "file_write":
		return types.FileWriteAction{
			Path:             llm.StrField(call, "path"),
			Content:          llm.StrField(call, "content"),
			CreateParentDirs: llm.BoolField(call, "create_parent_dirs"),
			Mode:             llm.IntField(call, "mode"),
		}, nil
	case "file_exists":
		return types.FileExistsAction{
			Path: llm.StrField(call, "path"),
		}, nil
	case "file_glob":
		return types.FileGlobAction{
			Pattern: llm.StrField(call, "pattern"),
			Path:    llm.StrField(call, "path"),
		}, nil
	case "browser_goto":
		return types.BrowserGotoAction{
			URL:       llm.StrField(call, "url"),
			WaitUntil: llm.StrField(call, "wait_until"),
		}, nil
	case "browser_click":
		return types.BrowserClickAction{
			Selector:  llm.StrField(call, "selector"),
			Text:      llm.StrField(call, "text"),
			Button:    llm.StrField(call, "button"),
			Modifiers: llm.StrSliceField(call, "modifiers"),
		}, nil
	case "browser_fill":
		return types.BrowserFillAction{
			Selector: llm.StrField(call, "selector"),
			Value:    llm.StrField(call, "value"),
		}, nil
	case "browser_eval":
		return types.BrowserEvalAction{
			Expression: llm.StrField(call, "expression"),
			Args:       llm.AnySliceField(call, "args"),
		}, nil
	case "mcp_call":
		return types.MCPCallAction{
			Server: llm.StrField(call, "server"),
			Method: llm.StrField(call, "method"),
			Params: llm.MapField(call, "params"),
		}, nil
	default:
		return nil, fmt.Errorf("assembleAction: unknown action tool %q", call.Name)
	}
}
