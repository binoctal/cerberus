package agent

import (
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/types"
)

// runReactLoop executes the ReAct loop for the test case.
func (se *stepExecution) runReactLoop() StepResult {
	r := se.loop

	for attempt := 1; attempt <= r.config.MaxSteerAttempts; attempt++ {
		// Steer: AI decides next action.
		action, err := r.steer(se.ctx, se.tc, se.lastResult, attempt)
		if err != nil {
			r.logger.Warn("steer failed", zap.Int("attempt", attempt), zap.Error(err))
			if err.Error() == "token budget exhausted" {
				return StepResult{
					TestCase:  se.tc,
					Status:    StepSkipped,
					TraceID:   se.traceID,
					Attempts:  attempt,
					Duration:  time.Since(se.start),
					Error:     err,
				}
			}
			se.lastResult = types.ErrorResult{Err: err.Error()}
			se.lastAction = action
			continue
		}

		// Check for destructive action.
		if r.checkDestructiveRisk(se.ctx, action, se.sessionID) {
			return StepResult{
				TestCase:  se.tc,
				Status:    StepSkipped,
				TraceID:   se.traceID,
				Attempts:  attempt,
				Duration:  time.Since(se.start),
				Error:     fmt.Errorf("skipped destructive action: %s %s", action.GetActionType(), action.Target()),
			}
		}

		// Observe: Execute the action.
		result := r.executor.Execute(se.ctx, action)
		r.recordEvidence(se.ctx, se.traceID, "steer_attempt", action, result)
		se.lastResult = result
		se.lastAction = action

		// Check for target unreachable.
		if !result.Success() {
			se.consecutiveTimeouts++
		} else {
			se.consecutiveTimeouts = 0
		}
		if r.checkTargetUnreachable(se.ctx, se.tc.Target, se.consecutiveTimeouts, se.sessionID) {
			return StepResult{
				TestCase:  se.tc,
				Status:    StepFailed,
				TraceID:   se.traceID,
				Attempts:  attempt,
				Duration:  time.Since(se.start),
				Error:     fmt.Errorf("target unreachable: %s", se.tc.Target),
			}
		}

		r.logger.Info("steer attempt",
			zap.Int("attempt", attempt),
			zap.String("action_type", string(action.GetActionType())),
			zap.String("target", action.Target()),
			zap.Bool("success", result.Success()),
			zap.Duration("latency", result.Duration()),
		)

		if result.Success() {
			if err := r.store.FinishTrace(se.ctx, se.traceID, string(StepPassed)); err != nil {
				r.logger.Error("finish trace", zap.Error(err))
			}
			return StepResult{
				TestCase:  se.tc,
				Status:    StepPassed,
				TraceID:   se.traceID,
				Attempts:  attempt,
				Duration:  time.Since(se.start),
				Action:    action,
				Result:    result,
				Evidence:  []Evidence{{Type: evidenceType(result), Content: result.Evidence().Content}},
			}
		}

		// Attempt recovery before next steer.
		if recovered := se.tryRecovery(attempt); recovered {
			continue
		}
	}

	// All attempts exhausted or recovery skipped.
	return se.finalizeResult()
}
