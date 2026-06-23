package agent

import (
	"strings"
	"sync/atomic"

	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/types"
)

// RuleEngine matches test cases to deterministic actions (zero tokens).
type RuleEngine struct {
	services []project.Service
	byName   map[string]project.Service
	actors   []project.Actor
	workDir  string
	hits     atomic.Int64
	misses   atomic.Int64
}

// NewRuleEngine creates a rule engine for the given services, actors, and workDir.
// workDir is used as the working directory for process and code actions.
func NewRuleEngine(services []project.Service, actors []project.Actor, workDir string) *RuleEngine {
	byName := make(map[string]project.Service, len(services))
	for _, s := range services {
		byName[s.Name] = s
	}
	return &RuleEngine{services: services, byName: byName, actors: actors, workDir: workDir}
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

// authHeadersFor returns auth headers for tc.Service's actor, falling back to
// a global actor (Actor.Service == "") then actors[0].
func (r *RuleEngine) authHeadersFor(tc TestCase) map[string]string {
	if len(r.actors) == 0 {
		return nil
	}
	var actor project.Actor
	found := false
	if tc.Service != "" {
		for _, a := range r.actors {
			if a.Service == tc.Service {
				actor, found = a, true
				break
			}
		}
	}
	if !found {
		for _, a := range r.actors {
			if a.Service == "" {
				actor, found = a, true
				break
			}
		}
	}
	if !found {
		actor = r.actors[0]
	}
	h := map[string]string{}
	if actor.Credentials.Email != "" {
		h["X-Test-User"] = actor.Credentials.Email
	}
	for k, v := range actor.Credentials.Headers {
		h[k] = v
	}
	if len(h) == 0 {
		return nil
	}
	return h
}

// baseURLFor returns the URL for tc.Service, falling back to the first
// configured service (backward compatible with single-service projects).
func (r *RuleEngine) baseURLFor(tc TestCase) string {
	if tc.Service != "" {
		if s, ok := r.byName[tc.Service]; ok {
			return strings.TrimRight(s.URL, "/")
		}
	}
	if len(r.services) > 0 {
		return strings.TrimRight(r.services[0].URL, "/")
	}
	return ""
}

// serviceHeaders returns service-level headers for tc.Service (nil if none).
func (r *RuleEngine) serviceHeaders(tc TestCase) map[string]string {
	if tc.Service != "" {
		if s, ok := r.byName[tc.Service]; ok && len(s.Headers) > 0 {
			return s.Headers
		}
	}
	if len(r.services) > 0 && len(r.services[0].Headers) > 0 {
		return r.services[0].Headers
	}
	return nil
}


func isURL(s string) bool {
	return strings.Contains(s, "://")
}
