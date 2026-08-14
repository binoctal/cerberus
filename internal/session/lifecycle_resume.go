package session

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"
)

// Resume loads a saved plan and continues from the first uncompleted test case.
// It skips Scout entirely, reuses the stored plan, and only executes remaining cases.
func (s *Session) Resume(ctx context.Context) (err error) {
	// Create resume phase with state
	rp := &resumePhase{
		session:   s,
		ctx:       ctx,
		startTime: time.Now(),
	}

	// Ensure finalization always runs
	defer rp.finalize()

	// Initialize
	if err := rp.initialize(); err != nil {
		rp.err = err
		return err
	}

	// Resolve dynamic auth (one login per actor with an auth: block) before
	// any remaining test case runs. Failures degrade; never abort.
	s.resolveActorAuth(ctx)

	// Real-process actors are relaunched on resume like any other run —
	// captured path params (deviceId etc.) come back with them.
	if err := s.launchRealProcessActors(ctx); err != nil {
		rp.err = err
		return err
	}

	// Load saved plan
	if err := rp.loadSavedPlan(); err != nil {
		rp.err = err
		return err
	}

	// Filter out completed cases
	if err := rp.filterRemainingCases(); err != nil {
		// If all cases completed, this is not an error
		if err.Error() == "all cases already completed" {
			return nil
		}
		rp.err = err
		return err
	}

	// Execute remaining cases
	if err := rp.executeRemainingCases(); err != nil {
		rp.err = fmt.Errorf("agent execute (resume): %w", err)
		return rp.err
	}

	// Examine results
	if err := rp.examineResults(); err != nil {
		rp.err = fmt.Errorf("examiner (resume): %w", err)
		return rp.err
	}

	// Consolidate phase — Write episodic memory (idempotent)
	if err := rp.executeConsolidatePhase(); err != nil {
		rp.session.Logger.Warn("consolidate phase failed (resume)", zap.Error(err))
	}

	// Build summary
	rp.buildSummary()

	return nil
}
