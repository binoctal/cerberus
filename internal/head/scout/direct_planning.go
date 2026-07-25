package scout

import (
	"context"
	"fmt"
	"strings"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/project"
)

// buildPlanningContext constructs the base context for AI planning.
func (s *Scout) buildPlanningContext(model *project.ProjectModel, memory string) string {
	planCtx := s.buildPlanContext(model)

	// Add service information with body_template hints.
	if len(s.config.Services) > 0 {
		planCtx += "\n\n## Services\n"
		for _, svc := range s.config.Services {
			planCtx += fmt.Sprintf("- %s: %s", svc.Name, svc.URL)
			if len(svc.PathPrefix) > 0 {
				planCtx += fmt.Sprintf(" (prefixes: %v)", svc.PathPrefix)
			}
			if svc.BodyTemplate != "" {
				planCtx += fmt.Sprintf("\n  body_template: %s", svc.BodyTemplate)
			}
			planCtx += "\n"
		}
	}

	// Inject L1 episodic memory for known targets.
	if memory != "" {
		planCtx += "\n\n## Previous Test History\n" + memory
	}

	return planCtx
}

// buildPlanningPrompt constructs the full AI prompt for planning.
func (s *Scout) buildPlanningPrompt(ctx context.Context, goal string, model *project.ProjectModel, memory string) string {
	planCtx := s.buildPlanningContext(model, memory)

	system := promptPlanSystem
	if s.isLocalOnly() {
		system = promptPlanSystemLocal
	}
	return ai.NewPrompt().
		System(system).
		Context(planCtx).
		Task(fmt.Sprintf("Generate test cases for this project.\nTest Goal: %s", goal)).
		Output(promptPlanToolGuide).
		Build()
}

// runAIPlanning calls DecideWithTools and assembles tool calls into a plan.
// Zero tool calls (drift/quality) → error. Transient LLM call error → fallback.
func (s *Scout) runAIPlanning(ctx context.Context, prompt string, goal string, model *project.ProjectModel) (*agent.TestPlan, map[string]map[string]bool, error) {
	res, err := s.driver.DecideWithTools(ctx, prompt, planTools())
	if err != nil {
		// Transient LLM call failure: degrade to deterministic fallback plan
		// so a flaky provider never blocks the run.
		s.logger.Warn("AI planning call failed, using deterministic fallback", zap.Error(err))
		fb := s.fallbackPlan(goal, model)
		return fb, map[string]map[string]bool{}, nil
	}
	if len(res.ToolCalls) == 0 {
		return nil, nil, fmt.Errorf("scout plan: zero tool calls (drift or quality)")
	}
	plan, covered := assemblePlan(res.ToolCalls, goal, s.resolveBaseURL(), s.config.Services)
	if len(plan.Cases) == 0 {
		return nil, nil, fmt.Errorf("scout plan: assembly produced zero cases")
	}

	s.logger.Info("test plan generated",
		zap.String("goal", goal),
		zap.Int("cases", len(plan.Cases)),
	)

	return plan, covered, nil
}

// directPlan generates a test plan via a single AI tool-calling round with
// deterministic fallback on transient LLM errors.
func (s *Scout) directPlan(ctx context.Context, goal string, model *project.ProjectModel) (*agent.TestPlan, map[string]map[string]bool, error) {
	memory := s.buildEpisodicContext(ctx, goal, model)
	prompt := s.buildPlanningPrompt(ctx, goal, model, memory)
	return s.runAIPlanning(ctx, prompt, goal, model)
}

// fallbackPlan generates test cases deterministically from the model
// when AI planning is unavailable.
func (s *Scout) fallbackPlan(goal string, model *project.ProjectModel) *agent.TestPlan {
	baseURL := s.resolveBaseURL()
	var cases []agent.TestCase

	// Generate one case per endpoint.
	for i, ep := range model.API.Endpoints {
		cases = append(cases, agent.TestCase{
			ID:          fmt.Sprintf("tc-%03d", i+1),
			Name:        fmt.Sprintf("%s %s returns success", ep.Method, ep.Path),
			Target:      ep.Path,
			Method:      ep.Method,
			Expectation: "Returns 2xx status code",
			Priority:    ep.Confidence,
			Service:     attributeService(ep.Path, s.config.Services),
		})
	}

	// Add invariants as test cases.
	for i, inv := range model.InvariantHints {
		cases = append(cases, agent.TestCase{
			ID:          fmt.Sprintf("inv-%03d", i+1),
			Name:        fmt.Sprintf("Invariant: %s", inv.ID),
			Target:      inv.Description,
			Expectation: inv.Description,
			Priority:    inv.Confidence,
			Severity:    inv.Severity,
		})
	}

	// If no cases generated, add a default health check.
	if len(cases) == 0 && baseURL != "" {
		cases = append(cases, agent.TestCase{
			ID:          "default-health",
			Name:        "Default health check",
			Target:      "/",
			Method:      "GET",
			Expectation: "Returns 200 OK",
			Priority:    0.5,
		})
	}

	// Fill body from service templates (mirrors assemblePlan's fillBody call).
	cases = fillBody(cases, s.config.Services)

	return &agent.TestPlan{
		Goal:       goal,
		Cases:      cases,
		ProjectURL: baseURL,
	}
}

func (s *Scout) resolveBaseURL() string {
	if len(s.config.Services) > 0 {
		return s.config.Services[0].URL
	}
	return ""
}

// attributeService returns the service whose PathPrefix contains the given
// endpoint path, or "" if none match (caller falls back to Services[0]).
func attributeService(path string, services []project.Service) string {
	for _, s := range services {
		for _, p := range s.PathPrefix {
			if strings.HasPrefix(path, p) {
				return s.Name
			}
		}
	}
	return ""
}

// fillBody sets each case's Body from its own body, falling back to the
// attributed service's body_template when the LLM emitted none. GET/DELETE
// keep empty body.
func fillBody(cases []agent.TestCase, services []project.Service) []agent.TestCase {
	byName := make(map[string]project.Service, len(services))
	for _, s := range services {
		byName[s.Name] = s
	}
	for i := range cases {
		c := &cases[i]
		if c.Body != "" {
			continue
		}
		m := strings.ToUpper(c.Method)
		if m != "POST" && m != "PUT" && m != "PATCH" {
			continue
		}
		if svc, ok := byName[c.Service]; ok && svc.BodyTemplate != "" {
			c.Body = svc.BodyTemplate
		}
	}
	return cases
}
