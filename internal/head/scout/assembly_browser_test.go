package scout

import (
	"testing"

	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
)

// Scout free-leg browser surface (spec §3.2 follow-up): begin_case followed
// by browser_* step calls must assemble a browser_flow case targeting the
// service's declared UI base URL — the LLM names the service, not the URL.
func TestAssemblePlanBrowserFlow(t *testing.T) {
	services := []project.Service{
		{Name: "open-agents", URL: "http://localhost:8989/ws/{userId}",
			Vocabulary: &project.Vocabulary{UI: &project.VocabUI{
				BaseURL: "http://localhost:5183", Locale: "en"}}},
	}
	calls := []llm.ToolCall{
		{Name: "begin_case", Input: map[string]any{
			"name": "missions shows connected state", "expectation": "sidebar shows Connected", "service": "open-agents"}},
		{Name: "browser_goto", Input: map[string]any{"url": "/dashboard/missions"}},
		{Name: "browser_expect", Input: map[string]any{
			"target": "text=Connected", "expectation": "text_present", "timeout": 15}},
		{Name: "browser_shot", Input: map[string]any{"label": "after-goto"}},
	}
	plan, _, _, _ := assemblePlan(calls, "goal", "http://localhost:8989", services)
	if len(plan.Cases) != 1 {
		t.Fatalf("want 1 case, got %d: %+v", len(plan.Cases), plan.Cases)
	}
	tc := plan.Cases[0]
	if tc.Action != "browser_flow" {
		t.Fatalf("action = %q, want browser_flow", tc.Action)
	}
	if tc.Target != "http://localhost:5183" {
		t.Fatalf("target must be the service's ui base_url, got %q", tc.Target)
	}
	if len(tc.Steps) != 3 {
		t.Fatalf("steps: %+v", tc.Steps)
	}
	if tc.Steps[0].Action != "browser_goto" || tc.Steps[0].URL != "/dashboard/missions" {
		t.Fatalf("goto step: %+v", tc.Steps[0])
	}
	e := tc.Steps[1]
	if e.Action != "browser_expect" || e.Target != "text=Connected" || e.Timeout != 15 {
		t.Fatalf("expect step: %+v", e)
	}
	if e.Asserts["expectation"] != "text_present" {
		t.Fatalf("comparator: %+v", e.Asserts)
	}
	if tc.Steps[2].Action != "browser_shot" || tc.Steps[2].Label != "after-goto" {
		t.Fatalf("shot step: %+v", tc.Steps[2])
	}
}

// A begin_case with browser steps but NO declared ui vocabulary cannot
// resolve a UI base URL — the case must be dropped, not run against the
// API URL (goto would load a JSON 404 page and every assertion would lie).
func TestAssemblePlanBrowserFlowWithoutUIVocab(t *testing.T) {
	services := []project.Service{{Name: "open-agents", URL: "http://localhost:8989/ws/{userId}"}}
	calls := []llm.ToolCall{
		{Name: "begin_case", Input: map[string]any{
			"name": "x", "expectation": "y", "service": "open-agents"}},
		{Name: "browser_goto", Input: map[string]any{"url": "/dashboard"}},
	}
	plan, _, _, _ := assemblePlan(calls, "goal", "http://localhost:8989", services)
	if len(plan.Cases) != 0 {
		t.Fatalf("browser case without ui vocab must drop, got %+v", plan.Cases)
	}
}

// browser steps outside any begin_case are no-ops (same policy as ws_*).
func TestAssemblePlanBrowserStepOrphan(t *testing.T) {
	services := []project.Service{{Name: "s", URL: "http://x",
		Vocabulary: &project.Vocabulary{UI: &project.VocabUI{BaseURL: "http://ui", Locale: "en"}}}}
	plan, _, _, _ := assemblePlan([]llm.ToolCall{
		{Name: "browser_goto", Input: map[string]any{"url": "/x"}},
	}, "goal", "http://x", services)
	if len(plan.Cases) != 0 {
		t.Fatalf("orphan browser step must be dropped, got %+v", plan.Cases)
	}
}

// A mixed case (ws steps then browser steps) must not silently mix executor
// families: the browser retag happens on FIRST browser step, so ws-first
// cases keep ws_flow and later browser steps... must also be rejected to
// avoid a chimera. The assembler drops browser steps on a ws_flow open case.
func TestAssemblePlanMixedFlowRejected(t *testing.T) {
	services := []project.Service{{Name: "s", URL: "http://x",
		Vocabulary: &project.Vocabulary{UI: &project.VocabUI{BaseURL: "http://ui", Locale: "en"}}}}
	plan, _, _, _ := assemblePlan([]llm.ToolCall{
		{Name: "begin_case", Input: map[string]any{"name": "m", "expectation": "e", "service": "s"}},
		{Name: "ws_connect", Input: map[string]any{"role": "web"}},
		{Name: "browser_goto", Input: map[string]any{"url": "/x"}},
	}, "goal", "http://x", services)
	if len(plan.Cases) != 1 {
		t.Fatalf("want 1 case, got %d", len(plan.Cases))
	}
	if plan.Cases[0].Action != "ws_flow" || len(plan.Cases[0].Steps) != 1 {
		t.Fatalf("ws case must not absorb browser steps: %+v", plan.Cases[0])
	}
}
