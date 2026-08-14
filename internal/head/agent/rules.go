package agent

import (
	"net/url"
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
	wsIdx    *WSProtocolIndex // optional; resolves {{role.param}} placeholders in HTTP case targets
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

// SetWSIndex wires the WS protocol index so HTTP rule-engine cases resolve
// {{role.param}} / {{param}} placeholders in their target URL the same way the
// http_request step path does (websocket.go resolvePlaceholders). Without it, a
// Scout free-form HTTP case carrying a placeholder target (e.g.
// /api/devices/{{bridge.deviceId}}/restart) dials the literal string and fails.
// Optional; nil leaves targets literal.
func (r *RuleEngine) SetWSIndex(idx *WSProtocolIndex) { r.wsIdx = idx }

// selectActor chooses the actor for tc.Service, falling back to a global actor
// (Actor.Service == "") then actors[0]. Shared by auth-header selection
// (authHeadersFor) and HTTP target placeholder resolution so both attribute the
// same actor for a case.
func (r *RuleEngine) selectActor(tc TestCase) project.Actor {
	if len(r.actors) == 0 {
		return project.Actor{}
	}
	if tc.Service != "" {
		for _, a := range r.actors {
			if a.Service == tc.Service {
				return a
			}
		}
	}
	for _, a := range r.actors {
		if a.Service == "" {
			return a
		}
	}
	return r.actors[0]
}

// resolveHTTPURL resolves {{role.param}}/{{param}} placeholders in an HTTP
// case URL when a WS protocol index is wired, mirroring the http_request step
// path (websocket.go resolvePlaceholders). No index, no protocol, no
// placeholder, or an unresolved token leaves the URL unchanged.
func (r *RuleEngine) resolveHTTPURL(tc TestCase, url string) string {
	if r.wsIdx == nil || !strings.Contains(url, "{{") {
		return url
	}
	svc, ok := r.byName[tc.Service]
	if !ok {
		// Match baseURLFor's fallback: a case with no/unknown Service resolves
		// against the first service (single-service projects) — the same service
		// that supplied the base URL.
		if len(r.services) == 0 {
			return url
		}
		svc = r.services[0]
	}
	if svc.Protocol == nil {
		return url
	}
	if resolved, err := resolvePlaceholders(r.wsIdx, svc.Protocol, r.selectActor(tc).Name, url); err == nil {
		return resolved
	}
	return url
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
	actor := r.selectActor(tc)
	h := map[string]string{}
	if actor.Credentials.Email != "" {
		h["X-Test-User"] = actor.Credentials.Email
	}
	for k, v := range actor.Credentials.Headers {
		h[k] = v
	}
	// A declared http_login captures an HTTP-route JWT (RawHTTPToken) distinct
	// from the WS web-token that inject_as placed in Headers. HTTP cases must
	// authenticate with the JWT, so it overrides the WS Authorization —
	// otherwise protected HTTP routes 401 on the web-token. Actors without an
	// http_login (RawHTTPToken empty) keep their Headers Authorization as-is.
	if actor.Credentials.RawHTTPToken != "" {
		h["Authorization"] = "Bearer " + actor.Credentials.RawHTTPToken
	}
	if len(h) == 0 {
		return nil
	}
	return h
}

// baseURLFor returns the URL for tc.Service, falling back to the first
// configured service (backward compatible with single-service projects).
func (r *RuleEngine) baseURLFor(tc TestCase) string {
	svcURL := ""
	if tc.Service != "" {
		if s, ok := r.byName[tc.Service]; ok {
			svcURL = s.URL
		}
	} else if len(r.services) > 0 {
		svcURL = r.services[0].URL
	}
	return httpBaseURL(svcURL)
}

// httpBaseURL turns a service URL into the base for HTTP API requests. A
// WebSocket service commonly declares its URL as a connection template with a
// path placeholder (e.g. "http://h:8989/ws/{userId}"); only scheme://host is a
// valid HTTP base, otherwise a host-relative API path concatenates onto the
// template (".../ws/{userId}/devices/...") and hits the WS upgrade endpoint
// (HTTP 426). A path with no placeholder (e.g. "/api/v1") is a legitimate REST
// base and is preserved. Falls back to the trimmed raw URL on parse error.
func httpBaseURL(svcURL string) string {
	svcURL = strings.TrimRight(svcURL, "/")
	if svcURL == "" {
		return ""
	}
	u, err := url.Parse(svcURL)
	if err != nil || !u.IsAbs() {
		return svcURL
	}
	if strings.Contains(u.Path, "{") {
		return u.Scheme + "://" + u.Host
	}
	return svcURL
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
