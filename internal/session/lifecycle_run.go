package session

import (
	"context"
	"fmt"
	"time"
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

	// Phase 4: AutoTest — Coverage-driven test generation (optional)
	rp.executeAutoTestPhase()

	// Build summary
	rp.buildSummary(model)

	return nil
}
