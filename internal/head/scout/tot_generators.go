package scout

import (
	"context"
	"fmt"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/project"
)

// propose generates N candidate strategies from a parent candidate.
func (t *ToTPlanner) propose(ctx context.Context, parent PlanCandidate, model *project.ProjectModel, goal string) ([]PlanCandidate, error) {
	task := t.buildProposeTask(parent, model, goal)

	prompt := ai.NewPrompt().
		System(`You are a test strategy planner. Generate diverse, high-quality test strategies.
Output JSON with a "strategies" array. Each strategy has "description" and "cases" (array of test case descriptions).`).
		Task(task).
		Output(`{"strategies": [{"description": "...", "cases": ["test case 1", "test case 2"]}]}`).
		Build()

	var out ProposeOutput
	if err := t.proposeDriver.Decide(ctx, prompt, &out); err != nil {
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
