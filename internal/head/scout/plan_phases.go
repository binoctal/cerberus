package scout

import (
	"context"

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

// executeDirectPlanning runs single-call AI planning with fallback.
func (s *Scout) executeDirectPlanning(ctx context.Context, goal string, model *project.ProjectModel) (*agent.TestPlan, error) {
	return s.directPlan(ctx, goal, model)
}

// augmentPlan appends executor-specific test cases (process, code, file).
func (s *Scout) augmentPlan(plan *agent.TestPlan, goal string) {
	s.appendExecutorCases(plan, goal)
}

// Plan generates a TestPlan from the goal and project model.
// Uses ToT deep planning if enabled, otherwise direct AI planning.
// Executor test cases (process, code, file) are appended based on
// the detected project type.
func (s *Scout) Plan(ctx context.Context, goal string, model *project.ProjectModel) (*agent.TestPlan, error) {
	var plan *agent.TestPlan
	var err error

	// Phase 1: Build memory context for all planning modes
	memory := s.buildMemoryContext(ctx, goal, model)

	// Phase 2: Execute planning based on mode
	if s.deepPlan {
		plan, err = s.executeDeepPlanning(ctx, goal, model, memory)
	} else {
		// Direct AI planning also uses memory context
		plan, err = s.executeDirectPlanning(ctx, goal, model)
	}

	if err != nil {
		return nil, err
	}

	// Phase 3: Augment plan with executor cases
	s.augmentPlan(plan, goal)

	return plan, nil
}

// appendExecutorCases detects the project type and appends non-HTTP test
// cases (build, test, lint, code analysis) plus WS connect/receive cases
// (when any service declares a protocol) to the plan.
func (s *Scout) appendExecutorCases(plan *agent.TestPlan, goal string) {
	rootDir := s.config.Code.Root
	if rootDir == "" {
		rootDir = "."
	}
	info := DetectProjectType(rootDir)
	cases := GenerateExecutorCases(info, goal)
	cases = append(cases, WSCases(s.config, goal)...)
	if len(cases) > 0 {
		s.logger.Info("appended executor cases",
			zap.String("project_type", string(info.Type)),
			zap.Int("cases", len(cases)),
		)
		plan.Cases = append(plan.Cases, cases...)
	}
}
