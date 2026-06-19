package types

// ToolDef represents an LLM tool/function definition.
type ToolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
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
			Description: "Click an element on the page",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"selector": map[string]any{"type": "string", "description": "CSS selector for the element"},
					"button":   map[string]any{"type": "string", "description": "Mouse button (left, right, middle)", "enum": []string{"left", "right", "middle"}},
				},
				"required": []string{"selector"},
			},
		},
		{
			Name:        "browser_fill",
			Description: "Fill a form field with text",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"selector": map[string]any{"type": "string", "description": "CSS selector for the field"},
					"value":    map[string]any{"type": "string", "description": "Text to fill"},
				},
				"required": []string{"selector", "value"},
			},
		},
		{
			Name:        "process_exec",
			Description: "Execute a system command",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"command":  map[string]any{"type": "string", "description": "Command to execute"},
					"args":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Command arguments"},
					"work_dir": map[string]any{"type": "string", "description": "Working directory"},
				},
				"required": []string{"command"},
			},
		},
		{
			Name:        "file_read",
			Description: "Read a file's contents",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{"type": "string", "description": "File path to read"},
				},
				"required": []string{"path"},
			},
		},
		{
			Name:        "file_write",
			Description: "Write content to a file",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":    map[string]any{"type": "string", "description": "File path to write"},
					"content": map[string]any{"type": "string", "description": "Content to write"},
				},
				"required": []string{"path", "content"},
			},
		},
		{
			Name:        "code_analyze",
			Description: "Analyze code structure and symbols",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"target_path": map[string]any{"type": "string", "description": "Directory or file to analyze"},
				},
				"required": []string{"target_path"},
			},
		},
		{
			Name:        "wait",
			Description: "Wait for a specified duration or wait for an element",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"duration": map[string]any{"type": "string", "description": "Duration to wait (e.g., '5s', '1m')"},
					"selector": map[string]any{"type": "string", "description": "CSS selector to wait for"},
				},
			},
		},
		{
			Name:        "db_query",
			Description: "Execute a database query",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"driver":            map[string]any{"type": "string", "description": "Database driver"},
					"connection_string": map[string]any{"type": "string", "description": "Database connection string"},
					"query":             map[string]any{"type": "string", "description": "SQL query to execute"},
				},
				"required": []string{"driver", "query"},
			},
		},
		{
			Name:        "mcp_call",
			Description: "Call an MCP (Model Context Protocol) server method",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"server": map[string]any{"type": "string", "description": "MCP server identifier"},
					"method": map[string]any{"type": "string", "description": "Method to call"},
				},
				"required": []string{"server", "method"},
			},
		},
	}
}
