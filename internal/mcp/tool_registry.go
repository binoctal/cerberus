// internal/mcp/tool_registry.go
package mcp

// listTools returns the 5 tool definitions.
func (srv *Server) listTools() []toolDefinition {
	return []toolDefinition{
		{
			Name:        "cerberus_run",
			Description: "Start a Cerberus test session. Returns session_id immediately. After calling, periodically call cerberus_status to check progress. Stop when status is 'completed' or 'failed'.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"goal": map[string]any{"type": "string", "description": "Test goal, e.g. 'Test all API endpoints'"},
					"url":  map[string]any{"type": "string", "description": "Target base URL, e.g. 'http://localhost:3000'"},
				},
				"required": []string{"goal", "url"},
			},
		},
		{
			Name:        "cerberus_status",
			Description: "Poll the progress of a running test session.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"session_id": map[string]any{"type": "string", "description": "Session ID from cerberus_run"},
				},
				"required": []string{"session_id"},
			},
		},
		{
			Name:        "cerberus_report",
			Description: "Get the final test report for a completed session.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"session_id": map[string]any{"type": "string", "description": "Session ID"},
				},
				"required": []string{"session_id"},
			},
		},
		{
			Name:        "cerberus_decide",
			Description: "Provide a user decision for a pending escalation event.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"session_id": map[string]any{"type": "string", "description": "Session ID"},
					"action":     map[string]any{"type": "string", "description": "Decision: 'continue', 'abort', or 'skip_case'"},
					"payload":    map[string]any{"type": "string", "description": "Optional extra info, e.g. new URL for unreachable targets"},
				},
				"required": []string{"session_id", "action"},
			},
		},
		{
			Name:        "cerberus_cancel",
			Description: "Cancel a running test session.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"session_id": map[string]any{"type": "string", "description": "Session ID to cancel"},
				},
				"required": []string{"session_id"},
			},
		},
	}
}
