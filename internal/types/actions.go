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

	// Database
	ActionDBQuery  ActionType = "db_query"
	ActionDBAssert ActionType = "db_assert"

	// GraphQL
	ActionGraphQLQuery ActionType = "graphql_query"

	// WebSocket
	ActionWSConnect ActionType = "ws_connect"
	ActionWSSend    ActionType = "ws_send"
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
func (a HTTPAction) Target() string            { return a.URL }
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
func (a NavigateAction) Target() string            { return a.URL }
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
func (a WaitAction) Target() string            { return "" }
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
func (a BuildAction) Unwrap() ProcessExecAction { return a.ProcessExecAction }

func (a ProcessExecAction) GetActionType() ActionType { return ActionProcessExec }
func (a ProcessExecAction) Target() string            { return a.Command }
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
func (a FileReadAction) Target() string            { return a.Path }
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
func (a FileWriteAction) Target() string            { return a.Path }
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
func (a FileExistsAction) Target() string            { return a.Path }
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
func (a FileGlobAction) Target() string            { return a.Pattern }
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
func (a MCPCallAction) Target() string            { return a.Server + "/" + a.Method }
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
func (a CodeAnalyzeAction) Target() string            { return a.TargetPath }
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
func (a CodeLintAction) Target() string            { return a.TargetPath }
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
func (a CodeSymbolsAction) Target() string            { return a.TargetPath }
func (a CodeSymbolsAction) Validate() error {
	if a.TargetPath == "" {
		return fmt.Errorf("target_path is required")
	}
	return nil
}

// --- Browser Actions ---

type BrowserGotoAction struct {
	URL       string `json:"url"`
	WaitUntil string `json:"wait_until,omitempty"` // "load", "domcontentloaded", "networkidle"
}

func (a BrowserGotoAction) GetActionType() ActionType { return ActionBrowserGoto }
func (a BrowserGotoAction) Target() string            { return a.URL }
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
func (a BrowserClickAction) Target() string            { return a.Selector }
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
func (a BrowserFillAction) Target() string            { return a.Selector }
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
func (a BrowserEvalAction) Target() string            { return a.Expression }
func (a BrowserEvalAction) Validate() error {
	if a.Expression == "" {
		return fmt.Errorf("expression is required")
	}
	return nil
}

// --- Database Actions ---

type DBQueryAction struct {
	Driver string `json:"driver"` // "sqlite", "postgres", "mysql"
	DSN    string `json:"dsn"`    // connection string
	Query  string `json:"query"`
	Args   []any  `json:"args,omitempty"`
}

func (a DBQueryAction) GetActionType() ActionType { return ActionDBQuery }
func (a DBQueryAction) Target() string            { return a.Query }
func (a DBQueryAction) Validate() error {
	if a.Query == "" {
		return fmt.Errorf("query is required")
	}
	if a.Driver == "" {
		return fmt.Errorf("driver is required")
	}
	return nil
}

type DBAssertAction struct {
	Driver    string `json:"driver"`
	DSN       string `json:"dsn"`
	Query     string `json:"query"`
	Assertion string `json:"assertion"` // e.g. "count == 0", "rows.length > 0"
}

func (a DBAssertAction) GetActionType() ActionType { return ActionDBAssert }
func (a DBAssertAction) Target() string            { return a.Query }
func (a DBAssertAction) Validate() error {
	if a.Query == "" {
		return fmt.Errorf("query is required")
	}
	if a.Assertion == "" {
		return fmt.Errorf("assertion is required")
	}
	if a.Driver == "" {
		return fmt.Errorf("driver is required")
	}
	return nil
}

// --- GraphQL Actions ---

type GraphQLQueryAction struct {
	URL           string            `json:"url"`
	Query         string            `json:"query"`
	Variables     map[string]any    `json:"variables,omitempty"`
	Headers       map[string]string `json:"headers,omitempty"`
	OperationName string            `json:"operation_name,omitempty"`
}

func (a GraphQLQueryAction) GetActionType() ActionType { return ActionGraphQLQuery }
func (a GraphQLQueryAction) Target() string            { return a.URL }
func (a GraphQLQueryAction) Validate() error {
	if a.URL == "" {
		return fmt.Errorf("url is required")
	}
	if a.Query == "" {
		return fmt.Errorf("query is required")
	}
	return nil
}

// --- WebSocket Actions ---

type WSConnectAction struct {
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
}

func (a WSConnectAction) GetActionType() ActionType { return ActionWSConnect }
func (a WSConnectAction) Target() string            { return a.URL }
func (a WSConnectAction) Validate() error {
	if a.URL == "" {
		return fmt.Errorf("url is required")
	}
	return nil
}

type WSSendAction struct {
	URL     string `json:"url"`
	Message string `json:"message"`
}

func (a WSSendAction) GetActionType() ActionType { return ActionWSSend }
func (a WSSendAction) Target() string            { return a.URL }
func (a WSSendAction) Validate() error {
	if a.URL == "" {
		return fmt.Errorf("url is required")
	}
	if a.Message == "" {
		return fmt.Errorf("message is required")
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
	ActionDBQuery:      func() TypedAction { return &DBQueryAction{} },
	ActionDBAssert:     func() TypedAction { return &DBAssertAction{} },
	ActionGraphQLQuery: func() TypedAction { return &GraphQLQueryAction{} },
	ActionWSConnect:    func() TypedAction { return &WSConnectAction{} },
	ActionWSSend:       func() TypedAction { return &WSSendAction{} },
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
	case *DBQueryAction:
		return *v
	case *DBAssertAction:
		return *v
	case *GraphQLQueryAction:
		return *v
	case *WSConnectAction:
		return *v
	case *WSSendAction:
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

// ToolDefinitions generates LLM tool schemas from the registered action types.
// Returns a slice of Tool definitions suitable for passing to LLM providers.
func ToolDefinitions() []ToolDef {
	return []ToolDef{
		{
			Name:        "api_request",
			Description: "Send an HTTP request to an API endpoint",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"method":  map[string]any{"type": "string", "description": "HTTP method (GET, POST, PUT, DELETE, PATCH)"},
					"url":     map[string]any{"type": "string", "description": "Request URL or path"},
					"headers": map[string]any{"type": "object", "description": "Request headers"},
					"body":    map[string]any{"type": "string", "description": "Request body (JSON string)"},
				},
				"required": []string{"method", "url"},
			},
		},
		{
			Name:        "browser_goto",
			Description: "Navigate the browser to a URL",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"url": map[string]any{"type": "string", "description": "URL to navigate to"},
				},
				"required": []string{"url"},
			},
		},
		{
			Name:        "browser_click",
			Description: "Click an element in the browser",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"selector": map[string]any{"type": "string", "description": "CSS selector for the element"},
				},
				"required": []string{"selector"},
			},
		},
		{
			Name:        "browser_fill",
			Description: "Fill a form field in the browser",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"selector": map[string]any{"type": "string", "description": "CSS selector for the input"},
					"value":    map[string]any{"type": "string", "description": "Value to fill in"},
				},
				"required": []string{"selector", "value"},
			},
		},
		{
			Name:        "browser_eval",
			Description: "Evaluate JavaScript in the browser",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"script": map[string]any{"type": "string", "description": "JavaScript code to evaluate"},
				},
				"required": []string{"script"},
			},
		},
		{
			Name:        "file_read",
			Description: "Read a file from the project",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{"type": "string", "description": "File path relative to project root"},
				},
				"required": []string{"path"},
			},
		},
		{
			Name:        "file_write",
			Description: "Write content to a file in the project",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":    map[string]any{"type": "string", "description": "File path relative to project root"},
					"content": map[string]any{"type": "string", "description": "File content to write"},
				},
				"required": []string{"path", "content"},
			},
		},
		{
			Name:        "file_exists",
			Description: "Check if a file exists in the project",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{"type": "string", "description": "File path to check"},
				},
				"required": []string{"path"},
			},
		},
		{
			Name:        "file_glob",
			Description: "Find files matching a glob pattern",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"pattern": map[string]any{"type": "string", "description": "Glob pattern (e.g. **/*.go)"},
				},
				"required": []string{"pattern"},
			},
		},
		{
			Name:        "process_exec",
			Description: "Execute a shell command",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"command": map[string]any{"type": "string", "description": "Shell command to execute"},
					"timeout": map[string]any{"type": "integer", "description": "Timeout in seconds"},
				},
				"required": []string{"command"},
			},
		},
		{
			Name:        "db_query",
			Description: "Execute a database query",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{"type": "string", "description": "SQL query to execute"},
				},
				"required": []string{"query"},
			},
		},
		{
			Name:        "code_analyze",
			Description: "Analyze code for issues and patterns",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":  map[string]any{"type": "string", "description": "File or directory to analyze"},
					"focus": map[string]any{"type": "string", "description": "Analysis focus: security, performance, quality"},
				},
				"required": []string{"path"},
			},
		},
		{
			Name:        "wait",
			Description: "Wait for a condition or duration",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"duration_ms": map[string]any{"type": "integer", "description": "Wait duration in milliseconds"},
					"condition":   map[string]any{"type": "string", "description": "Condition to wait for (URL contains, status code)"},
				},
			},
		},
		{
			Name:        "mcp_call",
			Description: "Call an MCP server tool",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"tool":   map[string]any{"type": "string", "description": "MCP tool name"},
					"params": map[string]any{"type": "object", "description": "Tool parameters"},
				},
				"required": []string{"tool"},
			},
		},
	}
}

// ToolDef is a provider-agnostic tool definition (mirrors llm.Tool to avoid import cycle).
type ToolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

// ToLLMTools converts ToolDefs to llm.Tool slice.
func ToLLMTools(defs []ToolDef) []ToolDef {
	return defs
}
