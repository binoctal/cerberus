package examiner

import "github.com/binoctal/cerberus/internal/llm"

// Tool surfaces for the five Examiner DecideWithTools sites. Each site exposes
// one structured-output tool whose schema mirrors the JSON tags of the
// corresponding output struct (JudgeResult / CritiqueResult / Reflection in
// types.go; contract.Assessment + contract.Gap in contract/types.go). The
// provider enforces the schema, replacing the legacy JSON Output(...) prompts.
//
// Field omissions are load-bearing:
//   - judge_result omits self_critique/critique_triggered (set by the evaluate
//     code path in judge.go, NOT the initial LLM call).
//   - assess_coverage omits coverage_pct (always overwritten by the objective
//     measure m.Pct in assess.go).

// judgeTools returns the Judge verdict tool: one call per StepResult.
func judgeTools() []llm.Tool {
	return []llm.Tool{{
		Name:        "judge_result",
		Description: "Emit the verdict for one test step result.",
		InputSchema: llm.ObjSchema([]any{"status", "existence_confidence", "correctness_confidence", "reasoning"}, map[string]any{
			"status":                 map[string]any{"type": "string", "enum": []any{"pass", "fail", "skip", "uncertain"}},
			"existence_confidence":   map[string]any{"type": "number"},
			"correctness_confidence": map[string]any{"type": "number"},
			"reasoning":              map[string]any{"type": "string"},
			"redispatch_hint": map[string]any{
				"type":        "string",
				"enum":        []any{"none", "endpoint_drift", "auth", "shape"},
				"description": "For a fail: the correctable root cause a replacement case could address. 'none' unless the failure is clearly correctable.",
			},
		}),
	}}
}

// criticTools returns the Self-Refine critique tool.
func criticTools() []llm.Tool {
	return []llm.Tool{{
		Name:        "critique_verdict",
		Description: "Emit the critique of an initial verdict.",
		InputSchema: llm.ObjSchema([]any{"issues_found", "critique", "suggested_status", "suggested_confidence"}, map[string]any{
			"issues_found":         map[string]any{"type": "boolean"},
			"critique":             map[string]any{"type": "string"},
			"suggested_status":     map[string]any{"type": "string", "enum": []any{"pass", "fail", "skip", "uncertain"}},
			"suggested_confidence": map[string]any{"type": "number"},
		}),
	}}
}

// assessTools returns the coverage-contract assessment tool: a single object
// with a nested gaps array (NOT split into per-gap tools — Assessment is one
// object with a gap list).
func assessTools() []llm.Tool {
	return []llm.Tool{{
		Name:        "assess_coverage",
		Description: "Assess whether the test session met its coverage contract.",
		InputSchema: llm.ObjSchema([]any{"reached", "gaps", "reasoning"}, map[string]any{
			"reached": map[string]any{"type": "boolean"},
			"gaps": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"kind":   map[string]any{"type": "string"},
						"detail": map[string]any{"type": "string"},
					},
				},
			},
			"reasoning": map[string]any{"type": "string"},
		}),
	}}
}

// autofixTools returns the auto-fix suggestion tool. `skip` is a FIELD here
// (single object), not a separate tool — autofix has no competing action surface.
func autofixTools() []llm.Tool {
	return []llm.Tool{{
		Name:        "suggest_fix",
		Description: "Suggest a corrective action or skip for a failing test case.",
		InputSchema: llm.ObjSchema([]any{"reasoning", "skip"}, map[string]any{
			"reasoning": map[string]any{"type": "string"},
			"skip":      map[string]any{"type": "boolean"},
		}),
	}}
}

// learnerTools returns the Reflexion reflection tool: one call per reflection.
func learnerTools() []llm.Tool {
	return []llm.Tool{{
		Name:        "report_reflection",
		Description: "Emit one reflection on a test result (failure or success).",
		InputSchema: llm.ObjSchema([]any{"type", "diagnosis", "strategy", "condition_pattern", "category"}, map[string]any{
			"type":              map[string]any{"type": "string", "enum": []any{"failure", "success"}},
			"diagnosis":         map[string]any{"type": "string"},
			"strategy":          map[string]any{"type": "string"},
			"condition_pattern": map[string]any{"type": "string"},
			"category":          map[string]any{"type": "string"},
		}),
	}}
}
