package scout

import (
	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/project"
)

// uiVocabCases emits one deterministic browser_flow per declared UI
// assertion: goto the route, wait-assert the display promise (spec §4).
// Assertions are STATIC promises — true of the page itself once rendered;
// protocol-coupled values (e.g. "mission card shows 3 tasks 100%") are a
// follow-up. Not claim-bound (same honesty tier as httpRouteCases: display
// reachability, not ledger promises). Unsupported assertions are outside the
// coverage denominator and emit nothing.
func uiVocabCases(svc project.Service) []agent.TestCase {
	if svc.Vocabulary == nil || svc.Vocabulary.UI == nil {
		return nil
	}
	ui := svc.Vocabulary.UI
	var cases []agent.TestCase
	for _, a := range ui.Assertions {
		if a.Unsupported {
			continue
		}
		timeout := a.Timeout
		if timeout == 0 {
			timeout = 10
		}
		cases = append(cases, agent.TestCase{
			ID:      "ui-vocab-" + a.ID,
			Name:    "UI assertion " + a.ID,
			Action:  "browser_flow",
			Target:  ui.BaseURL,
			Service: svc.Name,
			Steps: []agent.TestStep{
				{Action: "browser_goto", URL: a.Route},
				{Action: "browser_expect", Target: a.Target, Type: a.ID,
					Asserts: map[string]any{"expectation": a.Expectation}, Timeout: timeout},
			},
			Expectation: "UI assertion " + a.ID + " holds (" + a.Expectation + " " + a.Target + ")",
		})
	}
	return cases
}
