package examiner

import (
	"testing"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/llm"
)

// TestAssembleJudge_ParsesRedispatchHint: the judge_result tool's
// redispatch_hint enum maps to the agent.RedispatchHint constants; a missing or
// unrecognized value collapses to HintNone (never triggers replanning by
// accident).
func TestAssembleJudge_ParsesRedispatchHint(t *testing.T) {
	cases := map[string]agent.RedispatchHint{
		"endpoint_drift": agent.HintEndpointDrift,
		"auth":           agent.HintAuth,
		"shape":          agent.HintShape,
		"none":           agent.HintNone,
		"":               agent.HintNone,
		"bogus":          agent.HintNone,
		// "coverage" is a session-synthesized hint (D1 spec §5.1). The LLM never
		// emits it and the parser must NOT accept it — it round-trips via JSON, not
		// via parseRedispatchHint. Negative: accepting it here flips this to RED.
		"coverage": agent.HintNone,
	}
	for in, want := range cases {
		input := map[string]any{"status": "fail", "reasoning": "r"}
		if in != "" {
			input["redispatch_hint"] = in
		}
		jr, err := assembleJudge(llm.ToolCall{Name: "judge_result", Input: input})
		if err != nil {
			t.Fatalf("assembleJudge error for %q: %v", in, err)
		}
		if jr.RedispatchHint != want {
			t.Fatalf("input %q: want hint %q, got %q", in, want, jr.RedispatchHint)
		}
	}
}

// TestHintCoverage_EnumExists_ParserRejects: the HintCoverage enum value exists
// for session-synthesized verdicts (D1 spec §5.1), but parseRedispatchHint never
// returns it — "coverage" is NOT in the LLM-emitted set and collapses to HintNone.
func TestHintCoverage_EnumExists_ParserRejects(t *testing.T) {
	if agent.HintCoverage != "coverage" {
		t.Fatalf("HintCoverage constant = %q, want %q", agent.HintCoverage, "coverage")
	}
	if got := parseRedispatchHint("coverage"); got != agent.HintNone {
		t.Fatalf("parseRedispatchHint(\"coverage\") = %q, want HintNone (not LLM-emittable)", got)
	}
}
