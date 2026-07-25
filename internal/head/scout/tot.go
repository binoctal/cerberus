package scout

import (
	"context"
	"fmt"
	"sort"

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

// DefaultToTConfig returns default ToT configuration.
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

// ToTPlanner uses Tree-of-Thought beam search for deep test planning.
type ToTPlanner struct {
	proposeDriver  *ai.Driver // strategy generation (SONNET tier)
	evaluateDriver *ai.Driver // scoring, non-generative (HAIKU tier)
	config         ToTConfig
	memory         string // cross-session episodic + semantic context prepended to propose prompts
	logger         *zap.Logger
}

// NewToTPlanner creates a ToT planner with separate drivers for the propose
// (generation) and evaluate (scoring) subtasks — the Phase 1 tier principle
// applied to ToT's two subtasks. evaluateDriver may equal proposeDriver when
// tiering is unavailable; both may be nil only in tests that never reach an
// LLM call.
func NewToTPlanner(proposeDriver, evaluateDriver *ai.Driver, config ToTConfig, logger *zap.Logger) *ToTPlanner {
	return &ToTPlanner{
		proposeDriver:  proposeDriver,
		evaluateDriver: evaluateDriver,
		config:         config,
		logger:         logger,
	}
}

// SetMemory injects cross-session episodic + semantic context (the output of
// Scout.buildEpisodicContext) to prepend to every propose prompt. Empty memory
// leaves prompts unchanged (no regression for direct/standalone runs). The
// evaluate step never sees memory — it stays a pure scoring step on the cheap
// HAIKU tier.
func (t *ToTPlanner) SetMemory(memory string) { t.memory = memory }

// buildProposeTask renders the propose prompt body, prepending cross-session
// memory when present so ToT mode composes with Reflexion instead of excluding
// it. Empty memory yields the bare task (no regression for standalone runs).
func (t *ToTPlanner) buildProposeTask(parent PlanCandidate, model *project.ProjectModel, goal string) string {
	modelSummary := formatModelForToT(model)
	memoryBlock := ""
	if t.memory != "" {
		memoryBlock = fmt.Sprintf("Prior-session memory (apply relevant lessons, avoid repeating past failures):\n%s\n\n", t.memory)
	}
	return fmt.Sprintf(`Propose %d different test strategies.
%sParent strategy: %s
Project Model:
%s

Test Goal: %s

Each strategy should focus on a different aspect (happy path, error handling, edge cases, security, etc.) and include concrete test case descriptions.`,
		t.config.GenerateN, memoryBlock, parent.Description, modelSummary, goal)
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
		scored, err := t.evaluate(ctx, expanded, model, goal)
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
