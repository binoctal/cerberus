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
// 0–1 fraction compared against Gate.LineThreshold (Unit "line"/"function") or
// Gate.PathThreshold (Unit "path"); Known is false when coverage could not be
// measured, in which case AssessCoverage returns NOT APPLICABLE
// ({Measured:false}) without calling the LLM — the session outcome is
// verdict-based. The "path" unit is also objective: required message edges must
// be exercised, and AssessCoverage never consults the LLM for it.
func (e *Examiner) AssessCoverage(ctx context.Context, c *contract.Contract, results []agent.StepResult, m contract.CoverageMeasurement) (*contract.Assessment, error) {
	if !m.Known {
		// Unmeasured (SaaS/WS session with no local SUT module, or provider
		// failure): coverage is NOT APPLICABLE. Do NOT call the LLM — it would
		// hallucinate a 0% / not-reached verdict from the absence of data. The
		// session outcome is verdict-based; Reached/CoveragePct stay meaningless.
		return &contract.Assessment{Measured: false}, nil
	}
	if m.Unit == "path" {
		// Objective path gate (no LLM): required message edges must be
		// exercised. The caller (session.pathCoverage) computes Pct; this branch
		// is purely objective. Below PathThreshold ⇒ not reached + a headline
		// path gap. Per-edge gaps are attached by the session layer, which owns
		// the required-edge list.
		a := &contract.Assessment{}
		a.Measured = true
		a.CoveragePct = m.Pct
		a.Reached = m.Pct >= c.CoverageGate.PathThreshold
		if !a.Reached {
			a.Gaps = append(a.Gaps, contract.Gap{
				Kind:   "path",
				Detail: fmt.Sprintf("%.0f%% of required message edges exercised < %.0f%% gate", m.Pct*100, c.CoverageGate.PathThreshold*100),
			})
		}
		return a, nil
	}
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
	a.Measured = true
	return a, nil
}
