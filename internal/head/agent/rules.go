package agent

import (
	"strings"

	"github.com/binoctal/cerberus/internal/project"
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

// Match attempts to produce a deterministic Action for the given TestCase.
// Returns the Action and true if matched, zero Action and false otherwise.
func (r *RuleEngine) Match(tc TestCase) (Action, bool) {
	// Rule 1: API test — method is set and target is a path.
	if tc.Method != "" && strings.HasPrefix(tc.Target, "/") {
		action := Action{
			Type:   ActionAPIRequest,
			Target: r.baseURL + tc.Target,
			Method: strings.ToUpper(tc.Method),
		}
		// Attach first actor's credentials as basic auth headers if available.
		if len(r.actors) > 0 {
			action.Headers = r.authHeaders()
		}
		if tc.Expectation != "" {
			action.Value = tc.Expectation
		}
		return action, true
	}

	// Rule 2: Navigate action with a path target.
	if tc.Action == "navigate" && strings.HasPrefix(tc.Target, "/") {
		return Action{
			Type:   ActionNavigate,
			Target: r.baseURL + tc.Target,
		}, true
	}

	// Rule 3: Target is a full URL.
	if isURL(tc.Target) {
		action := Action{
			Type:   ActionNavigate,
			Target: tc.Target,
		}
		if tc.Method != "" {
			action.Type = ActionAPIRequest
			action.Method = strings.ToUpper(tc.Method)
		}
		return action, true
	}

	// No rule matches — AI Steer needed.
	return Action{}, false
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

