package scout

import (
	"testing"

	"github.com/binoctal/cerberus/internal/project"
)

func TestUIVocabCases(t *testing.T) {
	svc := project.Service{Name: "open-agents", URL: "ws://localhost:8989/ws"}
	svc.Vocabulary = &project.Vocabulary{UI: &project.VocabUI{
		BaseURL: "http://localhost:5183", Locale: "en",
		Assertions: []project.VocabUIAssertion{
			{ID: "missions-conn-status", Route: "/dashboard/missions", Target: "text=Connected", Expectation: "text_present", Timeout: 15},
			{ID: "devices-table", Route: "/dashboard/devices", Target: "css=table tbody tr", Expectation: "element_count>=1"},
			{ID: "exempt", Route: "/x", Target: "text=y", Expectation: "text_present", Unsupported: true, Reason: "plan-gated"},
		},
	}}
	cases := uiVocabCases(svc)
	if len(cases) != 2 {
		t.Fatalf("want 2 cases (unsupported skipped), got %d", len(cases))
	}
	c := cases[0]
	if c.ID != "ui-vocab-missions-conn-status" || c.Action != "browser_flow" || c.Target != "http://localhost:5183" {
		t.Fatalf("case shape: %+v", c)
	}
	if len(c.Steps) != 2 || c.Steps[0].Action != "browser_goto" || c.Steps[0].URL != "/dashboard/missions" {
		t.Fatalf("steps: %+v", c.Steps)
	}
	e := c.Steps[1]
	if e.Action != "browser_expect" || e.Target != "text=Connected" || e.Type != "missions-conn-status" || e.Timeout != 15 {
		t.Fatalf("expect step: %+v", e)
	}
	if e.Asserts["expectation"] != "text_present" {
		t.Fatalf("comparator: %+v", e.Asserts)
	}
	// Timeout defaulting: 0 -> 10.
	if cases[1].Steps[1].Timeout != 10 {
		t.Errorf("default timeout: %+v", cases[1].Steps[1])
	}
}

func TestUIVocabCasesNil(t *testing.T) {
	svc := project.Service{Name: "open-agents"}
	if got := uiVocabCases(svc); got != nil {
		t.Errorf("nil vocabulary must yield nil, got %d", len(got))
	}
	svc.Vocabulary = &project.Vocabulary{}
	if got := uiVocabCases(svc); got != nil {
		t.Errorf("nil ui must yield nil, got %d", len(got))
	}
}
