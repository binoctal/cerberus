package examiner

import (
	"fmt"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/head/contract"
	"github.com/binoctal/cerberus/internal/llm"
)

// Assembly maps Examiner tool calls to the output structs that today's
// Decide+JSON path populates directly. Each single-call assembler returns an
// error on unknown tool names so callers (judge/critic/assess/autofix sites)
// can route drift through their own degrade policy; the multi-call assembler
// (assembleReflections) skips unknown names since mixing valid + unknown calls
// is normal multi-call traffic.
//
// Field coercion uses the shared helpers in internal/llm/toolfield.go
// (StrField/NumField/BoolField) — each returns its zero value when the key is
// absent, matching JSON-unmarshal semantics.

// assembleJudge maps a judge_result call to a JudgeResult. SelfCritique and
// CritiqueTriggered are NOT extracted: they are set later by the critique code
// path (judge.go), not the initial LLM call.
func assembleJudge(call llm.ToolCall) (JudgeResult, error) {
	if call.Name != "judge_result" {
		return JudgeResult{}, fmt.Errorf("assembleJudge: unexpected tool %q", call.Name)
	}
	return JudgeResult{
		Status:                JudgeStatus(llm.StrField(call, "status")),
		ExistenceConfidence:   llm.NumField(call, "existence_confidence"),
		CorrectnessConfidence: llm.NumField(call, "correctness_confidence"),
		Reasoning:             llm.StrField(call, "reasoning"),
		RedispatchHint:        parseRedispatchHint(llm.StrField(call, "redispatch_hint")),
	}, nil
}

// parseRedispatchHint maps the LLM's redispatch_hint string to the enum; any
// missing or unrecognized value becomes HintNone so a malformed/omitted hint
// never accidentally triggers replanning.
func parseRedispatchHint(s string) agent.RedispatchHint {
	switch agent.RedispatchHint(s) {
	case agent.HintEndpointDrift, agent.HintAuth, agent.HintShape,
		agent.HintHandshake, agent.HintWsShape, agent.HintWsMatch:
		return agent.RedispatchHint(s)
	default:
		return agent.HintNone
	}
}

// assembleCritique maps a critique_verdict call to a CritiqueResult.
func assembleCritique(call llm.ToolCall) (CritiqueResult, error) {
	if call.Name != "critique_verdict" {
		return CritiqueResult{}, fmt.Errorf("assembleCritique: unexpected tool %q", call.Name)
	}
	return CritiqueResult{
		IssuesFound:         llm.BoolField(call, "issues_found"),
		Critique:            llm.StrField(call, "critique"),
		SuggestedStatus:     JudgeStatus(llm.StrField(call, "suggested_status")),
		SuggestedConfidence: llm.NumField(call, "suggested_confidence"),
	}, nil
}

// assembleAssessment maps an assess_coverage call to a contract.Assessment.
// The nested gaps array is walked manually since StrSliceField cannot describe
// []struct{Kind,Detail}. coverage_pct is intentionally NOT extracted — it is
// always overwritten by the objective measure in assess.go.
func assembleAssessment(call llm.ToolCall) (*contract.Assessment, error) {
	if call.Name != "assess_coverage" {
		return nil, fmt.Errorf("assembleAssessment: unexpected tool %q", call.Name)
	}
	a := &contract.Assessment{
		Reached:   llm.BoolField(call, "reached"),
		Reasoning: llm.StrField(call, "reasoning"),
	}
	if rawArr, ok := call.Input["gaps"].([]any); ok {
		a.Gaps = make([]contract.Gap, 0, len(rawArr))
		for _, raw := range rawArr {
			m, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			a.Gaps = append(a.Gaps, contract.Gap{
				Kind:   stringFromMap(m, "kind"),
				Detail: stringFromMap(m, "detail"),
			})
		}
	}
	return a, nil
}

// assembleAutofix maps a suggest_fix call to the (reasoning, skip) pair used by
// AutoFixer.Fix. The inline anonymous struct in autofix.go is the only call
// site.
func assembleAutofix(call llm.ToolCall) (reasoning string, skip bool, err error) {
	if call.Name != "suggest_fix" {
		return "", false, fmt.Errorf("assembleAutofix: unexpected tool %q", call.Name)
	}
	return llm.StrField(call, "reasoning"), llm.BoolField(call, "skip"), nil
}

// assembleReflections maps N report_reflection calls to []Reflection. Unknown
// tool names are skipped (multi-call traffic may include unrelated tool calls).
func assembleReflections(calls []llm.ToolCall) []Reflection {
	out := make([]Reflection, 0, len(calls))
	for _, c := range calls {
		if c.Name != "report_reflection" {
			continue
		}
		out = append(out, Reflection{
			Type:             llm.StrField(c, "type"),
			Diagnosis:        llm.StrField(c, "diagnosis"),
			Strategy:         llm.StrField(c, "strategy"),
			ConditionPattern: llm.StrField(c, "condition_pattern"),
			Category:         llm.StrField(c, "category"),
		})
	}
	return out
}

// stringFromMap returns the string at k, or "" if missing/not a string. Used
// for nested gap objects where llm.StrField (which takes a ToolCall) does not
// apply.
func stringFromMap(m map[string]any, k string) string {
	if v, ok := m[k].(string); ok {
		return v
	}
	return ""
}
