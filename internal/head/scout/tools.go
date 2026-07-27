package scout

import "github.com/binoctal/cerberus/internal/llm"

// planTools returns the two-tier tool surface for directPlan: high-level
// intent tools (one call = one TestCase) and low-level ws_* step tools gated by
// begin_case (multi-step choreography). Schemas are hard-enforced by the
// provider, replacing the legacy PlanOutput JSON.
func planTools() []llm.Tool {
	strs := func(items ...string) map[string]any {
		e := map[string]any{"type": "string"}
		if len(items) > 0 {
			cs := make([]any, len(items))
			for i, s := range items {
				cs[i] = s
			}
			e["enum"] = cs
		}
		return map[string]any{"type": "array", "items": e}
	}
	return []llm.Tool{
		{Name: "test_http_endpoint", Description: "Emit one HTTP test case.",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{
				"method": map[string]any{"type": "string", "enum": []any{"GET", "POST", "PUT", "PATCH", "DELETE"}},
				"path":   map[string]any{"type": "string"}, "body": map[string]any{"type": "string"},
				"service": map[string]any{"type": "string"}, "expect_status": map[string]any{"type": "number"},
				"expect_body": map[string]any{"type": "string"},
			}, "required": []any{"method", "path"}}},
		{Name: "check_invariant", Description: "Emit one invariant assertion case (executed via Steer).",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{
				"invariant_id": map[string]any{"type": "string"}, "description": map[string]any{"type": "string"},
				"assertion": map[string]any{"type": "string"}, "severity": map[string]any{"type": "string"},
			}}},
		{Name: "run_process", Description: "Emit one process test case. test/lint go through exec+cmd.",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{
				"action": map[string]any{"type": "string", "enum": []any{"build", "exec"}},
				"cmd":    map[string]any{"type": "string"}, "expect": map[string]any{"type": "string"},
			}, "required": []any{"action"}}},
		{Name: "analyze_code", Description: "Emit one static-analysis case.",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{
				"action": map[string]any{"type": "string", "enum": []any{"analyze", "lint", "symbols"}},
				"target": map[string]any{"type": "string"},
			}, "required": []any{"action"}}},
		{Name: "check_file", Description: "Emit one file-system case.",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{
				"action": map[string]any{"type": "string", "enum": []any{"exists", "read", "glob"}},
				"path":   map[string]any{"type": "string"}, "pattern": map[string]any{"type": "string"},
				"expect": map[string]any{"type": "string"},
			}, "required": []any{"action"}}},
		{Name: "navigate", Description: "Emit one browser navigation case.",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{
				"path": map[string]any{"type": "string"}, "expect": map[string]any{"type": "string"},
			}, "required": []any{"path"}}},
		{Name: "begin_case", Description: "Open a multi-step WS choreography case. Following ws_* calls belong to it until the next begin_case or high-level tool.",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{
				"name": map[string]any{"type": "string"}, "expectation": map[string]any{"type": "string"},
				"service": map[string]any{"type": "string"},
			}, "required": []any{"name", "expectation"}}},
		{Name: "ws_connect", Description: "WS step: open a connection as role.",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{
				"role": map[string]any{"type": "string"}, "url": map[string]any{"type": "string"},
			}, "required": []any{"role"}}},
		{Name: "ws_send", Description: "WS step: send a typed message on role's connection.",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{
				"role": map[string]any{"type": "string"}, "type": map[string]any{"type": "string"},
			}, "required": []any{"role", "type"}}},
		{Name: "ws_receive", Description: "WS step: await a typed message on role's connection.",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{
				"role": map[string]any{"type": "string"}, "type": map[string]any{"type": "string"},
				"aliases": strs(),
				"assert": map[string]any{
					"type":                 "object",
					"description":          "Optional path->value content checks on the MATCHED message, ONLY when the `type` routing-key match does not by itself prove the expectation. Keys are dotted JSON paths into the matched message (e.g. \"payload.approved\", \"type\"); values are the expected scalars (bool/string/number/null). Every entry must hold or the receive FAILS. OMIT this field entirely for arrival-only checks (the common case: if matching `type` is all you need, do not add assert). Example: {\"payload.approved\": true}. This is a flat path->value map, NOT an expression: never use field/op/value keys or operators.",
					"additionalProperties": true,
				},
				"timeout": map[string]any{"type": "number"},
			}, "required": []any{"role"}}},
		{Name: "ws_disconnect", Description: "WS step: close role's connection.",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{
				"role": map[string]any{"type": "string"},
			}, "required": []any{"role"}}},
	}
}

// proposeTools returns the Propose tool surface: propose_strategy surfaces one
// diverse test strategy each. Multiple calls = multiple candidates, preserving
// the legacy "strategies array" behavior. The provider schema enforces
// description+cases, replacing the legacy ProposeOutput JSON.
func proposeTools() []llm.Tool {
	return []llm.Tool{
		{Name: "propose_strategy", Description: "Propose one diverse test strategy.",
			InputSchema: llm.ObjSchema([]any{"description", "cases"}, map[string]any{
				"description": map[string]any{"type": "string"},
				"cases":       llm.StrArrSchema(),
			})},
	}
}

// analyzeTools returns the Analyze tool surface: report_endpoint/report_page
// surface one discovered API/page each, declare_tech declares the tech stack as
// a schema-enforced string array. Schemas are hard-enforced by the provider,
// replacing the legacy AnalyzeOutput JSON and the flexibleStrings drift patch.
func analyzeTools() []llm.Tool {
	return []llm.Tool{
		{Name: "report_endpoint", Description: "Report one discovered API endpoint.",
			InputSchema: llm.ObjSchema([]any{"method", "path"}, map[string]any{
				"method":     map[string]any{"type": "string"},
				"path":       map[string]any{"type": "string"},
				"confidence": map[string]any{"type": "number"},
			})},
		{Name: "report_page", Description: "Report one discovered page/route.",
			InputSchema: llm.ObjSchema([]any{"path"}, map[string]any{
				"path":       map[string]any{"type": "string"},
				"confidence": map[string]any{"type": "number"},
			})},
		{Name: "declare_tech", Description: "Declare detected tech stack (string array).",
			InputSchema: llm.ObjSchema([]any{"stack"}, map[string]any{
				"stack": llm.StrArrSchema(),
			})},
	}
}

// contractTools returns the coverage-contract tool surface: one typed tool per
// Contract field. The set_priority schema forces map[string][]string, replacing
// the Priorities.UnmarshalJSON dual-shape drift patch.
func contractTools() []llm.Tool {
	return []llm.Tool{
		{Name: "declare_scope", Description: "Declare the modules/paths in scope.",
			InputSchema: llm.ObjSchema([]any{"modules"}, map[string]any{"modules": llm.StrArrSchema()})},
		{Name: "declare_path_types", Description: "Declare path types to cover.",
			InputSchema: llm.ObjSchema([]any{"types"}, map[string]any{"types": llm.EnumArrSchema("happy", "alternative", "boundary", "edge")})},
		{Name: "declare_error_scope", Description: "Declare error scopes to cover.",
			InputSchema: llm.ObjSchema([]any{"scopes"}, map[string]any{"scopes": llm.EnumArrSchema("4xx", "validation", "exception")})},
		{Name: "declare_boundaries", Description: "Declare boundary classes to cover.",
			InputSchema: llm.ObjSchema([]any{"boundaries"}, map[string]any{"boundaries": llm.EnumArrSchema("empty", "zero", "max", "invalid", "extreme")})},
		{Name: "set_priority", Description: "Map a priority bucket to its modules.",
			InputSchema: llm.ObjSchema([]any{"bucket", "modules"}, map[string]any{
				"bucket":  map[string]any{"type": "string"},
				"modules": llm.StrArrSchema(),
			})},
		{Name: "set_coverage_gate", Description: "Set the objective coverage gate.",
			InputSchema: llm.ObjSchema([]any{"module"}, map[string]any{
				"module":           map[string]any{"type": "string"},
				"line_threshold":   map[string]any{"type": "number"},
				"branch_threshold": map[string]any{"type": "number"},
			})},
	}
}

// selfAssessTools returns the SelfAssess tool surface: report_contract_gap
// surfaces one gap note each. Schemas are hard-enforced by the provider,
// replacing the legacy notes JSON.
func selfAssessTools() []llm.Tool {
	return []llm.Tool{{Name: "report_contract_gap", Description: "Report one coverage gap.",
		InputSchema: llm.ObjSchema([]any{"note"}, map[string]any{"note": map[string]any{"type": "string"}})},
	}
}
