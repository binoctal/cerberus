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
				"aliases": strs(), "assert": map[string]any{"type": "object"}, "timeout": map[string]any{"type": "number"},
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
			InputSchema: objSchema([]any{"description", "cases"}, map[string]any{
				"description": map[string]any{"type": "string"},
				"cases":       strArrSchema(),
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
			InputSchema: objSchema([]any{"method", "path"}, map[string]any{
				"method":     map[string]any{"type": "string"},
				"path":       map[string]any{"type": "string"},
				"confidence": map[string]any{"type": "number"},
			})},
		{Name: "report_page", Description: "Report one discovered page/route.",
			InputSchema: objSchema([]any{"path"}, map[string]any{
				"path":       map[string]any{"type": "string"},
				"confidence": map[string]any{"type": "number"},
			})},
		{Name: "declare_tech", Description: "Declare detected tech stack (string array).",
			InputSchema: objSchema([]any{"stack"}, map[string]any{
				"stack": strArrSchema(),
			})},
	}
}

// contractTools returns the coverage-contract tool surface: one typed tool per
// Contract field. The set_priority schema forces map[string][]string, replacing
// the Priorities.UnmarshalJSON dual-shape drift patch.
func contractTools() []llm.Tool {
	return []llm.Tool{
		{Name: "declare_scope", Description: "Declare the modules/paths in scope.",
			InputSchema: objSchema([]any{"modules"}, map[string]any{"modules": strArrSchema()})},
		{Name: "declare_path_types", Description: "Declare path types to cover.",
			InputSchema: objSchema([]any{"types"}, map[string]any{"types": enumArrSchema("happy", "alternative", "boundary", "edge")})},
		{Name: "declare_error_scope", Description: "Declare error scopes to cover.",
			InputSchema: objSchema([]any{"scopes"}, map[string]any{"scopes": enumArrSchema("4xx", "validation", "exception")})},
		{Name: "declare_boundaries", Description: "Declare boundary classes to cover.",
			InputSchema: objSchema([]any{"boundaries"}, map[string]any{"boundaries": enumArrSchema("empty", "zero", "max", "invalid", "extreme")})},
		{Name: "set_priority", Description: "Map a priority bucket to its modules.",
			InputSchema: objSchema([]any{"bucket", "modules"}, map[string]any{
				"bucket":  map[string]any{"type": "string"},
				"modules": strArrSchema(),
			})},
		{Name: "set_coverage_gate", Description: "Set the objective coverage gate.",
			InputSchema: objSchema([]any{"module"}, map[string]any{
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
		InputSchema: objSchema([]any{"note"}, map[string]any{"note": map[string]any{"type": "string"}})},
	}
}

// objSchema wraps an object schema with required + properties.
func objSchema(required []any, props map[string]any) map[string]any {
	return map[string]any{"type": "object", "properties": props, "required": required}
}

// strArrSchema returns a schema for a free-form string array.
func strArrSchema() map[string]any {
	return map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
}

// enumArrSchema returns a schema for a string array whose items are constrained
// to one of the provided enum values.
func enumArrSchema(vals ...string) map[string]any {
	cs := make([]any, len(vals))
	for i, v := range vals {
		cs[i] = v
	}
	return map[string]any{"type": "array", "items": map[string]any{"type": "string", "enum": cs}}
}
