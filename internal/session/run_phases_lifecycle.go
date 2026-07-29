package session

import (
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/project"
)

// healthWarning returns a non-empty warning when the session shows signs of a
// broken accounting pipeline (activity but zero tokens recorded) — a
// regression guard for the per-head-budget-sharing fix.
func healthWarning(tokensUsed int, elapsed time.Duration) string {
	if tokensUsed == 0 && elapsed > 5*time.Second {
		return "no token usage recorded despite session activity — possible budget accounting issue (per-head drivers must share the session budget)"
	}
	return ""
}

// initialize prepares the session for running
func (rp *runPhase) initialize() error {
	rp.session.Logger.Info("session starting", zap.String("id", rp.session.ID))
	return nil
}

// finalize updates session stats and status after completion
func (rp *runPhase) finalize() {
	elapsed := time.Since(rp.startTime)
	tokensUsed := rp.session.Driver.Budget().SessionTotal - rp.session.Driver.Budget().Remaining()
	if w := healthWarning(tokensUsed, elapsed); w != "" {
		rp.session.Logger.Warn(w)
	}

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

	// Print human-readable summary.
	rp.session.Logger.Info("session summary", zap.String("summary", rp.summary.String()))
	fmt.Println(rp.summary.String())

	// Update status (terminal).
	status := "completed"
	if rp.err != nil {
		status = "failed"
	}
	if updateErr := rp.session.Store.UpdateSessionStatus(rp.ctx, rp.session.ID, status); updateErr != nil {
		rp.session.Logger.Error("update session status", zap.Error(updateErr))
	}
}

// buildSummary constructs the session summary
func (rp *runPhase) buildSummary(model *project.ProjectModel) {
	if rp.plan == nil {
		return
	}

	rp.summary = FromResults(
		rp.session.Goal,
		rp.session.resolveBaseURL(),
		plannedCaseCount(rp.plan),
		rp.results,
		rp.verdicts,
		rp.reflections,
		0, // tokens filled in finalize
		time.Since(rp.startTime),
	)

	if model != nil {
		rp.summary.EndpointsFound = len(model.API.Endpoints)
	}

	// Include coverage contract and assessment if present
	rp.summary.Contract = rp.session.Contract
	rp.summary.Assessment = rp.session.Assessment
}
