package session

import (
	"time"

	"go.uber.org/zap"
)

// initialize prepares the session for resuming.
func (rp *resumePhase) initialize() error {
	rp.session.Logger.Info("resuming session", zap.String("id", rp.session.ID))
	// Resume skips Scout, so reload the persisted coverage contract (if any) to
	// allow the Examiner to assess coverage. Best-effort: never aborts resume.
	if c, err := rp.session.Store.LoadContract(rp.ctx, rp.session.ID); err != nil {
		rp.session.Logger.Warn("load contract for resume", zap.Error(err))
	} else if c != nil {
		rp.session.Contract = c
	}
	return nil
}

// finalize updates session stats and status after completion
func (rp *resumePhase) finalize() {
	// Tear down real-process actors first so children never outlive the run.
	rp.session.harnessStopAll()

	elapsed := time.Since(rp.startTime)
	tokensUsed := rp.session.Driver.Budget().SessionTotal - rp.session.Driver.Budget().Remaining()

	// Build summary if not yet built (e.g. on early error).
	if rp.summary == nil {
		rp.summary = &SessionSummary{
			Goal:        rp.session.Goal,
			TotalTokens: tokensUsed,
			Duration:    elapsed.Round(time.Millisecond).String(),
			DurationMs:  elapsed.Milliseconds(),
		}
	} else {
		rp.summary.TotalTokens = tokensUsed
		rp.summary.Duration = elapsed.Round(time.Millisecond).String()
		rp.summary.DurationMs = elapsed.Milliseconds()
	}

	// Write stats to store.
	if statsErr := rp.session.Store.UpdateSessionStats(rp.ctx, rp.session.ID, rp.summary.CoveragePct, rp.summary); statsErr != nil {
		rp.session.Logger.Error("update session stats", zap.Error(statsErr))
	}

	rp.session.Logger.Info("session summary", zap.String("summary", rp.summary.String()))

	// Update status (terminal).
	status := "completed"
	if rp.err != nil {
		status = "failed"
	}
	if statsErr := rp.session.Store.UpdateSessionStatus(rp.ctx, rp.session.ID, status); statsErr != nil {
		rp.session.Logger.Error("update session status", zap.Error(statsErr))
	}
}
