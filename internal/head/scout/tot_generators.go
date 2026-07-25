package scout

import (
	"context"
	"fmt"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
)

// propose generates N candidate strategies from a parent candidate. Each
// propose_strategy tool call becomes one PlanCandidate; multiple calls = N
// strategies (preserves the legacy "strategies array" behavior). The provider
// schema enforces description+cases, replacing the legacy ProposeOutput JSON.
func (t *ToTPlanner) propose(ctx context.Context, parent PlanCandidate, model *project.ProjectModel, goal string) ([]PlanCandidate, error) {
	task := t.buildProposeTask(parent, model, goal)

	prompt := ai.NewPrompt().
		System(`You are a test strategy planner. Generate diverse, high-quality test strategies.
Emit one propose_strategy tool call per strategy.`).
		Task(task).
		Build()

	res, err := t.proposeDriver.DecideWithTools(ctx, prompt, proposeTools())
	if err != nil {
		return nil, fmt.Errorf("tot propose: %w", err)
	}

	var candidates []PlanCandidate
	for _, call := range res.ToolCalls {
		if call.Name != "propose_strategy" {
			continue
		}
		candidates = append(candidates, PlanCandidate{
			Description: llm.StrField(call, "description"),
			Cases:       llm.StrSliceField(call, "cases"),
		})
	}
	return candidates, nil
}
