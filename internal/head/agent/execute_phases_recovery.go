package agent

import (
	"fmt"
	"time"

	"go.uber.org/zap"
)

// failureResult creates a failure result with the given error.
func (se *stepExecution) failureResult(err error, attempts int) StepResult {
	return StepResult{
		TestCase: se.tc,
		Status:   StepFailed,
		TraceID:  se.traceID,
		Attempts: attempts,
		Duration: time.Since(se.start),
		Error:    err,
	}
}

// tryRecovery attempts to recover from a failed action.
// Returns true if recovery was attempted (regardless of success).
func (se *stepExecution) tryRecovery(attempt int) bool {
	r := se.loop
	if attempt >= r.config.MaxSteerAttempts || se.recoverAttempts >= r.config.MaxRecoverAttempts {
		return false
	}

	se.recoverAttempts++
	recResult, recErr := r.recovery.Recover(se.ctx, *se.tc, se.lastResult, attempt)
	if recErr != nil {
		r.logger.Warn("recovery failed", zap.Error(recErr))
		return false
	}

	if recResult.Skip {
		se.recoverySkipped = true
		return true
	}

	if recResult.Action != nil {
		recExecResult := r.executor.Execute(se.ctx, recResult.Action)
		r.recordEvidence(se.ctx, se.traceID, "recovery", recResult.Action, recExecResult)
		se.lastResult = recExecResult
		se.lastAction = recResult.Action
		if recExecResult.Success() {
			if err := r.store.FinishTrace(se.ctx, se.traceID, string(StepPassed)); err != nil {
				r.logger.Error("finish trace", zap.Error(err))
			}
			// Note: we return from runReactLoop, not tryRecovery
			return false
		}
	}

	return false
}

// finalizeResult creates the final result after all attempts are exhausted.
func (se *stepExecution) finalizeResult() StepResult {
	status := StepFailed
	if se.recoverySkipped {
		status = StepSkipped
	}

	if err := se.loop.store.FinishTrace(se.ctx, se.traceID, string(status)); err != nil {
		se.loop.logger.Error("finish trace", zap.Error(err))
	}

	var evContent string
	if se.lastResult != nil {
		evContent = se.lastResult.Evidence().Content
	}

	// If any attempt hit an environmental failure (target unreachable), surface
	// it on the final result so the examiner classifies the whole case as
	// environmental — a recalled strategy cannot be judged against an
	// unreachable target, so it must not be penalized for this case's failure.
	var envErr error
	if se.environmentalSeen && se.tc != nil {
		envErr = fmt.Errorf("target unreachable: %s", se.tc.Target)
	}

	return StepResult{
		TestCase: se.tc,
		Status:   status,
		TraceID:  se.traceID,
		Attempts: se.loop.config.MaxSteerAttempts,
		Duration: time.Since(se.start),
		Action:   se.lastAction,
		Result:   se.lastResult,
		Error:    envErr,
		Evidence: []Evidence{{Type: evidenceType(se.lastResult), Content: evContent}},
	}
}
