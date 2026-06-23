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
		Output(promptPlanOutput).
		Build()
}

// runAIPlanning executes AI planning with deterministic fallback.
func (s *Scout) runAIPlanning(ctx context.Context, prompt string, goal string, model *project.ProjectModel) (*agent.TestPlan, error) {
	var out PlanOutput
	if err := s.driver.Decide(ctx, prompt, &out); err != nil {
		// Fallback: generate test cases directly from the model without AI.
		s.logger.Warn("AI planning failed, using deterministic fallback", zap.Error(err))
		return s.fallbackPlan(goal, model), nil
	}

	// AI returned parseable but empty result — fall back to deterministic plan.
	if len(out.Cases) == 0 {
		s.logger.Warn("AI planning returned zero cases, using deterministic fallback")
		return s.fallbackPlan(goal, model), nil
	}

	return s.convertPlanOutput(goal, out), nil
}

// convertPlanOutput transforms AI output into a TestPlan.
func (s *Scout) convertPlanOutput(goal string, out PlanOutput) *agent.TestPlan {
	cases := make([]agent.TestCase, 0, len(out.Cases))
	for _, c := range out.Cases {
		cases = append(cases, agent.TestCase{
			ID:          c.ID,
			Name:        c.Name,
			Target:      c.Target,
			Method:      c.Method,
			Action:      c.Action,
			Expectation: c.Expectation,
			Priority:    c.Priority,
			Service:     attributeService(c.Target, s.config.Services),
		})
	}

	// Phase 2: LLM verification of service attribution.
	cases = verifyServiceAttribution(s.driver, cases, s.config.Services)

	plan := &agent.TestPlan{
		Goal:       goal,
		Cases:      cases,
		ProjectURL: s.resolveBaseURL(),
	}

	s.logger.Info("test plan generated",
		zap.String("goal", goal),
		zap.Int("cases", len(plan.Cases)),
	)

	return plan
}

// directPlan generates a test plan via a single AI call with deterministic fallback.
func (s *Scout) directPlan(ctx context.Context, goal string, model *project.ProjectModel) (*agent.TestPlan, error) {
	// Phase 1: Build memory context
	memory := s.buildEpisodicContext(ctx, goal, model)

	// Phase 2: Build planning prompt
	prompt := s.buildPlanningPrompt(ctx, goal, model, memory)

	// Phase 3: Run AI planning with fallback
	plan, err := s.runAIPlanning(ctx, prompt, goal, model)
	if err != nil {
		return nil, err
	}

	return plan, nil
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

// ServiceAttributionCorrection represents a single correction from the LLM.
type ServiceAttributionCorrection struct {
	Path    string `json:"path"`
	Service string `json:"service"`
}

// ServiceAttributionCorrections is the envelope format for LLM corrections.
type ServiceAttributionCorrections struct {
	Corrections []ServiceAttributionCorrection `json:"corrections"`
}

// verifyServiceAttribution uses the LLM to verify and correct service attribution
// for test cases. Returns cases with corrected service values. On any error,
// returns cases unchanged and logs a warning.
func verifyServiceAttribution(driver *ai.Driver, cases []agent.TestCase, services []project.Service) []agent.TestCase {
	// Build a set of valid service names for validation.
	validServices := make(map[string]bool)
	for _, svc := range services {
		validServices[svc.Name] = true
	}

	// Build the prompt for LLM verification.
	prompt := ai.NewPrompt().
		System("You are a service attribution verifier. Correct any misattributed services based on the test case paths and target.").
		Task(fmt.Sprintf("Verify service attribution for %d test cases. Return corrections ONLY when attribution is wrong.\n\n%s", len(cases), formatCasesForVerification(cases))).
		Output(`{"corrections":[{"path":"/path/to/case","service":"correct_service_name"}]}`).
		Build()

	// Call LLM to get corrections.
	var corrections ServiceAttributionCorrections
	if err := driver.Decide(context.Background(), prompt, &corrections); err != nil {
		// On LLM error, return cases unchanged.
		fmt.Printf("LLM verification failed: %v\n", err)
		return cases
	}

	// Apply corrections: build a map of path -> corrected service.
	correctionMap := make(map[string]string)
	for _, corr := range corrections.Corrections {
		// Only apply corrections whose target service exists.
		if !validServices[corr.Service] {
			fmt.Printf("Ignoring correction for %s: unknown service '%s'\n", corr.Path, corr.Service)
			continue
		}
		correctionMap[corr.Path] = corr.Service
	}

	// Apply corrections to cases.
	result := make([]agent.TestCase, len(cases))
	for i, tc := range cases {
		if correctedService, ok := correctionMap[tc.Target]; ok {
			tc.Service = correctedService
		}
		result[i] = tc
	}

	return result
}

// formatCasesForVerification formats test cases for LLM verification.
func formatCasesForVerification(cases []agent.TestCase) string {
	var sb strings.Builder
	sb.WriteString("Current test cases:\n")
	for _, tc := range cases {
		sb.WriteString(fmt.Sprintf("- Path: %s, Current Service: %s\n", tc.Target, tc.Service))
	}
	return sb.String()
}
