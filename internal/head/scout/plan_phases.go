package scout

import (
	"context"
	"net/url"
	"regexp"
	"strings"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/project"
)

// selectPlanningDriver chooses the appropriate driver (ToT vs direct).
func (s *Scout) selectPlanningDriver() (propose, evaluate *ai.Driver) {
	propose = s.proposeDriver
	if propose == nil {
		propose = s.driver
	}
	evaluate = s.evaluateDriver
	if evaluate == nil {
		evaluate = s.driver
	}
	return propose, evaluate
}

// buildMemoryContext constructs episodic and semantic memory context.
func (s *Scout) buildMemoryContext(ctx context.Context, goal string, model *project.ProjectModel) string {
	return s.buildEpisodicContext(ctx, goal, model)
}

// executeDeepPlanning runs ToT deep planning with memory.
func (s *Scout) executeDeepPlanning(ctx context.Context, goal string, model *project.ProjectModel, memory string) (*agent.TestPlan, error) {
	propose, evaluate := s.selectPlanningDriver()
	planner := NewToTPlanner(propose, evaluate, s.totCfg, s.logger)

	// ToT mode recalls cross-session memory too (closes the mutual-exclusion
	// gap where deep planning previously discarded episodic/semantic context).
	if memory != "" {
		planner.SetMemory(memory)
	}
	planner.SetVocabSummary(project.RenderVocabSummary(s.config.Services))

	return planner.Plan(ctx, goal, model, s.resolveBaseURL())
}

// executeDirectPlanning runs single-call AI tool-use planning with fallback.
// Returns the plan plus the per-service set of WS roles the LLM already
// connected (via begin_case+ws_connect groups), so WSCasesCovered can suppress
// redundant deterministic connects.
func (s *Scout) executeDirectPlanning(ctx context.Context, goal string, model *project.ProjectModel) (*agent.TestPlan, map[string]map[string]bool, map[string]map[string]string, map[string]map[string]string, error) {
	return s.directPlan(ctx, goal, model)
}

// augmentPlan appends executor-specific cases (process, code, WS) and then
// drops WS-endpoint HTTP drift. covered roles (already connected by an
// LLM-authored begin_case+ws_* group) are passed so WSCasesCovered does not
// redundantly re-connect them.
func (s *Scout) augmentPlan(plan *agent.TestPlan, goal string, covered map[string]map[string]bool, coveringCase map[string]map[string]string, httpCovering map[string]map[string]string) {
	s.appendExecutorCases(plan, goal, covered, coveringCase, httpCovering)
	filterWSEndpointDrift(plan, s.config)      // Finding-3: drop WS-endpoint HTTP drift
	filterProcessBoundConnects(plan, s.config) // tc-004: drop connects as real-process roles
}

// Plan generates a TestPlan from the goal and project model.
// Uses ToT deep planning if enabled, otherwise direct AI tool-use planning.
// Executor test cases (process, code, file) are appended based on
// the detected project type.
func (s *Scout) Plan(ctx context.Context, goal string, model *project.ProjectModel) (*agent.TestPlan, error) {
	// Phase 1: Build memory context for all planning modes
	memory := s.buildMemoryContext(ctx, goal, model)

	// Phase 2: Execute planning based on mode. Direct planning returns a
	// covered map; ToT synthesizes an empty one (it authors Steps directly).
	var plan *agent.TestPlan
	var covered map[string]map[string]bool
	var coveringCase map[string]map[string]string
	var httpCovering map[string]map[string]string
	var err error
	if s.deepPlan {
		plan, err = s.executeDeepPlanning(ctx, goal, model, memory)
		covered = map[string]map[string]bool{}
		coveringCase = map[string]map[string]string{}
		httpCovering = map[string]map[string]string{}
	} else {
		plan, covered, coveringCase, httpCovering, err = s.executeDirectPlanning(ctx, goal, model)
	}

	if err != nil {
		return nil, err
	}

	// Phase 3: Augment plan with executor cases
	s.augmentPlan(plan, goal, covered, coveringCase, httpCovering)

	return plan, nil
}

// appendExecutorCases detects the project type and appends non-HTTP test
// cases (build, test, lint, code analysis) plus WS connect/receive cases
// (when any service declares a protocol) to the plan.
func (s *Scout) appendExecutorCases(plan *agent.TestPlan, goal string, covered map[string]map[string]bool, coveringCase map[string]map[string]string, httpCovering map[string]map[string]string) {
	rootDir := s.config.Code.Root
	if rootDir == "" {
		rootDir = "."
	}
	info := DetectProjectType(rootDir)
	cases := GenerateExecutorCases(info, goal)
	cases = append(cases, WSCasesCovered(s.config, goal, covered, coveringCase)...)
	cases = append(cases, HTTPCasesCovered(s.config, httpCovering)...)
	if len(cases) > 0 {
		s.logger.Info("appended executor cases",
			zap.String("project_type", string(info.Type)),
			zap.Int("cases", len(cases)),
		)
		plan.Cases = append(plan.Cases, cases...)
	}
}

// filterWSEndpointDrift drops LLM free-form cases that target a WS endpoint
// with a non-ws_* action — the HTTP drift that produces 426 noise. Legitimate
// HTTP REST exploration (a different path), deterministic WS cases (a ws_*
// action), and LLM ws_* attempts on a WS endpoint are all kept. No-op when no
// service declares a protocol (byte-identical for non-WS projects).
func filterWSEndpointDrift(plan *agent.TestPlan, cfg *project.Config) {
	wsMatchers := []*regexp.Regexp{}
	for _, svc := range cfg.Services {
		if svc.Protocol == nil {
			continue
		}
		if u, err := url.Parse(svc.URL); err == nil && u.Path != "" {
			wsMatchers = append(wsMatchers, wsPathMatcher(u.Path))
		}
	}
	if len(wsMatchers) == 0 {
		return
	}
	kept := make([]agent.TestCase, 0, len(plan.Cases))
	for _, c := range plan.Cases {
		if !isWSAction(c.Action) {
			for _, m := range wsMatchers {
				if m.MatchString(urlPathOf(c.Target)) {
					goto drift
				}
			}
		}
		kept = append(kept, c)
	drift:
	}
	plan.Cases = kept
}

// wsPathMatcher builds a matcher for a declared WS path where each {param}
// segment is a single wildcard segment, and the path also matches its own
// sub-paths (/ws/{userId} covers /ws/u1 and /ws/u1/health — the whole
// template subtree is served by the WS handler that 426s non-upgrades).
func wsPathMatcher(wsPath string) *regexp.Regexp {
	segs := strings.Split(strings.Trim(wsPath, "/"), "/")
	for i, s := range segs {
		if strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}") {
			segs[i] = "[^/]+"
		} else {
			segs[i] = regexp.QuoteMeta(s)
		}
	}
	return regexp.MustCompile("^/" + strings.Join(segs, "/") + "(/.*)?$")
}

// filterProcessBoundConnects drops LLM ws_flow cases that ws_connect as a
// process-bound role — a role whose connection is owned by a real-process
// actor. The executor has no token for such an actor, so the connect fails at
// injectAuth ("ws auth: no token for actor") on every run (the tc-004 dogfood
// failure). The deterministic generators never connect as these roles; this
// filter closes the LLM exploration path. No-op when no role is process-bound.
func filterProcessBoundConnects(plan *agent.TestPlan, cfg *project.Config) {
	bound := map[string]bool{}
	for _, svc := range cfg.Services {
		if svc.Protocol == nil {
			continue
		}
		for name, role := range svc.Protocol.Roles {
			if role != nil && role.ProcessBound {
				bound[name] = true
			}
		}
	}
	if len(bound) == 0 {
		return
	}
	kept := make([]agent.TestCase, 0, len(plan.Cases))
	for _, c := range plan.Cases {
		if c.Action == "ws_flow" && connectsAsBoundRole(c.Steps, bound) {
			continue
		}
		kept = append(kept, c)
	}
	plan.Cases = kept
}

// connectsAsBoundRole reports whether any step is a ws_connect naming a
// process-bound role. Other ws_* steps on a bound connection_id cannot occur
// without the connect (the executor fails them as unknown connections).
func connectsAsBoundRole(steps []agent.TestStep, bound map[string]bool) bool {
	for _, st := range steps {
		if st.Action == "ws_connect" && bound[st.Role] {
			return true
		}
	}
	return false
}

// urlPathOf returns the path component of a target, which may be an absolute
// URL (http://h/ws/x), a relative path (/ws/x), or a ws:// URL. Returns "" on a
// parse failure or empty target so it never matches a WS path.
func urlPathOf(target string) string {
	if u, err := url.Parse(target); err == nil {
		return u.Path
	}
	return ""
}

// isWSAction reports whether action is one of the stepped-executor actions
// (WS choreography or browser UI flow). The set is fixed by the executors;
// these cases' Target is a dial/base URL, not an HTTP probe path, so the
// URL-drift filter must exempt them (a browser_flow whose service lookup
// fell back to the plan base URL would otherwise match a /ws/… path and be
// dropped as drift).
func isWSAction(action string) bool {
	switch action {
	case "ws_connect", "ws_send", "ws_receive", "ws_disconnect", "ws_flow", "browser_flow":
		return true
	}
	return false
}
