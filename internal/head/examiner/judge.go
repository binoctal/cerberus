package examiner

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/types"
)

// Judge performs Self-Refine evaluation: initial judgment (main model) +
// optional critique (critic model) for uncertain results.
type Judge struct {
	judgeDriver  *ai.Driver
	criticDriver *ai.Driver // nil means no Self-Refine
	config       ExaminerConfig
	critiqueUsed atomic.Int64 // session-level critique counter (touched by concurrent judges)
}

// NewJudge creates a Judge with main and optional critic drivers.
func NewJudge(judgeDriver, criticDriver *ai.Driver, config ExaminerConfig) *Judge {
	return &Judge{
		judgeDriver:  judgeDriver,
		criticDriver: criticDriver,
		config:       config,
	}
}

// Judge evaluates a step result against its expectation and returns a verdict.
func (j *Judge) Judge(ctx context.Context, result agent.StepResult) (*JudgeResult, error) {
	// Fast path: results with an objective success signal (process exit code,
	// HTTP status, positive expectation) are judged deterministically,
	// skipping the LLM for speed, cost, and reliability.
	if v, ok := objectiveVerdict(result, result.TestCase.Expectation); ok {
		return v, nil
	}
	// evidence/expectation are also needed by critique below; buildJudgePrompt
	// recomputes evidence itself (self-contained, testable), which is cheap
	// string work and keeps the prompt builder independently unit-testable.
	evidence := j.buildEvidenceContext(result)
	expectation := result.TestCase.Expectation
	prompt := j.buildJudgePrompt(result)

	// Judge site: DecideWithTools + assembleJudge. Error OR zero tool calls
	// surface as an error (NOT a silent verdict) so the caller (examiner.go)
	// maps a judge failure to fallbackVerdict — preserving graceful degrade.
	res, err := j.judgeDriver.DecideWithTools(ctx, prompt, judgeTools())
	if err != nil {
		return nil, fmt.Errorf("judge decide: %w", err)
	}
	if len(res.ToolCalls) == 0 {
		return nil, fmt.Errorf("judge decide: zero tool calls (drift or quality)")
	}
	judgeResult, err := assembleJudge(res.ToolCalls[0])
	if err != nil {
		return nil, fmt.Errorf("judge decide: %w", err)
	}

	// Phase 2: Early stop — high confidence, not uncertain.
	if j.isHighConfidence(&judgeResult) {
		return &judgeResult, nil
	}

	// Phase 3: Self-Refine critique if critic available.
	if j.criticDriver != nil {
		critiqued := j.critique(ctx, judgeResult, evidence, expectation)
		if critiqued != nil {
			return critiqued, nil
		}
	}

	return &judgeResult, nil
}

// buildJudgePrompt assembles the judge prompt. When VocabSummary is set it is
// prepended to the evidence so the judge anchors verdicts to the service's
// concrete legal message types and routing direction. Empty VocabSummary
// yields a byte-identical prompt (non-WS projects regress nothing). The
// critic deliberately does NOT receive vocab — it reviews verdict internal
// consistency, not protocol legality, and stays on the scoring tier.
func (j *Judge) buildJudgePrompt(result agent.StepResult) string {
	evidence := j.buildEvidenceContext(result)
	if len(j.dimensionsFor(result)) > 0 {
		evidence = dimensionGuidance + "\n" + evidence
	}
	if j.config.VocabSummary != "" {
		evidence = j.config.VocabSummary + "\n" + evidence
	}
	task := fmt.Sprintf("Evaluate this test evidence against expectations.\nExpectation: %s", result.TestCase.Expectation)
	return ai.NewPrompt().
		System(promptJudgeSystem).
		Context(evidence).
		Task(task).
		Output(promptJudgeToolGuide).
		Build()
}

// isHighConfidence checks if the result is confident enough to skip critique.
func (j *Judge) isHighConfidence(r *JudgeResult) bool {
	return r.CorrectnessConfidence >= j.config.ConfThreshold && r.Status != StatusUncertain
}

// critique runs the critic model to review the initial verdict.
func (j *Judge) critique(ctx context.Context, initial JudgeResult, evidence, expectation string) *JudgeResult {
	// Atomically claim one of the MaxCritiques slots (CAS loop) so concurrent
	// judges can't all pass the budget check and overrun it.
	for {
		cur := j.critiqueUsed.Load()
		if cur >= int64(j.config.MaxCritiques) {
			return nil
		}
		if j.critiqueUsed.CompareAndSwap(cur, cur+1) {
			break
		}
	}

	judgeJSON, _ := json.Marshal(initial)
	task := fmt.Sprintf("Review this initial verdict for errors.\nInitial verdict: %s\nEvidence: %s\nExpectation: %s",
		string(judgeJSON), evidence, expectation)

	prompt := ai.NewPrompt().
		System(promptCriticSystem).
		Task(task).
		Output(promptCriticToolGuide).
		Build()

	// Critic site: DecideWithTools + assembleCritique. Error OR zero tool calls
	// refund the reserved slot and return nil (keep the initial verdict) —
	// identical to the pre-migration Decide-error policy. SelfCritique and
	// CritiqueTriggered are code-set here, never LLM-emitted.
	res, err := j.criticDriver.DecideWithTools(ctx, prompt, criticTools())
	if err != nil {
		// Critic failed — refund the reserved slot and use the initial result.
		j.critiqueUsed.Add(-1)
		return nil
	}
	if len(res.ToolCalls) == 0 {
		// Drift: zero tool calls — refund the reserved slot, keep initial.
		j.critiqueUsed.Add(-1)
		return nil
	}
	critique, err := assembleCritique(res.ToolCalls[0])
	if err != nil {
		// Malformed call (should not happen post-schema) — refund, keep initial.
		j.critiqueUsed.Add(-1)
		return nil
	}

	if !critique.IssuesFound {
		return nil // No issues found — keep initial result.
	}

	// Apply critique corrections.
	result := initial
	result.Status = critique.SuggestedStatus
	result.CorrectnessConfidence = critique.SuggestedConfidence
	result.SelfCritique = critique.Critique
	result.CritiqueTriggered = true
	return &result
}

// buildEvidenceContext formats step result data for the Judge prompt.
func (j *Judge) buildEvidenceContext(r agent.StepResult) string {
	var b []byte
	b = append(b, fmt.Sprintf("Test Case: %s (%s)\n", r.TestCase.Name, r.TestCase.ID)...)
	b = append(b, fmt.Sprintf("Target: %s\n", r.TestCase.Target)...)
	b = append(b, fmt.Sprintf("Status: %s\n", r.Status)...)
	b = append(b, fmt.Sprintf("Attempts: %d\n", r.Attempts)...)

	if r.Result != nil {
		// Extract result-specific details. HTTP and WS carry bodies that
		// Summary() omits; surface them so content expectations are judgeable.
		switch res := r.Result.(type) {
		case types.HTTPResult:
			if res.StatusCode != 0 {
				b = append(b, fmt.Sprintf("HTTP Status: %d\n", res.StatusCode)...)
			}
			if res.Body != "" {
				body := res.Body
				if len(body) > 2000 {
					body = body[:2000] + "\n... (truncated)"
				}
				b = append(b, fmt.Sprintf("Response Body: %s\n", body)...)
			}
			if res.Err != "" {
				b = append(b, fmt.Sprintf("Error: %s\n", res.Err)...)
			}
		case types.WSResult:
			if res.MatchedMessage != "" {
				msg := res.MatchedMessage
				if len(msg) > 2000 {
					msg = msg[:2000] + "\n... (truncated)"
				}
				b = append(b, fmt.Sprintf("WS Matched Message: %s\n", msg)...)
			}
			// MatchAll receives split the burst into MatchedMessage (first) +
			// MatchedMessages (rest); surface the rest and the count so the
			// judge can verify "every item" contracts, not just the first item.
			if len(res.MatchedMessages) > 0 {
				b = append(b, fmt.Sprintf("WS Matched Items (%d total, first shown above):\n", res.MatchedCount)...)
				for i, m := range res.MatchedMessages {
					if i >= 10 { // cap noise from large batches
						b = append(b, fmt.Sprintf("... and %d more matched items\n", len(res.MatchedMessages)-i)...)
						break
					}
					if len(m) > 1000 {
						m = m[:1000] + "\n... (truncated)"
					}
					b = append(b, fmt.Sprintf("  - %s\n", m)...)
				}
			} else if res.MatchedCount > 0 {
				b = append(b, fmt.Sprintf("WS Matched Items: %d\n", res.MatchedCount)...)
			}
			for i, seen := range res.SeenMessages {
				if i >= 5 { // cap noise from heartbeats
					b = append(b, fmt.Sprintf("... and %d more seen messages\n", len(res.SeenMessages)-5)...)
					break
				}
				b = append(b, fmt.Sprintf("WS Seen: %s\n", seen)...)
			}
			if res.Err != "" {
				b = append(b, fmt.Sprintf("WS Error: %s\n", res.Err)...)
			}
		default:
			// Other result types: use summary and evidence.
			b = append(b, fmt.Sprintf("Result Summary: %s\n", r.Result.Summary())...)
		}
	}
	// Multi-step (Steps / ws_flow) cases carry their per-step trace in
	// Evidence; Result holds only the LAST step. Without surfacing Evidence,
	// the judge never sees the decisive step when it is not last — e.g. a relay
	// case whose decisive device:online receive is followed by ws_disconnect:
	// Result is the inert disconnect and the matched receive is invisible, so a
	// passing relay gets misjudged (judge drift / near-fail). Surface the trace
	// so the decisive receive is judgeable. (React-loop cases set no Evidence.)
	if len(r.Evidence) > 0 {
		b = append(b, "Step Trace:\n"...)
		for i, ev := range r.Evidence {
			content := ev.Content
			if len(content) > 500 {
				content = content[:500] + "... (truncated)"
			}
			b = append(b, fmt.Sprintf("  %d. %s\n", i+1, content)...)
		}
	}
	if r.Action != nil {
		envelope, _ := types.MarshalAction(r.Action)
		actionJSON, _ := json.Marshal(envelope)
		b = append(b, fmt.Sprintf("Last Action: %s\n", string(actionJSON))...)
	}
	if r.Error != nil {
		// A step-level error means the test harness itself failed (steer parse,
		// executor crash, etc.) — the target was never exercised. Flag it so the
		// judge does not mistake it for the system-under-test returning an error.
		b = append(b, fmt.Sprintf("Step Error (FRAMEWORK — the test harness failed to execute the target; this is NOT the system-under-test responding): %s\n", r.Error)...)
	}

	// Dimensions: merged from result-carried (source 1) and flow-derived
	// (source 2). Empty set ⇒ nothing rendered ⇒ byte-identical prompt.
	if d := renderDimensions(j.dimensionsFor(r)); d != "" {
		b = append(b, d...)
	}

	return string(b)
}
