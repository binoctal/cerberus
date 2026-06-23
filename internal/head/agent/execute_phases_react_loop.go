package agent

import (
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/types"
)

// runReactLoop executes the ReAct loop for the test case.
func (se *stepExecution) runReactLoop() StepResult {
	r := se.loop

	for attempt := 1; attempt <= r.config.MaxSteerAttempts; attempt++ {
		// Phase 1: Steer - AI decides next action
		action, err := r.steer(se.ctx, se.tc, se.lastResult, attempt)
		if err != nil {
			r.logger.Warn("steer failed", zap.Int("attempt", attempt), zap.Error(err))
			if checkTokenBudgetExhaustion(err) {
				return buildSkippedResultForTokenBudget(se.tc, se.traceID, attempt, se.start, err)
			}
			se.lastResult = types.ErrorResult{Err: err.Error()}
			se.lastAction = action
			continue
		}

		// Phase 2: Check for destructive action
		if r.checkDestructiveRisk(se.ctx, action, se.sessionID) {
			return buildSkippedResultForDestructiveAction(se.tc, se.traceID, attempt, se.start, action)
		}

		// Phase 3: Execute action and record
		newResult := executeAndRecordAction(r, se.ctx, action, se.traceID)
		se.lastResult = newResult
		se.lastAction = action
		if types.IsEnvironmentalFailure(newResult) {
			se.environmentalSeen = true
		}

		// Phase 4: Check for target unreachable
		se.consecutiveTimeouts = updateConsecutiveTimeouts(newResult, se.consecutiveTimeouts)
		if r.checkTargetUnreachable(se.ctx, se.tc.Target, se.consecutiveTimeouts, se.sessionID) {
			return buildFailedResultForUnreachableTarget(se.tc, se.traceID, attempt, se.start, se.tc.Target)
		}

		// Phase 5: Log attempt
		logSteerAttempt(r.logger, attempt, action, newResult)

		// Phase 6: Check for success. A pure duration wait only delays — it
		// verifies nothing about the target — so its success must not be judged
		// as a passing step; fall through to recovery / the next attempt.
		if newResult.Success() && !isNoopWait(action) {
			if err := r.store.FinishTrace(se.ctx, se.traceID, string(StepPassed)); err != nil {
				r.logger.Error("finish trace", zap.Error(err))
			}
			return buildPassedResult(se.tc, se.traceID, attempt, se.start, action, newResult)
		}

		// Phase 7: Attempt recovery before next steer
		if se.tryRecovery(attempt) {
			continue
		}
	}

	// All attempts exhausted or recovery skipped
	return se.finalizeResult()
}
