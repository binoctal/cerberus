package agent

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/types"
)

// steer calls the LLM to decide the next action.
func (r *ReActLoop) steer(ctx context.Context, tc *TestCase, prevResult types.ExecutorResult, attempt int) (types.TypedAction, error) {
	observationCtx := formatResultContext(tc, prevResult, attempt)

	prompt := ai.NewPrompt().
		System(promptSteerSystem).
		Context(observationCtx).
		Task(fmt.Sprintf("Test case: %s\nTarget: %s\nExpectation: %s\nAttempt: %d/%d",
			tc.Name, tc.Target, tc.Expectation, attempt, r.config.MaxSteerAttempts)).
		Output(promptSteerOutput).
		Build()

	var out SteerOutput
	if err := r.driver.Decide(ctx, prompt, &out); err != nil {
		if isParseError(err) {
			r.logger.Warn("steer parse failed, using fallback", zap.Error(err))
			return FallbackParseAction(err.Error(), tc.Target), nil
		}
		return nil, fmt.Errorf("steer attempt %d: %w", attempt, err)
	}

	action, err := types.UnmarshalAction(out.Envelope)
	if err != nil {
		// A malformed/empty action payload is an LLM output defect, not a
		// terminal failure. Fall back to a safe default action so the case
		// proceeds (mirrors the driver-layer parse fallback above). Without
		// this, a schema mismatch with non-Claude models wastes every
		// MaxSteerAttempts retry and fails the whole case.
		if isParseError(err) {
			r.logger.Warn("steer action parse failed, using fallback", zap.Error(err))
			return FallbackParseAction(err.Error(), tc.Target), nil
		}
		return nil, fmt.Errorf("unmarshal steer action: %w", err)
	}
	return action, nil
}
