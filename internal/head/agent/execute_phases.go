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
		loop:               r,
		ctx:                ctx,
		tc:                 tc,
		sessionID:          sessionID,
		start:              time.Now(),
		consecutiveTimeouts: 0,
		recoverAttempts:    0,
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
