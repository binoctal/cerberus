package session

import (
	"testing"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/project"
)

func uiTestConfig() *project.Config {
	cfg := project.DefaultConfig()
	cfg.Services = []project.Service{{
		Name: "open-agents",
		Vocabulary: &project.Vocabulary{UI: &project.VocabUI{
			BaseURL: "http://localhost:5183", Locale: "en",
			Assertions: []project.VocabUIAssertion{
				{ID: "missions-conn-status", Route: "/dashboard/missions", Target: "text=Connected", Expectation: "text_present"},
				{ID: "exempt", Route: "/x", Target: "text=y", Expectation: "text_present", Unsupported: true, Reason: "plan-gated"},
			},
		}},
	}}
	return &cfg
}

func TestRequiredEdgesIncludesUIAssertions(t *testing.T) {
	sess := &Session{Config: uiTestConfig()}
	edges := requiredEdges(sess)
	var uiEdges []project.VocabEdge
	for _, e := range edges {
		if e.Trigger == "ui_assert" {
			uiEdges = append(uiEdges, e)
		}
	}
	if len(uiEdges) != 1 {
		t.Fatalf("ui edges: got %d want 1 (unsupported excluded): %+v", len(uiEdges), uiEdges)
	}
	if uiEdges[0].Type != "ui_assert missions-conn-status" || uiEdges[0].FromRole != "browser" || uiEdges[0].ToRole != "web_ui" {
		t.Errorf("ui edge shape: %+v", uiEdges[0])
	}
}

func TestExercisedEdgesCreditsBrowserExpect(t *testing.T) {
	sess := &Session{Config: uiTestConfig()}
	req := requiredEdges(sess)
	results := []agent.StepResult{{
		TestCase: &agent.TestCase{Action: "browser_flow", Target: "http://localhost:5183", Steps: []agent.TestStep{
			{Action: "browser_goto", URL: "/dashboard/missions"},
			{Action: "browser_expect", Type: "missions-conn-status"},
		}},
		Evidence: []agent.Evidence{
			{Action: "browser_expect", MatchedType: "missions-conn-status", Matched: true},
		},
	}}
	exercised, _ := exercisedEdges(results, req, nil)
	credited := false
	for _, e := range req {
		if e.Trigger == "ui_assert" {
			credited = exercised[edgeKey(e.FromRole, e.ToRole, e.Type)]
		}
	}
	if !credited {
		t.Error("matched browser_expect evidence must credit the ui_assert edge")
	}

	// An UNMATCHED expect must not credit.
	bad := []agent.StepResult{{
		TestCase: &agent.TestCase{Action: "browser_flow", Steps: []agent.TestStep{{Action: "browser_expect", Type: "missions-conn-status"}}},
		Evidence: []agent.Evidence{{Action: "browser_expect", MatchedType: "missions-conn-status", Matched: false}},
	}}
	exercised, _ = exercisedEdges(bad, req, nil)
	for _, e := range req {
		if e.Trigger == "ui_assert" && exercised[edgeKey(e.FromRole, e.ToRole, e.Type)] {
			t.Error("unmatched browser_expect must not credit the ui_assert edge")
		}
	}
}
