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
		action, zeroCall, err := r.steer(se.ctx, se.tc, se.lastResult, attempt)
		if err != nil {
			r.logger.Warn("steer failed", zap.Int("attempt", attempt), zap.Error(err))
			if checkTokenBudgetExhaustion(err) {
				return buildSkippedResultForTokenBudget(se.tc, se.traceID, attempt, se.start, err)
			}
			se.lastResult = types.ErrorResult{Err: err.Error()}
			se.lastAction = action
			continue
		}

		// Track consecutive zero-call (drift) steers. Two in a row ⇒ StepSkipped
		// so the Examiner can distinguish LLM drift from a real test failure
		// (spec §3). A single drift stays in the loop with the deterministic
		// WaitAction default and gets a retry.
		if zeroCall {
			se.consecutiveZeroSteer++
		} else {
			se.consecutiveZeroSteer = 0
		}
		if se.consecutiveZeroSteer >= driftSkipThreshold {
			r.logger.Warn("steer drift exhausted: skipping case",
				zap.Int("attempt", attempt),
				zap.Int("consecutive_zero_steers", se.consecutiveZeroSteer))
			return buildSkippedResultForDrift(se.tc, se.traceID, attempt, se.start)
		}

		// Phase 2: Check for destructive action
		if r.checkDestructiveRisk(se.ctx, action, se.sessionID) {
			return buildSkippedResultForDestructiveAction(se.tc, se.traceID, attempt, se.start, action)
		}

		// Phase 3: Execute action and record
		newResult := executeAndRecordAction(r, se.ctx, *se.tc, action, se.traceID)
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

		// Phase 6: Check for success. An intermediate step (pure wait,
		// WSConnect/WSSend/WSDisconnect, or non-decisive WSReceive) only
		// advances plumbing — its success verifies nothing about the target,
		// so it must not be judged as a passing step; fall through to Phase 7.
		if newResult.Success() && !isIntermediateStep(action) {
			if err := r.store.FinishTrace(se.ctx, se.traceID, string(StepPassed)); err != nil {
				r.logger.Error("finish trace", zap.Error(err))
			}
			return buildPassedResult(se.tc, se.traceID, attempt, se.start, action, newResult)
		}

		// Phase 7: Attempt recovery ONLY on actual failures. An intermediate
		// step that succeeded (connect/send/non-decisive receive) must proceed
		// to the next steer without burning a recovery LLM call or setting
		// recoverySkipped (which would mislabel a non-passing case as skipped
		// instead of failed). Spec D2.
		if !isIntermediateStep(action) || !newResult.Success() {
			if se.tryRecovery(attempt) {
				continue
			}
		}
	}

	// All attempts exhausted or recovery skipped
	return se.finalizeResult()
}
