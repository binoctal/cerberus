package scout

import (
	"context"
	"net/url"

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

	return planner.Plan(ctx, goal, model, s.resolveBaseURL())
}

// executeDirectPlanning runs single-call AI tool-use planning with fallback.
// Returns the plan plus the per-service set of WS roles the LLM already
// connected (via begin_case+ws_connect groups), so WSCasesCovered can suppress
// redundant deterministic connects.
func (s *Scout) executeDirectPlanning(ctx context.Context, goal string, model *project.ProjectModel) (*agent.TestPlan, map[string]map[string]bool, error) {
	return s.directPlan(ctx, goal, model)
}

// augmentPlan appends executor-specific cases (process, code, WS) and then
// drops WS-endpoint HTTP drift. covered roles (already connected by an
// LLM-authored begin_case+ws_* group) are passed so WSCasesCovered does not
// redundantly re-connect them.
func (s *Scout) augmentPlan(plan *agent.TestPlan, goal string, covered map[string]map[string]bool) {
	s.appendExecutorCases(plan, goal, covered)
	filterWSEndpointDrift(plan, s.config) // Finding-3: drop WS-endpoint HTTP drift
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
	var err error
	if s.deepPlan {
		plan, err = s.executeDeepPlanning(ctx, goal, model, memory)
		covered = map[string]map[string]bool{}
	} else {
		plan, covered, err = s.executeDirectPlanning(ctx, goal, model)
	}

	if err != nil {
		return nil, err
	}

	// Phase 3: Augment plan with executor cases
	s.augmentPlan(plan, goal, covered)

	return plan, nil
}

// appendExecutorCases detects the project type and appends non-HTTP test
// cases (build, test, lint, code analysis) plus WS connect/receive cases
// (when any service declares a protocol) to the plan.
func (s *Scout) appendExecutorCases(plan *agent.TestPlan, goal string, covered map[string]map[string]bool) {
	rootDir := s.config.Code.Root
	if rootDir == "" {
		rootDir = "."
	}
	info := DetectProjectType(rootDir)
	cases := GenerateExecutorCases(info, goal)
	cases = append(cases, WSCasesCovered(s.config, goal, covered)...)
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
	wsPaths := map[string]bool{}
	for _, svc := range cfg.Services {
		if svc.Protocol == nil {
			continue
		}
		if u, err := url.Parse(svc.URL); err == nil && u.Path != "" {
			wsPaths[u.Path] = true
		}
	}
	if len(wsPaths) == 0 {
		return
	}
	kept := make([]agent.TestCase, 0, len(plan.Cases))
	for _, c := range plan.Cases {
		if wsPaths[urlPathOf(c.Target)] && !isWSAction(c.Action) {
			continue
		}
		kept = append(kept, c)
	}
	plan.Cases = kept
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

// isWSAction reports whether action is one of the WS executor actions. The set
// is fixed by the coder/websocket executor.
func isWSAction(action string) bool {
	switch action {
	case "ws_connect", "ws_send", "ws_receive", "ws_disconnect", "ws_flow":
		return true
	}
	return false
}
