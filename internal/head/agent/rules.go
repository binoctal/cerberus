package agent

import (
	"strings"
	"sync/atomic"

	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/types"
)

// RuleEngine matches test cases to deterministic actions (zero tokens).
type RuleEngine struct {
	baseURL string
	actors  []project.Actor
	workDir string
	hits    atomic.Int64
	misses  atomic.Int64
}

// NewRuleEngine creates a rule engine for the given base URL, actors, and workDir.
// workDir is used as the working directory for process and code actions.
func NewRuleEngine(baseURL string, actors []project.Actor, workDir string) *RuleEngine {
	return &RuleEngine{
		baseURL: strings.TrimRight(baseURL, "/"),
		actors:  actors,
		workDir: workDir,
	}
}

// Match attempts to produce a deterministic TypedAction for the given TestCase.
// Returns the action and true if matched, nil and false otherwise.
func (r *RuleEngine) Match(tc TestCase) (types.TypedAction, bool) {
	action, matched := r.matchRules(tc)
	if matched {
		r.hits.Add(1)
	} else {
		r.misses.Add(1)
	}
	return action, matched
}

// Stats returns the cumulative hit and miss counts for observability.
func (r *RuleEngine) Stats() (hits, misses int64) {
	return r.hits.Load(), r.misses.Load()
}

// matchRules contains the actual rule matching logic.
func (r *RuleEngine) matchRules(tc TestCase) (types.TypedAction, bool) {
	// Phase 1: HTTP and navigate rules
	if action, matched := r.matchHTTPRules(tc); matched {
		return action, true
	}

	// Phase 2: Process execution rules
	if action, matched := r.matchProcessRules(tc); matched {
		return action, true
	}

	// Phase 3: Code analysis rules
	if action, matched := r.matchCodeRules(tc); matched {
		return action, true
	}

	// Phase 4: File operation rules
	if action, matched := matchFileRules(tc); matched {
		return action, true
	}

	// Phase 5: Wait rule
	if action, matched := matchWaitRule(tc); matched {
		return action, true
	}

	// Phase 6: MCP call rule
	if action, matched := matchMCPRule(tc); matched {
		return action, true
	}

	// Phase 7: Browser automation rules
	if action, matched := r.matchBrowserRules(tc); matched {
		return action, true
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
