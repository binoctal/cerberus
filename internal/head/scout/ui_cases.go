package scout

import (
	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/project"
)

// uiVocabCases emits one deterministic browser_flow per declared UI
// assertion: goto the route, wait-assert the display promise (spec §4).
// Static assertions are true of the page itself once rendered; a
// protocol-coupled assertion (from_api) prepends an authenticated GET whose
// captured values template into the selector as {{case.<name>}} — the
// display must then agree with the protocol-side truth.
// Not claim-bound (same honesty tier as httpRouteCases: display
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
		steps := []agent.TestStep{
			{Action: "browser_goto", URL: a.Route},
			{Action: "browser_expect", Target: a.Target, Type: a.ID,
				Asserts: map[string]any{"expectation": a.Expectation}, Timeout: timeout},
		}
		if a.FromAPI != nil {
			authRole := a.FromAPI.AuthRole
			if authRole == "" {
				authRole = "web"
			}
			steps = append([]agent.TestStep{{
				Action: "http_request", Method: a.FromAPI.Method,
				URL:      serviceHost(svc.URL) + a.FromAPI.Path,
				AuthRole: authRole, ExpectStatusClass: "2xx",
				Capture: a.FromAPI.Capture,
			}}, steps...)
		}
		cases = append(cases, agent.TestCase{
			ID:          "ui-vocab-" + a.ID,
			Name:        "UI assertion " + a.ID,
			Action:      "browser_flow",
			Target:      ui.BaseURL,
			Service:     svc.Name,
			Steps:       steps,
			Expectation: "UI assertion " + a.ID + " holds (" + a.Expectation + " " + a.Target + ")",
		})
	}
	return cases
}
