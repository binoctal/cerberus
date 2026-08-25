package agent

import (
	"github.com/binoctal/cerberus/internal/types"
)

// matchBrowserRules matches browser automation actions
func (r *RuleEngine) matchBrowserRules(tc TestCase) (types.TypedAction, bool) {
	switch tc.Action {
	// Rule 12: browser_goto
	case "browser_goto":
		url := tc.Target
		if !isURL(url) {
			url = r.baseURLFor(tc) + url
		}
		return types.BrowserGotoAction{URL: url}, true

	// Rule 13: browser_click
	case "browser_click":
		return types.BrowserClickAction{Selector: tc.Target}, true

	// Rule 14: browser_fill
	case "browser_fill":
		return types.BrowserFillAction{Selector: tc.Target, Value: tc.Expectation}, true

	// Rule 15: browser_eval
	case "browser_eval":
		return types.BrowserEvalAction{Expression: tc.Target}, true

	// Rule 15b: browser_expect — wait-type DOM assertion. The comparator
	// rides Expectation; TestCase carries no timeout field, so the executor
	// default window applies.
	case "browser_expect":
		return types.BrowserExpectAction{Selector: tc.Target, Expectation: tc.Expectation}, true

	default:
		return nil, false
	}
}
