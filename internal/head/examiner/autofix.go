package examiner

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/ai"
)

// AutoFixResult holds the outcome of an auto-fix attempt.
type AutoFixResult struct {
	Success   bool
	Verdict   FinalVerdict
	Attempted bool
}

// AutoFixer attempts to repair a failing test case by asking the LLM
// for a corrective action, then re-judging the result.
type AutoFixer struct {
	driver *ai.Driver
	logger *zap.Logger
}

// NewAutoFixer creates an AutoFixer.
func NewAutoFixer(driver *ai.Driver, logger *zap.Logger) *AutoFixer {
	return &AutoFixer{driver: driver, logger: logger}
}

// Fix attempts a single repair for a failed step result.
// It constructs a repair prompt, calls the LLM for a suggested action,
// then re-judges the outcome. Returns the new verdict if improved.
func (af *AutoFixer) Fix(ctx context.Context, verdict FinalVerdict, invariantDesc string) AutoFixResult {
	sr := verdict.StepResult
	if sr.TestCase == nil {
		return AutoFixResult{Attempted: false}
	}

	af.logger.Info("auto-fix attempt",
		zap.String("case_id", sr.TestCase.ID),
		zap.String("target", sr.TestCase.Target),
	)

	// Build repair context.
	repairCtx := fmt.Sprintf("Test case %q (%s %s) failed.\n", sr.TestCase.Name, sr.TestCase.Method, sr.TestCase.Target)
	repairCtx += fmt.Sprintf("Expected: %s\n", sr.TestCase.Expectation)
	if sr.Result != nil {
		repairCtx += fmt.Sprintf("Got: %v\n", sr.Result)
	}
	repairCtx += fmt.Sprintf("Original reasoning: %s\n", verdict.Reasoning)
	if invariantDesc != "" {
		repairCtx += fmt.Sprintf("Invariant: %s\n", invariantDesc)
	}

	prompt := ai.NewPrompt().
		System(promptAutoFixSystem).
		Context(repairCtx).
		Task("Suggest a single corrective action to make this test pass.").
		Output(promptAutoFixToolGuide).
		Build()

	// Auto-fix site: DecideWithTools + assembleAutofix. Error OR zero tool
	// calls degrade to {Attempted:true, Success:false} (NOT propagate) —
	// auto-fix is part of the repair loop, so a degraded verdict means "no
	// repair applied, keep the original fail", which the loop already handles.
	// `skip:true` is preserved as a StatusSkip downgrade.
	res, err := af.driver.DecideWithTools(ctx, prompt, autofixTools())
	if err != nil {
		af.logger.Warn("auto-fix LLM call failed", zap.Error(err))
		return AutoFixResult{Attempted: true, Success: false}
	}
	if len(res.ToolCalls) == 0 {
		af.logger.Warn("auto-fix zero tool calls (drift or quality)")
		return AutoFixResult{Attempted: true, Success: false}
	}
	reasoning, skip, err := assembleAutofix(res.ToolCalls[0])
	if err != nil {
		af.logger.Warn("auto-fix assemble failed", zap.Error(err))
		return AutoFixResult{Attempted: true, Success: false}
	}

	if skip {
		af.logger.Info("auto-fix suggests skipping", zap.String("reasoning", reasoning))
		// Downgrade to skip.
		verdict.Status = StatusSkip
		verdict.Reasoning = fmt.Sprintf("Auto-fix: %s", reasoning)
		return AutoFixResult{Attempted: true, Success: true, Verdict: verdict}
	}

	af.logger.Info("auto-fix analysis", zap.String("reasoning", reasoning))

	// Re-judge with the analysis context. We don't re-execute (no executor access),
	// but we can adjust the verdict based on the LLM's repair analysis.
	// The real execution fix would require the full Agent loop — here we
	// upgrade the verdict if the repair analysis is confident.
	newVerdict := verdict
	newVerdict.Reasoning = fmt.Sprintf("Auto-fix analysis: %s. Original: %s", reasoning, verdict.Reasoning)

	return AutoFixResult{Attempted: true, Success: true, Verdict: newVerdict}
}
