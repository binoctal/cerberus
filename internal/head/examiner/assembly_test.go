package examiner

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/head/contract"
	"github.com/binoctal/cerberus/internal/llm"
)

// judge_result → JudgeResult covers all four emitted fields and leaves the
// code-path-only fields (SelfCritique/CritiqueTriggered) at zero.
func TestAssembleJudge(t *testing.T) {
	call := llm.ToolCall{
		Name: "judge_result",
		Input: map[string]any{
			"status":                 "fail",
			"existence_confidence":   float64(0.4),
			"correctness_confidence": float64(0.3),
			"reasoning":              "body mismatch",
		},
	}
	got, err := assembleJudge(call)
	require.NoError(t, err)
	assert.Equal(t, StatusFail, got.Status)
	assert.InDelta(t, 0.4, got.ExistenceConfidence, 1e-9)
	assert.InDelta(t, 0.3, got.CorrectnessConfidence, 1e-9)
	assert.Equal(t, "body mismatch", got.Reasoning)
	assert.Empty(t, got.SelfCritique, "SelfCritique is set by the critique code path, not the LLM")
	assert.False(t, got.CritiqueTriggered, "CritiqueTriggered is set by the critique code path, not the LLM")
}

// critique_verdict → CritiqueResult.
func TestAssembleCritique(t *testing.T) {
	call := llm.ToolCall{
		Name: "critique_verdict",
		Input: map[string]any{
			"issues_found":         true,
			"critique":             "missed 500 path",
			"suggested_status":     "uncertain",
			"suggested_confidence": float64(0.55),
		},
	}
	got, err := assembleCritique(call)
	require.NoError(t, err)
	assert.True(t, got.IssuesFound)
	assert.Equal(t, "missed 500 path", got.Critique)
	assert.Equal(t, StatusUncertain, got.SuggestedStatus)
	assert.InDelta(t, 0.55, got.SuggestedConfidence, 1e-9)
}

// assess_coverage carries a nested gaps array — assembler must walk it into
// []contract.Gap. coverage_pct is intentionally absent from the schema.
func TestAssembleAssessment_NestedGaps(t *testing.T) {
	call := llm.ToolCall{
		Name: "assess_coverage",
		Input: map[string]any{
			"reached": false,
			"gaps": []any{
				map[string]any{"kind": "scope", "detail": "no /admin"},
				map[string]any{"kind": "boundary", "detail": "no zero"},
			},
			"reasoning": "two gaps remain",
		},
	}
	got, err := assembleAssessment(call)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.False(t, got.Reached)
	assert.Equal(t, "two gaps remain", got.Reasoning)
	require.Len(t, got.Gaps, 2)
	assert.Equal(t, contract.Gap{Kind: "scope", Detail: "no /admin"}, got.Gaps[0])
	assert.Equal(t, contract.Gap{Kind: "boundary", Detail: "no zero"}, got.Gaps[1])
}

// gaps omitted entirely → empty slice, no error (LLM may emit reached=true).
func TestAssembleAssessment_NoGaps(t *testing.T) {
	call := llm.ToolCall{
		Name: "assess_coverage",
		Input: map[string]any{
			"reached":   true,
			"reasoning": "all covered",
		},
	}
	got, err := assembleAssessment(call)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.True(t, got.Reached)
	assert.Empty(t, got.Gaps)
}

// suggest_fix{skip:false} returns the reasoning + skip signal.
func TestAssembleAutofix(t *testing.T) {
	call := llm.ToolCall{
		Name: "suggest_fix",
		Input: map[string]any{
			"reasoning": "tighten timeout",
			"skip":      false,
		},
	}
	reasoning, skip, err := assembleAutofix(call)
	require.NoError(t, err)
	assert.Equal(t, "tighten timeout", reasoning)
	assert.False(t, skip)
}

// suggest_fix{skip:true} → skip=true, reasoning preserved.
func TestAssembleAutofix_Skip(t *testing.T) {
	call := llm.ToolCall{
		Name: "suggest_fix",
		Input: map[string]any{
			"reasoning": "flaky target",
			"skip":      true,
		},
	}
	reasoning, skip, err := assembleAutofix(call)
	require.NoError(t, err)
	assert.True(t, skip)
	assert.Equal(t, "flaky target", reasoning)
}

// 2× report_reflection → []Reflection{2}.
func TestAssembleReflections_Multiple(t *testing.T) {
	calls := []llm.ToolCall{
		{Name: "report_reflection", Input: map[string]any{
			"type":              "failure",
			"diagnosis":         "race on counter",
			"strategy":          "atomic.Int64",
			"condition_pattern": "concurrent increment",
			"category":          "concurrency",
		}},
		{Name: "report_reflection", Input: map[string]any{
			"type":              "success",
			"diagnosis":         "happy path stable",
			"strategy":          "none",
			"condition_pattern": "status 200",
			"category":          "api",
		}},
	}
	got := assembleReflections(calls)
	require.Len(t, got, 2)
	assert.Equal(t, "failure", got[0].Type)
	assert.Equal(t, "race on counter", got[0].Diagnosis)
	assert.Equal(t, "atomic.Int64", got[0].Strategy)
	assert.Equal(t, "concurrent increment", got[0].ConditionPattern)
	assert.Equal(t, "concurrency", got[0].Category)
	assert.Equal(t, "success", got[1].Type)
	assert.Equal(t, "api", got[1].Category)
}

// assembleReflections drops unknown tool names (multi-call assembler).
func TestAssembleReflections_SkipsUnknown(t *testing.T) {
	calls := []llm.ToolCall{
		{Name: "report_reflection", Input: map[string]any{"type": "success"}},
		{Name: "unknown_thing", Input: map[string]any{}},
		{Name: "report_reflection", Input: map[string]any{"type": "failure"}},
	}
	got := assembleReflections(calls)
	require.Len(t, got, 2, "unknown tool names are skipped, not errors")
}

// Empty input for the multi-call assembler → empty slice.
func TestAssembleReflections_Empty(t *testing.T) {
	got := assembleReflections(nil)
	assert.Empty(t, got)
}

// Single-call assemblers surface unknown tool names as errors so callers can
// route drift (zero calls / wrong tool) through their degrade policy.
func TestAssembleSingle_UnknownTool(t *testing.T) {
	bad := llm.ToolCall{Name: "nope", Input: map[string]any{}}

	if _, err := assembleJudge(bad); err == nil {
		t.Error("assembleJudge should error on unknown tool name")
	}
	if _, err := assembleCritique(bad); err == nil {
		t.Error("assembleCritique should error on unknown tool name")
	}
	if _, err := assembleAssessment(bad); err == nil {
		t.Error("assembleAssessment should error on unknown tool name")
	}
	if _, _, err := assembleAutofix(bad); err == nil {
		t.Error("assembleAutofix should error on unknown tool name")
	}
}

// Tool surface sanity checks: each *Tools() exposes exactly the expected tool
// names with object schemas, pinning the per-site contract.
func TestToolSurfaces(t *testing.T) {
	cases := []struct {
		name string
		fn   func() []llm.Tool
		want []string
	}{
		{"judge", judgeTools, []string{"judge_result"}},
		{"critic", criticTools, []string{"critique_verdict"}},
		{"assess", assessTools, []string{"assess_coverage"}},
		{"autofix", autofixTools, []string{"suggest_fix"}},
		{"learner", learnerTools, []string{"report_reflection"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tools := tc.fn()
			require.Len(t, tools, len(tc.want), "%s tool surface size", tc.name)
			for i, want := range tc.want {
				assert.Equal(t, want, tools[i].Name, "%s tool[%d] name", tc.name, i)
				// Object schema shape: must have type=object + properties.
				s, ok := tools[i].InputSchema["type"].(string)
				require.True(t, ok, "%s tool[%d] schema type missing", tc.name, i)
				assert.Equal(t, "object", s, "%s tool[%d] schema type", tc.name, i)
				_, ok = tools[i].InputSchema["properties"]
				assert.True(t, ok, "%s tool[%d] properties missing", tc.name, i)
			}
		})
	}
}

// assess_coverage schema must NOT expose coverage_pct (always overwritten by
// the objective measure) and must nest gaps as [{kind,detail}].
func TestAssessTools_SchemaShape(t *testing.T) {
	tools := assessTools()
	require.Len(t, tools, 1)
	props := tools[0].InputSchema["properties"].(map[string]any)
	_, hasPct := props["coverage_pct"]
	assert.False(t, hasPct, "coverage_pct must NOT be in the assess_coverage schema (overwritten by objective measure)")
	gaps, ok := props["gaps"].(map[string]any)
	require.True(t, ok, "gaps schema missing")
	assert.Equal(t, "array", gaps["type"])
	items, ok := gaps["items"].(map[string]any)
	require.True(t, ok, "gaps.items missing")
	assert.Equal(t, "object", items["type"])
	itemProps := items["properties"].(map[string]any)
	for _, f := range []string{"kind", "detail"} {
		fld, ok := itemProps[f].(map[string]any)
		require.True(t, ok, "gaps.items.%s missing", f)
		assert.Equal(t, "string", fld["type"], "gaps.items.%s.type", f)
	}
}

// judge_result schema must OMIT self_critique/critique_triggered (set by code).
func TestJudgeTools_SchemaShape(t *testing.T) {
	tools := judgeTools()
	require.Len(t, tools, 1)
	props := tools[0].InputSchema["properties"].(map[string]any)
	for _, f := range []string{"self_critique", "critique_triggered"} {
		_, present := props[f]
		assert.False(t, present, "%s must NOT be in judge_result schema (set by code path)", f)
	}
	// status enum must include all four JudgeStatus values.
	status, ok := props["status"].(map[string]any)
	require.True(t, ok)
	enum, _ := status["enum"].([]any)
	require.Len(t, enum, 4)
	got := map[string]bool{}
	for _, v := range enum {
		if s, ok := v.(string); ok {
			got[s] = true
		}
	}
	for _, want := range []string{"pass", "fail", "skip", "uncertain"} {
		assert.True(t, got[want], "status enum missing %q", want)
	}
}
