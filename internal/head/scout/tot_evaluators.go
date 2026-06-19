package scout

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/project"
)

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
	if err := t.evaluateDriver.Decide(ctx, prompt, &out); err != nil {
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
