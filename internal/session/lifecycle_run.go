package session

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"
)

// Run executes the full three-head workflow: Scout → Agent → Examiner → AutoTest.
func (s *Session) Run(ctx context.Context) (err error) {
	runStart := time.Now()

	// Create run phase with state
	rp := &runPhase{
		session:   s,
		ctx:       ctx,
		startTime: runStart,
	}

	// Ensure finalization always runs
	defer rp.finalize()

	// Initialize
	if err := rp.initialize(); err != nil {
		rp.err = err
		return err
	}

	// Resolve dynamic auth (one login per actor with an auth: block) before
	// any test case runs. Failures degrade; never abort.
	s.resolveActorAuth(ctx)

	// Launch real-process actors (fidelity: real-process). Unlike auth, a
	// launch failure aborts: every case routing to a dead real actor would
	// fail misleadingly. Teardown runs in rp.finalize().
	if err := s.launchRealProcessActors(ctx); err != nil {
		rp.err = err
		return err
	}

	// Phase 1: Scout — Analyze + Plan
	model, err := rp.executeScoutPhase()
	if err != nil {
		rp.err = err
		return err
	}

	// Phase 2: Agent — Execute
	if err := rp.executeAgentPhase(); err != nil {
		rp.err = fmt.Errorf("agent execute: %w", err)
		return rp.err
	}

	// Phase 3: Examiner — Judge + Learn
	if err := rp.executeExaminerPhase(); err != nil {
		rp.err = fmt.Errorf("examiner: %w", err)
		return rp.err
	}

	// Phase 3.1: Repair loop — Examiner->Scout targeted replanning (feature #3).
	if err := rp.executeRepairLoop(); err != nil {
		rp.session.Logger.Warn("repair loop failed", zap.Error(err))
	}

	// Phase 3.5: Consolidate — Write episodic memory (idempotent)
	if err := rp.executeConsolidatePhase(); err != nil {
		rp.session.Logger.Warn("consolidate phase failed", zap.Error(err))
	}

	// Phase 4: AutoTest — Coverage-driven test generation (optional)
	rp.executeAutoTestPhase()

	// Build summary
	rp.buildSummary(model)

	// Claims gate: an unproven critical claim makes the session incomplete
	// even when execution succeeded (cerberus run exits 3, not 1). Assigned
	// before the deferred finalize so the summary persists with the gate flag
	// and the terminal status becomes "incomplete".
	rp.err = gateErrorIfFailed(rp.summary)
	return rp.err
}
