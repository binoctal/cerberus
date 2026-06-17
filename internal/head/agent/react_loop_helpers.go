package agent

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/types"
)

// checkTokenBudgetExhaustion checks if steer failed due to token budget
func checkTokenBudgetExhaustion(err error) bool {
	return err != nil && err.Error() == "token budget exhausted"
}

// buildSkippedResultForTokenBudget creates a skipped result for token budget exhaustion
func buildSkippedResultForTokenBudget(tc *TestCase, traceID int64, attempt int, start time.Time, err error) StepResult {
	return StepResult{
		TestCase:  tc,
		Status:    StepSkipped,
		TraceID:   traceID,
		Attempts:  attempt,
		Duration:  time.Since(start),
		Error:     err,
	}
}

// buildSkippedResultForDestructiveAction creates a skipped result for destructive actions
func buildSkippedResultForDestructiveAction(tc *TestCase, traceID int64, attempt int, start time.Time, action types.TypedAction) StepResult {
	return StepResult{
		TestCase:  tc,
		Status:    StepSkipped,
		TraceID:   traceID,
		Attempts:  attempt,
		Duration:  time.Since(start),
		Error:     fmt.Errorf("skipped destructive action: %s %s", action.GetActionType(), action.Target()),
	}
}

// buildFailedResultForUnreachableTarget creates a failed result for unreachable targets
func buildFailedResultForUnreachableTarget(tc *TestCase, traceID int64, attempt int, start time.Time, target string) StepResult {
	return StepResult{
		TestCase:  tc,
		Status:    StepFailed,
		TraceID:   traceID,
		Attempts:  attempt,
		Duration:  time.Since(start),
		Error:     fmt.Errorf("target unreachable: %s", target),
	}
}

// buildPassedResult creates a successful result
func buildPassedResult(tc *TestCase, traceID int64, attempt int, start time.Time, action types.TypedAction, result types.ExecutorResult) StepResult {
	return StepResult{
		TestCase:  tc,
		Status:    StepPassed,
		TraceID:   traceID,
		Attempts:  attempt,
		Duration:  time.Since(start),
		Action:    action,
		Result:    result,
		Evidence:  []Evidence{{Type: evidenceType(result), Content: result.Evidence().Content}},
	}
}

// executeAndRecordAction executes the action and records evidence
func executeAndRecordAction(r *ReActLoop, ctx context.Context, action types.TypedAction, traceID int64) types.ExecutorResult {
	result := r.executor.Execute(ctx, action)
	r.recordEvidence(ctx, traceID, "steer_attempt", action, result)
	return result
}

// updateConsecutiveTimeouts updates the consecutive timeout counter
func updateConsecutiveTimeouts(result types.ExecutorResult, currentCount int) int {
	if !result.Success() {
		return currentCount + 1
	}
	return 0
}

// logSteerAttempt logs the steer attempt details
func logSteerAttempt(logger *zap.Logger, attempt int, action types.TypedAction, result types.ExecutorResult) {
	logger.Info("steer attempt",
		zap.Int("attempt", attempt),
		zap.String("action_type", string(action.GetActionType())),
		zap.String("target", action.Target()),
		zap.Bool("success", result.Success()),
		zap.Duration("latency", result.Duration()),
	)
}
