package agent

import (
	"strings"

	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/types"
)

// RuleEngine matches test cases to deterministic actions (zero tokens).
type RuleEngine struct {
	baseURL string
	actors  []project.Actor
}

// NewRuleEngine creates a rule engine for the given base URL and actors.
func NewRuleEngine(baseURL string, actors []project.Actor) *RuleEngine {
	return &RuleEngine{baseURL: strings.TrimRight(baseURL, "/"), actors: actors}
}

// Match attempts to produce a deterministic TypedAction for the given TestCase.
// Returns the action and true if matched, nil and false otherwise.
func (r *RuleEngine) Match(tc TestCase) (types.TypedAction, bool) {
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

	// No rule matches — AI Steer needed.
	return nil, false
}

// authHeaders returns basic auth headers from the first configured actor.
func (r *RuleEngine) authHeaders() map[string]string {
	if len(r.actors) == 0 {
		return nil
	}
	actor := r.actors[0]
	if actor.Credentials.Email == "" {
		return nil
	}
	return map[string]string{
		"X-Test-User": actor.Credentials.Email,
	}
}

func isURL(s string) bool {
	return strings.Contains(s, "://")
}
