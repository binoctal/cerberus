package agent

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"
)

// executeStep runs the ReAct loop for a single TestCase.
func (r *ReActLoop) executeStep(ctx context.Context, tc *TestCase, sessionID string) StepResult {
	se := &stepExecution{
		loop:                r,
		ctx:                 ctx,
		tc:                  tc,
		sessionID:           sessionID,
		start:               time.Now(),
		consecutiveTimeouts: 0,
		recoverAttempts:     0,
		caseParams:          map[string]string{},
	}

	// Surface per-case timeouts as analyzable diagnostics rather than a silent
	// cancellation. A DeadlineExceeded here usually means a deadlock, a hang, or
	// a missing internal timeout in the executor — logging target + elapsed +
	// how far the ReAct loop got makes it findable instead of a mystery.
	defer func() {
		if se.ctx.Err() == context.DeadlineExceeded {
			r.logger.Warn("case exceeded per-case timeout (possible deadlock/hang)",
				zap.String("target", tc.Target),
				zap.Duration("elapsed", time.Since(se.start)),
				zap.Int("consecutive_timeouts", se.consecutiveTimeouts),
				zap.Int("recover_attempts", se.recoverAttempts))
		}
	}()

	// Apply per-case timeout. Stepped (deterministic) cases declare their own
	// time expectations via ws_receive windows — a mission-orchestration case
	// legitimately runs minutes (planner decompose, orchestrator alarms, ACP
	// connect timeout) — so the deadlock guard must not fire before the
	// declared windows do. For those cases the budget is the sum of the
	// receive windows plus setup/connect slack; the default applies to
	// LLM-steered and rule-engine cases.
	if r.config.PerCaseTimeout > 0 {
		timeout := r.config.PerCaseTimeout
		if declared := declaredReceiveBudget(tc); declared > 0 {
			timeout = declared + time.Minute
		}
		var cancel context.CancelFunc
		se.ctx, cancel = context.WithTimeout(se.ctx, timeout)
		defer cancel()
	}
	// Carry the case identifier so the WS executor can namespace connection-table
	// keys by <caseID>:<connectionID> — parallel cases passing the same
	// LLM-supplied connection_id otherwise collide on the shared table.
	se.ctx = context.WithValue(se.ctx, caseIDKey{}, tc.ID)

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

	// Phase 0: Deterministic multi-step WS case (no Steer LLM).
	if len(se.tc.Steps) > 0 {
		return se.runSteps()
	}

	// Phase 1: Try rule engine (zero tokens).
	if result := se.tryRuleEngine(); result != nil {
		return *result
	}

	// Phase 2: ReAct loop (max MaxSteerAttempts).
	return se.runReactLoop()
}

// declaredReceiveBudget sums the case's explicit ws_receive windows. Zero when
// the case declares none (guard falls back to the configured PerCaseTimeout).
func declaredReceiveBudget(tc *TestCase) time.Duration {
	var total time.Duration
	for _, s := range tc.Steps {
		if s.Action == "ws_receive" && s.Timeout > 0 {
			total += time.Duration(s.Timeout) * time.Second
		}
	}
	return total
}
