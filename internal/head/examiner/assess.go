package examiner

import (
	"context"
	"fmt"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/head/contract"
)

func (e *Examiner) AssessCoverage(ctx context.Context, c *contract.Contract, results []agent.StepResult, coveragePct float64) (*contract.Assessment, error) {
	prompt := ai.NewPrompt().
		System(`You assess a test session against its coverage contract. Judge whether scope, path types, error scopes, boundaries, and invariants are covered. Use the objective coverage %. Report gaps concretely.`).
		Task(fmt.Sprintf("Contract: %+v\nCases run: %d\nObjective coverage of gated module: %.2f (gate: %.2f)", c, len(results), coveragePct, c.CoverageGate.LineThreshold)).
		Output(`Respond with JSON: {"reached":false,"gaps":[{"kind":"","detail":""}],"coverage_pct":0.0,"reasoning":""}`).
		Build()
	var a contract.Assessment
	if err := e.judge.judgeDriver.Decide(ctx, prompt, &a); err != nil {
		return nil, fmt.Errorf("assess coverage: %w", err)
	}
	// Objective gate override: below threshold → not reached regardless of LLM.
	if coveragePct < c.CoverageGate.LineThreshold {
		a.Reached = false
		a.Gaps = append(a.Gaps, contract.Gap{Kind: "coverage", Detail: fmt.Sprintf("%.0f%% < %.0f%% gate", coveragePct*100, c.CoverageGate.LineThreshold*100)})
	}
	a.CoveragePct = coveragePct
	return &a, nil
}
