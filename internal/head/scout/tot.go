package scout

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/project"
)

// ToTConfig controls the Tree-of-Thought beam search parameters. The search
// explores a "strategy tree" along three orthogonal dimensions; each field
// constrains one.
//
//   - GenerateN (breadth):   how many child strategies each surviving parent
//     expands into during the propose phase (e.g. happy-path / error / edge /
//     security angles). This is "how many you make".
//   - BeamWidth (survivors): how many top-scored candidates the select phase
//     keeps after pruning the rest. This is "how many you keep".
//   - MaxSteps (depth):      how many propose→evaluate→select rounds run. Each
//     round re-proposes from the survivors, so it is iterative refinement of
//     the previous best, not a plain N-level tree expansion.
//
// Per-step evaluate cost scales with BeamWidth × GenerateN; total cost scales
// roughly with MaxSteps × (BeamWidth × GenerateN). Raise MaxSteps for depth,
// GenerateN for breadth, BeamWidth to avoid pruning good strategies (the most
// expensive, because it compounds every subsequent step).
type ToTConfig struct {
	BeamWidth int // Candidates kept per step after pruning (default 3).
	GenerateN int // Candidates proposed per surviving parent each step (default 5).
	MaxSteps  int // Propose→evaluate→select refinement rounds (default 3).
}

func DefaultToTConfig() ToTConfig {
	return ToTConfig{BeamWidth: 3, GenerateN: 5, MaxSteps: 3}
}

// PlanCandidate represents a test strategy in the ToT search.
type PlanCandidate struct {
	Description string   `json:"description"`
	Cases       []string `json:"cases"`
	Score       float64  `json:"score"`
	AIScore     float64  `json:"ai_score"`
	Coverage    float64  `json:"coverage"` // Deterministic endpoint coverage score
}

// ProposeOutput is the LLM response for a Propose call.
type ProposeOutput struct {
	Strategies []StrategyProposal `json:"strategies"`
}

// StrategyProposal is a single proposed test strategy.
type StrategyProposal struct {
	Description string   `json:"description"`
	Cases       []string `json:"cases"`
}

// EvaluateOutput is the LLM response for an Evaluate call.
type EvaluateOutput struct {
	Score     float64 `json:"score"`
	Reasoning string  `json:"reasoning"`
}

// ToTPlanner uses Tree-of-Thought beam search for deep test planning.
type ToTPlanner struct {
	driver *ai.Driver
	config ToTConfig
	logger *zap.Logger
}

// NewToTPlanner creates a ToT planner.
func NewToTPlanner(driver *ai.Driver, config ToTConfig, logger *zap.Logger) *ToTPlanner {
	return &ToTPlanner{driver: driver, config: config, logger: logger}
}

// Plan runs the ToT beam search: propose → evaluate → select for MaxSteps rounds.
func (t *ToTPlanner) Plan(ctx context.Context, goal string, model *project.ProjectModel, baseURL string) (*agent.TestPlan, error) {
	// Start with the goal as the only candidate.
	candidates := []PlanCandidate{{Description: goal}}

	for step := 0; step < t.config.MaxSteps; step++ {
		t.logger.Info("tot step",
			zap.Int("step", step+1),
			zap.Int("candidates_in", len(candidates)),
		)

		// Phase 1: Propose — expand each candidate into N proposals.
		var expanded []PlanCandidate
		for _, c := range candidates {
			proposals, err := t.propose(ctx, c, model, goal)
			if err != nil {
				t.logger.Warn("tot propose failed, stopping search", zap.Error(err))
				return t.bestToPlan(candidates, goal, baseURL), nil
			}
			expanded = append(expanded, proposals...)
		}

		if len(expanded) == 0 {
			t.logger.Warn("tot no proposals generated, stopping search")
			return t.bestToPlan(candidates, goal, baseURL), nil
		}

		// Phase 2: Evaluate — score each proposal.
		scored, err := t.evaluate(ctx, expanded, model)
		if err != nil {
			t.logger.Warn("tot evaluate failed, stopping search", zap.Error(err))
			return t.bestToPlan(candidates, goal, baseURL), nil
		}

		// Phase 3: Select — keep top-k.
		sort.Slice(scored, func(i, j int) bool { return scored[i].Score > scored[j].Score })
		if len(scored) > t.config.BeamWidth {
			scored = scored[:t.config.BeamWidth]
		}
		candidates = scored

		t.logger.Info("tot step complete",
			zap.Int("step", step+1),
			zap.Int("kept", len(candidates)),
			zap.Float64("best_score", candidates[0].Score),
		)
	}

	return t.bestToPlan(candidates, goal, baseURL), nil
}

// propose generates N candidate strategies from a parent candidate.
func (t *ToTPlanner) propose(ctx context.Context, parent PlanCandidate, model *project.ProjectModel, goal string) ([]PlanCandidate, error) {
	modelSummary := formatModelForToT(model)
	task := fmt.Sprintf(`Propose %d different test strategies.

Parent strategy: %s
Project Model:
%s

Test Goal: %s

Each strategy should focus on a different aspect (happy path, error handling, edge cases, security, etc.) and include concrete test case descriptions.`,
		t.config.GenerateN, parent.Description, modelSummary, goal)

	prompt := ai.NewPrompt().
		System(`You are a test strategy planner. Generate diverse, high-quality test strategies.
Output JSON with a "strategies" array. Each strategy has "description" and "cases" (array of test case descriptions).`).
		Task(task).
		Output(`{"strategies": [{"description": "...", "cases": ["test case 1", "test case 2"]}]}`).
		Build()

	var out ProposeOutput
	if err := t.driver.Decide(ctx, prompt, &out); err != nil {
		return nil, fmt.Errorf("tot propose: %w", err)
	}

	var candidates []PlanCandidate
	for _, s := range out.Strategies {
		candidates = append(candidates, PlanCandidate{
			Description: s.Description,
			Cases:       s.Cases,
		})
	}
	return candidates, nil
}

// evaluate scores each candidate using AI (70%) + deterministic coverage (30%).
func (t *ToTPlanner) evaluate(ctx context.Context, candidates []PlanCandidate, model *project.ProjectModel) ([]PlanCandidate, error) {
	endpointSummary := formatEndpointsForEval(model)

	// Score all candidates in parallel.
	var wg sync.WaitGroup
	results := make([]PlanCandidate, len(candidates))

	for i := range candidates {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			c := candidates[idx]

			// Deterministic coverage score (30%).
			c.Coverage = t.coverageScore(&c, model)

			// AI quality score (70%).
			aiScore, err := t.aiScore(ctx, &c, endpointSummary)
			if err != nil {
				t.logger.Warn("tot ai score failed", zap.Error(err))
				aiScore = 5.0 // Mid-range fallback.
			}
			c.AIScore = aiScore

			// Combined: AI 70% + coverage 30%.
			c.Score = (aiScore / 10.0 * 0.7) + (c.Coverage * 0.3)

			results[idx] = c
		}(i)
	}
	wg.Wait()

	return results, nil
}

// aiScore asks the LLM to rate a strategy on a 1-10 scale.
func (t *ToTPlanner) aiScore(ctx context.Context, c *PlanCandidate, endpointSummary string) (float64, error) {
	casesJSON, _ := json.Marshal(c.Cases)
	task := fmt.Sprintf(`Rate this test strategy on a scale of 1-10.

Focus on STRATEGY QUALITY:
- Risk focus: targets high-risk, high-impact areas?
- Completeness: covers happy path AND error cases AND edge cases?
- Diversity: tests different types of failures?
- Efficiency: can be executed within time constraints?

Known endpoints (for coverage reference):
%s

Strategy: %s
Cases: %s

Output ONLY: {"score": N, "reasoning": "brief explanation"}`,
		endpointSummary, c.Description, string(casesJSON))

	prompt := ai.NewPrompt().
		System("You are a test strategy evaluator. Rate strategies objectively.").
		Task(task).
		Output(`{"score": 7, "reasoning": "explanation"}`).
		Build()

	var out EvaluateOutput
	if err := t.driver.Decide(ctx, prompt, &out); err != nil {
		return 5.0, err
	}
	return out.Score, nil
}

// coverageScore computes a deterministic endpoint coverage score [0, 1].
func (t *ToTPlanner) coverageScore(c *PlanCandidate, model *project.ProjectModel) float64 {
	if len(model.API.Endpoints) == 0 {
		return 0.5 // No endpoints to cover.
	}

	// Count how many endpoint paths are mentioned in the strategy cases.
	casesText := strings.ToLower(strings.Join(c.Cases, " "))
	matched := 0
	for _, ep := range model.API.Endpoints {
		pathKey := strings.ToLower(strings.TrimPrefix(ep.Path, "/"))
		if pathKey == "" {
			continue
		}
		// Check if any part of the path appears in the cases text.
		parts := strings.Split(pathKey, "/")
		for _, part := range parts {
			if len(part) > 2 && strings.Contains(casesText, part) {
				matched++
				break
			}
		}
	}

	return float64(matched) / float64(len(model.API.Endpoints))
}

// bestToPlan converts the best candidate to a TestPlan.
func (t *ToTPlanner) bestToPlan(candidates []PlanCandidate, goal, baseURL string) *agent.TestPlan {
	if len(candidates) == 0 {
		return &agent.TestPlan{Goal: goal, ProjectURL: baseURL}
	}

	best := candidates[0]
	cases := make([]agent.TestCase, 0, len(best.Cases))
	for i, desc := range best.Cases {
		caseID := fmt.Sprintf("tot-%03d", i+1)
		cases = append(cases, agent.TestCase{
			ID:          caseID,
			Name:        truncateName(desc, 80),
			Target:      inferTarget(desc),
			Expectation: desc,
			Priority:    1.0 - float64(i)*0.05, // Slightly decreasing priority.
		})
	}

	return &agent.TestPlan{
		Goal:       goal,
		Cases:      cases,
		ProjectURL: baseURL,
	}
}

// formatModelForToT creates a compact model summary for ToT prompts.
func formatModelForToT(model *project.ProjectModel) string {
	var b strings.Builder
	if len(model.API.Endpoints) > 0 {
		b.WriteString("Endpoints:\n")
		for _, ep := range model.API.Endpoints {
			fmt.Fprintf(&b, "- %s %s (conf: %.1f)\n", ep.Method, ep.Path, ep.Confidence)
		}
	}
	if len(model.Navigation.Pages) > 0 {
		b.WriteString("Pages:\n")
		for _, pg := range model.Navigation.Pages {
			fmt.Fprintf(&b, "- %s (conf: %.1f)\n", pg.Path, pg.Confidence)
		}
	}
	if len(model.InvariantHints) > 0 {
		b.WriteString("Invariants:\n")
		for _, inv := range model.InvariantHints {
			fmt.Fprintf(&b, "- [%s] %s\n", inv.ID, inv.Description)
		}
	}
	if len(model.TechStack) > 0 {
		fmt.Fprintf(&b, "Tech stack: %s\n", strings.Join(model.TechStack, ", "))
	}
	return b.String()
}

// formatEndpointsForEval creates a compact endpoint list for evaluation prompts.
func formatEndpointsForEval(model *project.ProjectModel) string {
	if len(model.API.Endpoints) == 0 {
		return "(no known endpoints)"
	}
	var parts []string
	for _, ep := range model.API.Endpoints {
		parts = append(parts, ep.Method+" "+ep.Path)
	}
	return strings.Join(parts, ", ")
}

// truncateName truncates a string to maxLen, adding "..." if needed.
func truncateName(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// inferTarget attempts to extract an HTTP path from a test case description.
func inferTarget(desc string) string {
	desc = strings.ToLower(desc)
	// Look for patterns like "get /path" or "post /path".
	for _, method := range []string{"get", "post", "put", "delete", "patch"} {
		idx := strings.Index(desc, method+" /")
		if idx >= 0 {
			// Extract from the method to end of path-like segment.
			rest := desc[idx:]
			end := strings.IndexAny(rest, " \t\n,.")
			if end < 0 {
				end = len(rest)
			}
			return rest[:end]
		}
	}
	// Look for bare paths.
	if idx := strings.Index(desc, "/api/"); idx >= 0 {
		rest := desc[idx:]
		end := strings.IndexAny(rest, " \t\n,.")
		if end < 0 {
			end = len(rest)
		}
		return rest[:end]
	}
	return desc
}
