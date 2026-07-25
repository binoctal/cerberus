package agent

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/types"
)

// steer calls the LLM (via DecideWithTools) to decide the next action.
//
// Returns the assembled TypedAction, a zeroCall flag signalling that the LLM
// emitted no action tool calls (drift), and any transient error. The zeroCall
// flag lets the ReAct loop distinguish a real WaitAction choice from the
// deterministic drift default so it can escalate consecutive drifts to
// StepSkipped (see runReactLoop). On a transient LLM error the loop's existing
// retry/token-budget handling applies unchanged.
func (r *ReActLoop) steer(ctx context.Context, tc *TestCase, prevResult types.ExecutorResult, attempt int) (types.TypedAction, bool, error) {
	observationCtx := formatResultContext(tc, prevResult, attempt)

	// Include service base URL in the prompt to guide the LLM to the correct host.
	base := ""
	if r.engine != nil {
		base = r.engine.baseURLFor(*tc)
	}
	taskExtra := ""
	if base != "" {
		taskExtra = fmt.Sprintf("\nService base URL: %s (use this host for api_request URLs)", base)
	}

	prompt := ai.NewPrompt().
		System(promptSteerSystem).
		Context(observationCtx).
		Task(fmt.Sprintf("Test case: %s\nTarget: %s\nExpectation: %s\nAttempt: %d/%d%s\nEmit one action tool call for the next step.",
			tc.Name, tc.Target, tc.Expectation, attempt, r.config.MaxSteerAttempts, taskExtra)).
		Build()

	res, err := r.driver.DecideWithTools(ctx, prompt, actionTools())
	if err != nil {
		return nil, false, fmt.Errorf("steer attempt %d: %w", attempt, err)
	}
	if len(res.ToolCalls) == 0 {
		r.logger.Warn("steer: zero action tool calls (drift)", zap.Int("attempt", attempt))
		return types.WaitAction{Duration: "1s"}, true, nil
	}
	action, aErr := assembleAction(res.ToolCalls[0])
	if aErr != nil {
		// Should not happen post-schema: the provider validates the tool call
		// against actionTools() before returning it. Treat the rare malformed
		// call as drift so the loop's consecutive-zero-call escalation still
		// fires rather than hard-failing the case on a recoverable glitch.
		r.logger.Warn("steer: action assemble failed, using wait default", zap.Error(aErr))
		return types.WaitAction{Duration: "1s"}, true, nil
	}
	return action, false, nil
}
