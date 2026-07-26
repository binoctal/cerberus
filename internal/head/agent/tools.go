package agent

import "github.com/binoctal/cerberus/internal/llm"

// actionTools returns the action tool surface for the Agent head's
// DecideWithTools call sites. One tool per LLM-reachable ActionType; the
// schema of each mirrors the corresponding TypedAction struct fields in
// internal/types/actions_*.go.
//
// Used by steer (executor_steer.go). Recovery.Recover uses recoveryTools()
// instead, which appends a `skip` control tool to this surface.
//
// Excluded (rule-engine/phase-0 domain, never LLM-chosen in steer):
//   - ws_* (built from TestCase.Steps in execute_phases.go)
//   - code_* (built from tc.Language in rules_code.go)
//   - db_* / graphql_query (built in rules_file_other.go / database.go)
//   - process_build (constructed from TestStep.Action == "process_build")
//
// The `skip` control tool is intentionally NOT in this surface: steer must not
// be able to abandon a target. Recovery.Recover appends skip via
// recoveryTools() so the abandon decision is scoped to the recovery path only.
// Keep this list alphabetically ordered by family so additions are easy to
// audit against the spec.
func actionTools() []llm.Tool {
	return []llm.Tool{
		// --- HTTP ---
		{
			Name:        "api_request",
			Description: "Issue an HTTP request to the target service.",
			InputSchema: llm.ObjSchema([]any{"method", "url"}, map[string]any{
				"method":  map[string]any{"type": "string", "enum": []any{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"}},
				"url":     map[string]any{"type": "string"},
				"body":    map[string]any{"type": "string"},
				"headers": map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}},
				"timeout": map[string]any{"type": "number"},
			}),
		},

		// --- Navigate / Wait ---
		{
			Name:        "navigate",
			Description: "Navigate the browser to a URL.",
			InputSchema: llm.ObjSchema([]any{"url"}, map[string]any{
				"url":           map[string]any{"type": "string"},
				"wait_selector": map[string]any{"type": "string"},
				"wait_for":      map[string]any{"type": "number"},
			}),
		},
		{
			Name:        "wait",
			Description: "Wait before proceeding (duration, selector, or state).",
			InputSchema: llm.ObjSchema(nil, map[string]any{
				"duration":       map[string]any{"type": "string"},
				"selector":       map[string]any{"type": "string"},
				"wait_for_state": map[string]any{"type": "string"},
			}),
		},

		// --- Process ---
		{
			Name:        "process_exec",
			Description: "Execute a system command.",
			InputSchema: llm.ObjSchema([]any{"command"}, map[string]any{
				"command":  map[string]any{"type": "string"},
				"args":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"work_dir": map[string]any{"type": "string"},
				"env":      map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}},
				"timeout":  map[string]any{"type": "string"},
			}),
		},

		// --- File ---
		{
			Name:        "file_read",
			Description: "Read bytes from a file.",
			InputSchema: llm.ObjSchema([]any{"path"}, map[string]any{
				"path":   map[string]any{"type": "string"},
				"offset": map[string]any{"type": "number"},
				"limit":  map[string]any{"type": "number"},
			}),
		},
		{
			Name:        "file_write",
			Description: "Write content to a file.",
			InputSchema: llm.ObjSchema([]any{"path", "content"}, map[string]any{
				"path":               map[string]any{"type": "string"},
				"content":            map[string]any{"type": "string"},
				"create_parent_dirs": map[string]any{"type": "boolean"},
				"mode":               map[string]any{"type": "number"},
			}),
		},
		{
			Name:        "file_exists",
			Description: "Check whether a file exists.",
			InputSchema: llm.ObjSchema([]any{"path"}, map[string]any{
				"path": map[string]any{"type": "string"},
			}),
		},
		{
			Name:        "file_glob",
			Description: "Find files matching a glob pattern.",
			InputSchema: llm.ObjSchema([]any{"pattern"}, map[string]any{
				"pattern": map[string]any{"type": "string"},
				"path":    map[string]any{"type": "string"},
			}),
		},

		// --- Browser ---
		{
			Name:        "browser_goto",
			Description: "Navigate the browser to a URL (Playwright-style).",
			InputSchema: llm.ObjSchema([]any{"url"}, map[string]any{
				"url":        map[string]any{"type": "string"},
				"wait_until": map[string]any{"type": "string", "enum": []any{"load", "domcontentloaded", "networkidle0", "networkidle2"}},
			}),
		},
		{
			Name:        "browser_click",
			Description: "Click an element matching a CSS selector.",
			InputSchema: llm.ObjSchema([]any{"selector"}, map[string]any{
				"selector":  map[string]any{"type": "string"},
				"text":      map[string]any{"type": "string"},
				"button":    map[string]any{"type": "string", "enum": []any{"left", "right", "middle"}},
				"modifiers": map[string]any{"type": "array", "items": map[string]any{"type": "string", "enum": []any{"Alt", "Control", "ControlOrMeta", "Meta", "Shift"}}},
			}),
		},
		{
			Name:        "browser_fill",
			Description: "Fill a form field with a value.",
			InputSchema: llm.ObjSchema([]any{"selector", "value"}, map[string]any{
				"selector": map[string]any{"type": "string"},
				"value":    map[string]any{"type": "string"},
			}),
		},
		{
			Name:        "browser_eval",
			Description: "Evaluate a JavaScript expression in the browser.",
			InputSchema: llm.ObjSchema([]any{"expression"}, map[string]any{
				"expression": map[string]any{"type": "string"},
				"args":       map[string]any{"type": "array"},
			}),
		},

		// --- MCP ---
		{
			Name:        "mcp_call",
			Description: "Call a method on an MCP server.",
			InputSchema: llm.ObjSchema([]any{"server", "method"}, map[string]any{
				"server": map[string]any{"type": "string"},
				"method": map[string]any{"type": "string"},
				"params": map[string]any{"type": "object"},
			}),
		},
	}
}

// objSchema + strArrSchema + enumArrSchema live in internal/llm/schema.go,
// shared by Scout, Agent, and Examiner.

// recoveryTools returns the action tool surface plus a dedicated `skip`
// control tool used only by Recovery.Recover. Steer must not be able to skip a
// target (only the recovery path can decide a target is unrecoverable), so
// `skip` lives here rather than in actionTools(). assembleAction also rejects
// `skip` — Recover inspects call.Name == "skip" directly and never delegates
// the skip signal to assembleAction.
//
// The `skip` tool carries an empty schema (no required props, no fields): the
// decision is binary and needs no payload.
func recoveryTools() []llm.Tool {
	return append(actionTools(), llm.Tool{
		Name:        "skip",
		Description: "Abandon this target — it is unrecoverable.",
		InputSchema: llm.ObjSchema(nil, map[string]any{}),
	})
}
