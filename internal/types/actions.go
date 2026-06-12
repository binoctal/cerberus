// Package types defines shared action and result types for the multi-executor architecture.
// Both agent and policy packages import this to avoid circular dependencies.
package types

import (
	"encoding/json"
	"fmt"
	"time"
)

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
)

// TypedAction is the interface for all concrete action types.
type TypedAction interface {
	GetActionType() ActionType
	Validate() error
	Target() string
}

// ActionEnvelope is the unified envelope for serialization and routing.
type ActionEnvelope struct {
	Type ActionType      `json:"type"`
	Raw  json.RawMessage `json:"raw"`
}

// --- HTTP Actions ---

type HTTPAction struct {
	Method  string            `json:"method,omitempty"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    string            `json:"body,omitempty"`
}

func (a HTTPAction) GetActionType() ActionType { return ActionAPIRequest }
func (a HTTPAction) Target() string             { return a.URL }
func (a HTTPAction) Validate() error {
	if a.URL == "" {
		return fmt.Errorf("url is required")
	}
	return nil
}

type NavigateAction struct {
	URL string `json:"url"`
}

func (a NavigateAction) GetActionType() ActionType { return ActionNavigate }
func (a NavigateAction) Target() string             { return a.URL }
func (a NavigateAction) Validate() error {
	if a.URL == "" {
		return fmt.Errorf("url is required")
	}
	return nil
}

type WaitAction struct {
	Duration string `json:"duration"`
}

func (a WaitAction) GetActionType() ActionType { return ActionWait }
func (a WaitAction) Target() string             { return "" }
func (a WaitAction) Validate() error {
	if a.Duration != "" {
		if _, err := time.ParseDuration(a.Duration); err != nil {
			return fmt.Errorf("invalid timeout %q: %w", a.Duration, err)
		}
	}
	return nil
}

// --- Process Actions ---

type ProcessExecAction struct {
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	WorkDir string            `json:"work_dir,omitempty"`
	Timeout string            `json:"timeout,omitempty"`
}

// BuildAction wraps a ProcessExecAction to mark it as a build action.
// It preserves the underlying action but overrides GetActionType.
type BuildAction struct {
	ProcessExecAction
}

func (a BuildAction) GetActionType() ActionType { return ActionProcessBuild }
func (a BuildAction) Unwrap() ProcessExecAction  { return a.ProcessExecAction }

func (a ProcessExecAction) GetActionType() ActionType { return ActionProcessExec }
func (a ProcessExecAction) Target() string             { return a.Command }
func (a ProcessExecAction) Validate() error {
	if a.Command == "" {
		return fmt.Errorf("command is required")
	}
	if a.Timeout != "" {
		if _, err := time.ParseDuration(a.Timeout); err != nil {
			return fmt.Errorf("invalid timeout %q: %w", a.Timeout, err)
		}
	}
	return nil
}

// --- File Actions ---

type FileReadAction struct {
	Path string `json:"path"`
}

func (a FileReadAction) GetActionType() ActionType { return ActionFileRead }
func (a FileReadAction) Target() string             { return a.Path }
func (a FileReadAction) Validate() error {
	if a.Path == "" {
		return fmt.Errorf("path is required")
	}
	return nil
}

type FileWriteAction struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func (a FileWriteAction) GetActionType() ActionType { return ActionFileWrite }
func (a FileWriteAction) Target() string             { return a.Path }
func (a FileWriteAction) Validate() error {
	if a.Path == "" {
		return fmt.Errorf("path is required")
	}
	return nil
}

type FileExistsAction struct {
	Path string `json:"path"`
}

func (a FileExistsAction) GetActionType() ActionType { return ActionFileExists }
func (a FileExistsAction) Target() string             { return a.Path }
func (a FileExistsAction) Validate() error {
	if a.Path == "" {
		return fmt.Errorf("path is required")
	}
	return nil
}

type FileGlobAction struct {
	Pattern string `json:"pattern"`
}

func (a FileGlobAction) GetActionType() ActionType { return ActionFileGlob }
func (a FileGlobAction) Target() string             { return a.Pattern }
func (a FileGlobAction) Validate() error {
	if a.Pattern == "" {
		return fmt.Errorf("pattern is required")
	}
	return nil
}

// --- MCP Actions ---

type MCPCallAction struct {
	Server string         `json:"server"`
	Method string         `json:"method"`
	Params map[string]any `json:"params,omitempty"`
}

func (a MCPCallAction) GetActionType() ActionType { return ActionMCPCall }
func (a MCPCallAction) Target() string             { return a.Server + "/" + a.Method }
func (a MCPCallAction) Validate() error {
	if a.Method == "" {
		return fmt.Errorf("method is required")
	}
	return nil
}

// --- Code Actions ---

type CodeAnalyzeAction struct {
	TargetPath string   `json:"target_path"`
	Language   string   `json:"language,omitempty"`
	Checks     []string `json:"checks,omitempty"`
}

func (a CodeAnalyzeAction) GetActionType() ActionType { return ActionCodeAnalyze }
func (a CodeAnalyzeAction) Target() string             { return a.TargetPath }
func (a CodeAnalyzeAction) Validate() error {
	if a.TargetPath == "" {
		return fmt.Errorf("target_path is required")
	}
	return nil
}

type CodeLintAction struct {
	TargetPath string   `json:"target_path"`
	Language   string   `json:"language,omitempty"`
	Rules      []string `json:"rules,omitempty"`
}

func (a CodeLintAction) GetActionType() ActionType { return ActionCodeLint }
func (a CodeLintAction) Target() string             { return a.TargetPath }
func (a CodeLintAction) Validate() error {
	if a.TargetPath == "" {
		return fmt.Errorf("target_path is required")
	}
	return nil
}

type CodeSymbolsAction struct {
	TargetPath string `json:"target_path"`
	Language   string `json:"language,omitempty"`
}

func (a CodeSymbolsAction) GetActionType() ActionType { return ActionCodeSymbols }
func (a CodeSymbolsAction) Target() string             { return a.TargetPath }
func (a CodeSymbolsAction) Validate() error {
	if a.TargetPath == "" {
		return fmt.Errorf("target_path is required")
	}
	return nil
}

// --- Browser Actions ---

type BrowserGotoAction struct {
	URL        string `json:"url"`
	WaitUntil string `json:"wait_until,omitempty"` // "load", "domcontentloaded", "networkidle"
}

func (a BrowserGotoAction) GetActionType() ActionType { return ActionBrowserGoto }
func (a BrowserGotoAction) Target() string             { return a.URL }
func (a BrowserGotoAction) Validate() error {
	if a.URL == "" {
		return fmt.Errorf("url is required")
	}
	return nil
}

type BrowserClickAction struct {
	Selector string `json:"selector"`
	Text     string `json:"text,omitempty"`
}

func (a BrowserClickAction) GetActionType() ActionType { return ActionBrowserClick }
func (a BrowserClickAction) Target() string             { return a.Selector }
func (a BrowserClickAction) Validate() error {
	if a.Selector == "" {
		return fmt.Errorf("selector is required")
	}
	return nil
}

type BrowserFillAction struct {
	Selector string `json:"selector"`
	Value    string `json:"value"`
}

func (a BrowserFillAction) GetActionType() ActionType { return ActionBrowserFill }
func (a BrowserFillAction) Target() string             { return a.Selector }
func (a BrowserFillAction) Validate() error {
	if a.Selector == "" {
		return fmt.Errorf("selector is required")
	}
	return nil
}

type BrowserEvalAction struct {
	Expression string `json:"expression"`
}

func (a BrowserEvalAction) GetActionType() ActionType { return ActionBrowserEval }
func (a BrowserEvalAction) Target() string             { return a.Expression }
func (a BrowserEvalAction) Validate() error {
	if a.Expression == "" {
		return fmt.Errorf("expression is required")
	}
	return nil
}

// --- Serialization Registry ---

// unmarshalRegistry maps ActionType to a factory for deserialization.
// Factories return pointers so json.Unmarshal can write into them;
// UnmarshalAction dereferences before returning so type switches
// match the value types (e.g. types.HTTPAction, not *types.HTTPAction).
var unmarshalRegistry = map[ActionType]func() TypedAction{
	ActionAPIRequest:   func() TypedAction { return &HTTPAction{} },
	ActionNavigate:     func() TypedAction { return &NavigateAction{} },
	ActionWait:         func() TypedAction { return &WaitAction{} },
	ActionProcessExec:  func() TypedAction { return &ProcessExecAction{} },
	ActionFileRead:     func() TypedAction { return &FileReadAction{} },
	ActionFileWrite:    func() TypedAction { return &FileWriteAction{} },
	ActionFileExists:   func() TypedAction { return &FileExistsAction{} },
	ActionFileGlob:     func() TypedAction { return &FileGlobAction{} },
	ActionMCPCall:      func() TypedAction { return &MCPCallAction{} },
	ActionCodeAnalyze:  func() TypedAction { return &CodeAnalyzeAction{} },
	ActionCodeLint:     func() TypedAction { return &CodeLintAction{} },
	ActionCodeSymbols:  func() TypedAction { return &CodeSymbolsAction{} },
	ActionBrowserGoto:  func() TypedAction { return &BrowserGotoAction{} },
	ActionBrowserClick: func() TypedAction { return &BrowserClickAction{} },
	ActionBrowserFill:  func() TypedAction { return &BrowserFillAction{} },
	ActionBrowserEval:  func() TypedAction { return &BrowserEvalAction{} },
}

// derefAction returns the value behind a pointer TypedAction.
// If the action is already a value type, it is returned as-is.
func derefAction(a TypedAction) TypedAction {
	switch v := a.(type) {
	case *HTTPAction:
		return *v
	case *NavigateAction:
		return *v
	case *WaitAction:
		return *v
	case *ProcessExecAction:
		return *v
	case *FileReadAction:
		return *v
	case *FileWriteAction:
		return *v
	case *FileExistsAction:
		return *v
	case *FileGlobAction:
		return *v
	case *MCPCallAction:
		return *v
	case *CodeAnalyzeAction:
		return *v
	case *CodeLintAction:
		return *v
	case *CodeSymbolsAction:
		return *v
	case *BrowserGotoAction:
		return *v
	case *BrowserClickAction:
		return *v
	case *BrowserFillAction:
		return *v
	case *BrowserEvalAction:
		return *v
	}
	return a
}

// UnmarshalAction deserializes an ActionEnvelope into a concrete TypedAction.
func UnmarshalAction(envelope ActionEnvelope) (TypedAction, error) {
	factory, ok := unmarshalRegistry[envelope.Type]
	if !ok {
		return nil, fmt.Errorf("unknown action type: %s", envelope.Type)
	}
	action := factory()
	if err := json.Unmarshal(envelope.Raw, action); err != nil {
		return nil, fmt.Errorf("unmarshal %s: %w", envelope.Type, err)
	}
	if err := action.Validate(); err != nil {
		return nil, err
	}
	return derefAction(action), nil
}

// MarshalAction serializes a TypedAction into an ActionEnvelope.
func MarshalAction(action TypedAction) (ActionEnvelope, error) {
	raw, err := json.Marshal(action)
	if err != nil {
		return ActionEnvelope{}, err
	}
	return ActionEnvelope{Type: action.GetActionType(), Raw: raw}, nil
}
