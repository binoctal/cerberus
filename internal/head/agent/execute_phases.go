package agent

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/types"
)

// stepExecution holds state for executing a single test step
type stepExecution struct {
	loop              *ReActLoop
	ctx               context.Context
	tc                *TestCase
	sessionID         string
	traceID           int64
	start             time.Time
	lastResult        types.ExecutorResult
	lastAction        types.TypedAction
	recoverySkipped   bool
	consecutiveTimeouts int
	recoverAttempts   int
}

// executeStep runs the ReAct loop for a single TestCase.
func (r *ReActLoop) executeStep(ctx context.Context, tc *TestCase, sessionID string) StepResult {
	se := &stepExecution{
		loop:              r,
		ctx:               ctx,
		tc:                tc,
		sessionID:         sessionID,
		start:             time.Now(),
		consecutiveTimeouts: 0,
		recoverAttempts:   0,
	}

	// Apply per-case timeout.
	if r.config.PerCaseTimeout > 0 {
		var cancel context.CancelFunc
		se.ctx, cancel = context.WithTimeout(se.ctx, r.config.PerCaseTimeout)
		defer cancel()
	}

	// Create a trace for this step.
	traceID, err := r.store.CreateTrace(se.ctx, sessionID, "agent", tc.Target)
	if err != nil {
		r.logger.Error("create trace", zap.Error(err))
		return se.failureResult(fmt.Errorf("create trace: %w", err), 0)
	}
	se.traceID = traceID
	defer func() {
		if err := r.store.FinishTrace(se.ctx, traceID, string(StepPassed)); err != nil {
			r.logger.Error("finish trace", zap.Error(err))
		}
	}()

	// Phase 1: Try rule engine (zero tokens).
	if result := se.tryRuleEngine(); result != nil {
		return *result
	}

	// Phase 2: ReAct loop (max MaxSteerAttempts).
	return se.runReactLoop()
}

// failureResult creates a failure result with the given error.
func (se *stepExecution) failureResult(err error, attempts int) StepResult {
	return StepResult{
		TestCase:  se.tc,
		Status:    StepFailed,
		TraceID:   se.traceID,
		Attempts:  attempts,
		Duration:  time.Since(se.start),
		Error:     err,
	}
}

// tryRuleEngine attempts to execute the test case using the rule engine.
// Returns a successful result if the rule engine handles the case, nil otherwise.
func (se *stepExecution) tryRuleEngine() *StepResult {
	r := se.loop
	action, matched := r.engine.Match(*se.tc)
	if !matched {
		return nil
	}

	// Handle background process: start it and return immediately.
	if se.tc.Background {
		if procAct, isProc := action.(types.ProcessExecAction); isProc {
			mp := &ManagedProcess{
				Name:    se.tc.ID,
				Cmd:     procAct.Command,
				Args:    procAct.Args,
				WorkDir: procAct.WorkDir,
				Health:  se.tc.WaitFor,
				Timeout: 30 * time.Second,
			}
			if err := r.processMgr.Start(se.ctx, mp); err != nil {
				result := StepResult{
					TestCase:  se.tc,
					Status:    StepFailed,
					TraceID:   se.traceID,
					Attempts:  1,
					Duration:  time.Since(se.start),
					Error:     err,
				}
				return &result
			}
			result := StepResult{
				TestCase:  se.tc,
				Status:    StepPassed,
				TraceID:   se.traceID,
				Attempts:  1,
				Duration:  time.Since(se.start),
				Action:    action,
				Evidence:  []Evidence{{Type: "background_process", Content: fmt.Sprintf("started %s", procAct.Command)}},
			}
			return &result
		}
	}

	// Check for destructive action.
	if r.checkDestructiveRisk(se.ctx, action, se.sessionID) {
		result := StepResult{
			TestCase:  se.tc,
			Status:    StepSkipped,
			TraceID:   se.traceID,
			Attempts:  0,
			Duration:  time.Since(se.start),
			Error:     fmt.Errorf("skipped destructive action: %s %s", action.GetActionType(), action.Target()),
		}
		return &result
	}

	result := r.executor.Execute(se.ctx, action)
	r.recordEvidence(se.ctx, se.traceID, "rule_engine", action, result)
	if result.Success() {
		stepResult := StepResult{
			TestCase:  se.tc,
			Status:    StepPassed,
			TraceID:   se.traceID,
			Attempts:  1,
			Duration:  time.Since(se.start),
			Action:    action,
			Result:    result,
			Evidence:  []Evidence{{Type: evidenceType(result), Content: result.Evidence().Content}},
		}
		return &stepResult
	}

	// Rule engine action failed — fall through to ReAct loop.
	r.logger.Info("rule engine action failed, entering ReAct loop",
		zap.String("target", se.tc.Target),
		zap.String("summary", result.Summary()),
	)
	return nil
}

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

	return StepResult{
		TestCase:  se.tc,
		Status:    status,
		TraceID:   se.traceID,
		Attempts:  se.loop.config.MaxSteerAttempts,
		Duration:  time.Since(se.start),
		Action:    se.lastAction,
		Result:    se.lastResult,
		Evidence:  []Evidence{{Type: evidenceType(se.lastResult), Content: evContent}},
	}
}
