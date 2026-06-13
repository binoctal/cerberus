package scout

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/ai"
	embedPkg "github.com/binoctal/cerberus/internal/embed"
	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/store"
)

// Scout performs project reconnaissance: analyze to build a cognitive model,
// then plan to generate a test plan.
type Scout struct {
	driver   *ai.Driver
	store    *store.Store
	config   *project.Config
	logger   *zap.Logger
	deepPlan bool              // Enable ToT deep planning mode
	totCfg   ToTConfig         // ToT configuration (only used when deepPlan=true)
	embedder embedPkg.Provider // embedding provider for semantic search
}

// NewScout creates a Scout head.
func NewScout(driver *ai.Driver, store *store.Store, config *project.Config, logger *zap.Logger) *Scout {
	return &Scout{
		driver:   driver,
		store:    store,
		config:   config,
		logger:   logger,
		embedder: embedPkg.NewTrigramProvider(embedPkg.DefaultDimension),
	}
}

// SetDeepPlan enables ToT deep planning mode with the given config.
func (s *Scout) SetDeepPlan(cfg ToTConfig) {
	s.deepPlan = true
	s.totCfg = cfg
}

// Analyze builds a ProjectModel from the target info using AI inference
// and the project configuration as ground truth.
func (s *Scout) Analyze(ctx context.Context, target TargetInfo) (*project.ProjectModel, error) {
	// Start with ground truth from project config.
	model := s.buildModelFromConfig()

	// If the model already has good coverage from config, skip AI inference.
	if model.InfoScore(false) >= 0.7 {
		s.logger.Info("model has sufficient coverage from config, skipping AI inference",
			zap.Float64("info_score", model.InfoScore(false)),
		)
		return model, nil
	}

	// Use AI to infer additional endpoints and pages.
	analyzeCtx := s.buildAnalyzeContext(target)
	prompt := ai.NewPrompt().
		System(promptAnalyzeSystem).
		Context(analyzeCtx).
		Task(s.buildAnalyzeTask(target)).
		Output(promptAnalyzeOutput).
		Build()

	var out AnalyzeOutput
	if err := s.driver.Decide(ctx, prompt, &out); err != nil {
		s.logger.Warn("AI analysis failed, using config-only model", zap.Error(err))
		return model, nil // Graceful degradation: return what we have.
	}

	// Merge AI-inferred data into the model.
	s.mergeAIInference(model, out)

	s.logger.Info("project model built",
		zap.Int("endpoints", len(model.API.Endpoints)),
		zap.Int("pages", len(model.Navigation.Pages)),
		zap.Float64("info_score", model.InfoScore(false)),
	)

	return model, nil
}

// buildAnalyzeTask constructs the analysis task prompt.
// When URL is empty (local-only mode), the Base URL line is omitted
// so the LLM skips HTTP test case generation.
func (s *Scout) buildAnalyzeTask(target TargetInfo) string {
	if target.URL == "" {
		return fmt.Sprintf("Analyze this project and infer its testable surface.\nGoal: %s", target.Goal)
	}
	return fmt.Sprintf("Analyze this SaaS project and infer its API surface.\nBase URL: %s\nGoal: %s",
		target.URL, target.Goal)
}

// Plan generates a TestPlan from the goal and project model.
// Uses ToT deep planning if enabled, otherwise direct AI planning.
// Executor test cases (process, code, file) are appended based on
// the detected project type.
func (s *Scout) Plan(ctx context.Context, goal string, model *project.ProjectModel) (*agent.TestPlan, error) {
	var plan *agent.TestPlan
	var err error

	// ToT deep planning mode.
	if s.deepPlan {
		planner := NewToTPlanner(s.driver, s.totCfg, s.logger)
		plan, err = planner.Plan(ctx, goal, model, s.resolveBaseURL())
	} else {
		// Direct AI planning (default).
		plan, err = s.directPlan(ctx, goal, model)
	}

	if err != nil {
		return nil, err
	}

	s.appendExecutorCases(plan, goal)
	return plan, nil
}

// appendExecutorCases detects the project type and appends non-HTTP test
// cases (build, test, lint, code analysis) to the plan.
func (s *Scout) appendExecutorCases(plan *agent.TestPlan, goal string) {
	rootDir := s.config.Code.Root
	if rootDir == "" {
		rootDir = "."
	}
	info := DetectProjectType(rootDir)
	cases := GenerateExecutorCases(info, goal)
	if len(cases) > 0 {
		s.logger.Info("appended executor cases",
			zap.String("project_type", string(info.Type)),
			zap.Int("cases", len(cases)),
		)
		plan.Cases = append(plan.Cases, cases...)
	}
}

// directPlan generates a test plan via a single AI call with deterministic fallback.
func (s *Scout) directPlan(ctx context.Context, goal string, model *project.ProjectModel) (*agent.TestPlan, error) {
	planCtx := s.buildPlanContext(model)

	// Inject L1 episodic memory for known targets.
	if episodicCtx := s.buildEpisodicContext(ctx, goal, model); episodicCtx != "" {
		planCtx += "\n\n## Previous Test History\n" + episodicCtx
	}

	prompt := ai.NewPrompt().
		System(promptPlanSystem).
		Context(planCtx).
		Task(fmt.Sprintf("Generate test cases for this project.\nTest Goal: %s", goal)).
		Output(promptPlanOutput).
		Build()

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

	// Convert PlanOutput to TestPlan.
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
		})
	}

	plan := &agent.TestPlan{
		Goal:       goal,
		Cases:      cases,
		ProjectURL: s.resolveBaseURL(),
	}

	s.logger.Info("test plan generated",
		zap.String("goal", goal),
		zap.Int("cases", len(plan.Cases)),
	)

	return plan, nil
}

// buildModelFromConfig creates a ProjectModel from the project YAML config.
func (s *Scout) buildModelFromConfig() *project.ProjectModel {
	model := &project.ProjectModel{}

	// Add endpoints from config invariants (which reference specific paths).
	for _, inv := range s.config.Invariants {
		model.InvariantHints = append(model.InvariantHints, project.InvariantHint{
			ID:          inv.ID,
			Source:      "config",
			Description: inv.Description,
			Confidence:  0.95, // Explicit config = high confidence.
			Severity:    inv.Severity,
		})
	}

	// Add known endpoints from service health checks.
	for _, svc := range s.config.Services {
		if svc.Health != "" {
			model.API.Endpoints = append(model.API.Endpoints, project.EndpointDef{
				Method:     "GET",
				Path:       svc.Health,
				Confidence: 0.95,
			})
		}
	}

	return model
}

// buildAnalyzeContext formats project info for the Analyze prompt.
func (s *Scout) buildAnalyzeContext(target TargetInfo) string {
	var b strings.Builder

	fmt.Fprintf(&b, "Project: %s\n", s.config.Project.Name)
	fmt.Fprintf(&b, "Base URL: %s\n", target.URL)

	if len(s.config.Services) > 0 {
		b.WriteString("\nServices:\n")
		for _, svc := range s.config.Services {
			fmt.Fprintf(&b, "- %s: %s (health: %s)\n", svc.Name, svc.URL, svc.Health)
		}
	}

	if len(s.config.Invariants) > 0 {
		b.WriteString("\nInvariants:\n")
		for _, inv := range s.config.Invariants {
			fmt.Fprintf(&b, "- [%s] %s (check: %s, assertion: %s)\n",
				inv.ID, inv.Description, inv.Check, inv.Assertion)
		}
	}

	if len(s.config.Databases) > 0 {
		b.WriteString("\nDatabases:\n")
		for _, db := range s.config.Databases {
			fmt.Fprintf(&b, "- %s\n", db.Name)
		}
	}

	// Include already-known endpoints as ground truth.
	model := s.buildModelFromConfig()
	if len(model.API.Endpoints) > 0 {
		b.WriteString("\nKnown endpoints:\n")
		for _, ep := range model.API.Endpoints {
			fmt.Fprintf(&b, "- %s %s (confidence: %.1f)\n", ep.Method, ep.Path, ep.Confidence)
		}
	}

	return b.String()
}

// mergeAIInference adds AI-discovered endpoints and pages to the model.
// Avoids duplicates with config-ground-truth entries.
func (s *Scout) mergeAIInference(model *project.ProjectModel, aiOut AnalyzeOutput) {
	existingEndpoints := make(map[string]bool)
	for _, ep := range model.API.Endpoints {
		existingEndpoints[ep.Method+" "+ep.Path] = true
	}

	for _, ep := range aiOut.Endpoints {
		key := ep.Method + " " + ep.Path
		if !existingEndpoints[key] {
			model.API.Endpoints = append(model.API.Endpoints, project.EndpointDef{
				Method:     ep.Method,
				Path:       ep.Path,
				Confidence: ep.Confidence,
			})
		}
	}

	existingPages := make(map[string]bool)
	for _, pg := range model.Navigation.Pages {
		existingPages[pg.Path] = true
	}

	for _, pg := range aiOut.Pages {
		if !existingPages[pg.Path] {
			model.Navigation.Pages = append(model.Navigation.Pages, project.PageDef{
				Path:       pg.Path,
				Confidence: pg.Confidence,
			})
		}
	}

	model.TechStack = append(model.TechStack, aiOut.TechStack...)
}

// buildPlanContext formats the ProjectModel for the Plan prompt.
func (s *Scout) buildPlanContext(model *project.ProjectModel) string {
	var b strings.Builder

	if len(model.API.Endpoints) > 0 {
		b.WriteString("API Endpoints:\n")
		for _, ep := range model.API.Endpoints {
			fmt.Fprintf(&b, "- %s %s (confidence: %.1f)\n", ep.Method, ep.Path, ep.Confidence)
		}
	}

	if len(model.Navigation.Pages) > 0 {
		b.WriteString("\nPages:\n")
		for _, pg := range model.Navigation.Pages {
			fmt.Fprintf(&b, "- %s (confidence: %.1f)\n", pg.Path, pg.Confidence)
		}
	}

	if len(model.InvariantHints) > 0 {
		b.WriteString("\nInvariants:\n")
		for _, inv := range model.InvariantHints {
			fmt.Fprintf(&b, "- [%s] %s\n", inv.ID, inv.Description)
		}
	}

	modelJSON, _ := json.Marshal(model)
	b.WriteString("\n## Raw Model\n")
	b.WriteString(string(modelJSON))

	return b.String()
}

// buildEpisodicContext queries L1 episodic memory for known targets and formats
// a summary of previous test outcomes to inform planning.
func (s *Scout) buildEpisodicContext(ctx context.Context, goal string, model *project.ProjectModel) string {
	// Collect unique targets from the model.
	seen := make(map[string]bool)
	var targets []string
	for _, ep := range model.API.Endpoints {
		key := ep.Method + " " + ep.Path
		if !seen[key] {
			seen[key] = true
			targets = append(targets, ep.Path) // Use path for episodic lookup
		}
	}

	var b strings.Builder
	for _, target := range targets {
		memories, err := s.store.GetEpisodicByTarget(ctx, target, 10)
		if err != nil {
			s.logger.Debug("episodic lookup failed", zap.String("target", target), zap.Error(err))
			continue
		}
		if len(memories) == 0 {
			continue
		}
		fmt.Fprintf(&b, "Target %s:\n", target)
		for _, m := range memories {
			fmt.Fprintf(&b, "- %s (verdict: %s, duration: %dms)\n", m.Status, m.Verdict, m.DurationMs)
		}
	}

	// Append L2 semantic memory: search for facts related to the goal.
	if goal != "" {
		queryEmb, _ := s.embedder.Embed(ctx, goal)
		semanticResults, err := s.store.SearchSemanticForProject(ctx, queryEmb, s.config.Project.Name, 5, 0.3)
		if err != nil {
			s.logger.Debug("semantic search failed", zap.Error(err))
		} else if len(semanticResults) > 0 {
			fmt.Fprintf(&b, "\nRelated past insights:\n")
			for _, sr := range semanticResults {
				fmt.Fprintf(&b, "- %s (score: %.2f)\n", sr.Content, sr.Score)
			}
		}
	}

	return b.String()
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
