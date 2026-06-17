package agent

import (
	"strings"

	"github.com/binoctal/cerberus/internal/types"
)

// matchHTTPRules matches HTTP and navigate actions
func (r *RuleEngine) matchHTTPRules(tc TestCase) (types.TypedAction, bool) {
	// Rule 1: API test — method is set and target is a path.
	if tc.Method != "" && strings.HasPrefix(tc.Target, "/") {
		action := types.HTTPAction{
			Method: strings.ToUpper(tc.Method),
			URL:    r.baseURL + tc.Target,
		}
		if len(r.actors) > 0 {
			action.Headers = r.authHeaders()
		}
		return action, true
	}

	// Rule 2: Navigate action with a path target.
	if tc.Action == "navigate" && strings.HasPrefix(tc.Target, "/") {
		return types.NavigateAction{URL: r.baseURL + tc.Target}, true
	}

	// Rule 3: Target is a full URL.
	if isURL(tc.Target) {
		if tc.Method != "" {
			return types.HTTPAction{
				Method: strings.ToUpper(tc.Method),
				URL:    tc.Target,
			}, true
		}
		return types.NavigateAction{URL: tc.Target}, true
	}

	return nil, false
}
