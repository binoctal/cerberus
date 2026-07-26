package examiner

import (
	"context"
	"fmt"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/head/contract"
)

// AssessCoverage judges whether a test session met its coverage contract.
// m is the objective coverage measurement for the Agent's tests. Its Pct is a
// 0–1 fraction compared against Gate.LineThreshold; Unit is "line" or
// "function"; Known is false when coverage could not be measured, in which case
// the objective gate is skipped and the LLM's verdict stands.
func (e *Examiner) AssessCoverage(ctx context.Context, c *contract.Contract, results []agent.StepResult, m contract.CoverageMeasurement) (*contract.Assessment, error) {
	prompt := ai.NewPrompt().
		System(`You assess a test session against its coverage contract. Judge whether scope, path types, error scopes, boundaries, and invariants are covered. Use the objective coverage %. Report gaps concretely.`).
		Task(fmt.Sprintf("Contract: %+v\nCases run: %d\nObjective coverage of gated module: %.2f (unit: %s, gate: %.2f)", c, len(results), m.Pct, m.Unit, c.CoverageGate.LineThreshold)).
		Output(promptAssessToolGuide).
		Build()

	// Assess site: DecideWithTools + assembleAssessment. Error OR zero tool
	// calls PROPAGATE as "assess coverage: ..." — unlike judge/critic/autofix/
	// learner (which degrade gracefully), assess feeds the contract gate, so
	// drift must surface as an error, not look like "not reached".
	res, err := e.judge.judgeDriver.DecideWithTools(ctx, prompt, assessTools())
	if err != nil {
		return nil, fmt.Errorf("assess coverage: %w", err)
	}
	if len(res.ToolCalls) == 0 {
		return nil, fmt.Errorf("assess coverage: zero tool calls (drift or quality)")
	}
	a, err := assembleAssessment(res.ToolCalls[0])
	if err != nil {
		return nil, fmt.Errorf("assess coverage: %w", err)
	}

	if !m.Known {
		// Unmeasured: do NOT bias the verdict. Leave Reached and Gaps to the LLM.
		a.CoveragePct = 0
		return a, nil
	}

	// Objective gate: below threshold → not reached regardless of the LLM.
	if m.Pct < c.CoverageGate.LineThreshold {
		a.Reached = false
		detail := fmt.Sprintf("%.0f%% < %.0f%% gate", m.Pct*100, c.CoverageGate.LineThreshold*100)
		if m.Unit != "line" {
			detail += fmt.Sprintf(" (measured as %s coverage)", m.Unit)
		}
		a.Gaps = append(a.Gaps, contract.Gap{Kind: "coverage", Detail: detail})
	}
	// The objective measurement always overrides the model's estimate.
	a.CoveragePct = m.Pct
	return a, nil
}
