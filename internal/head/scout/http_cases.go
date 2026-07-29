package scout

import (
	"fmt"
	"maps"
	"slices"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/project"
)

// HTTPCasesCovered emits one lazy GET-smoke fallback per HTTP endpoint covered
// by an LLM HTTP case (A1 #4). The smoke asserts reachable-and-non-5xx; the
// Agent skips it by default (Priority<0) and activates it only when the bound
// primary case fails non-environmentally. Reuses the generic FallbackFor
// activation + Recovered tally/render — no Agent/store/report changes.
//
// Determinism: services are iterated in cfg.Services slice order and each
// service's paths in sorted name order, so the emitted plan is stable across
// runs (map iteration order is random). Mirrors WSCasesCovered's discipline.
func HTTPCasesCovered(cfg *project.Config, httpCovering map[string]map[string]string) []agent.TestCase {
	if cfg == nil || len(httpCovering) == 0 {
		return nil
	}
	// cfg.Services is the source of truth for URLs; the smoke case carries
	// Service so the executor resolves the same URL as the covering LLM case.

	var cases []agent.TestCase
	for _, svc := range cfg.Services {
		paths := httpCovering[svc.Name]
		if len(paths) == 0 {
			continue
		}
		sortedPaths := slices.Sorted(maps.Keys(paths))
		for _, path := range sortedPaths {
			covererID := paths[path]
			if covererID == "" {
				continue
			}
			cases = append(cases, httpSmokeCase(svc.Name, path, covererID))
		}
	}
	return cases
}

// httpSmokeCase builds a lazy GET-smoke fallback bound to the LLM HTTP case
// that covered this endpoint. Target/Service match the primary so the executor
// resolves the same URL; Method=GET with a path Target routes through the
// deterministic rule engine (matchHTTPRules Rule 1), staying off the ReAct LLM
// path. The non-5xx judgment is applied by the rule engine (Task 3).
func httpSmokeCase(service, path, covererID string) agent.TestCase {
	return agent.TestCase{
		ID:          fmt.Sprintf("smoke-%s-%s", service, trimPathForID(path)),
		Name:        fmt.Sprintf("smoke GET %s", path),
		Target:      path,
		Method:      "GET",
		Service:     service,
		Expectation: "reachable and non-5xx (any 2xx/3xx/4xx response; no transport error/timeout)",
		FallbackFor: covererID,
		Priority:    -1,
	}
}

// trimPathForID turns a path into an ID-safe fragment (e.g. "/users/:id" ->
// "users-id"). IDs are cosmetic here; they only need to be stable + unique
// within a service.
func trimPathForID(path string) string {
	out := make([]byte, 0, len(path))
	for _, r := range path {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			out = append(out, byte(r))
		default:
			out = append(out, '-')
		}
	}
	// collapse leading/trailing dashes
	s := string(out)
	for len(s) > 0 && s[0] == '-' {
		s = s[1:]
	}
	for len(s) > 0 && s[len(s)-1] == '-' {
		s = s[:len(s)-1]
	}
	if s == "" {
		s = "root"
	}
	return s
}
