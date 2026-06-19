package session

import (
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/head/agent"
)

// loadSavedPlan loads the test plan from the database
func (rp *resumePhase) loadSavedPlan() error {
	var plan agent.TestPlan
	if err := rp.session.Store.LoadPlan(rp.ctx, rp.session.ID, &plan); err != nil {
		return fmt.Errorf("load plan for session %s: %w", rp.session.ID, err)
	}

	if len(plan.Cases) == 0 {
		return fmt.Errorf("saved plan has no test cases")
	}

	rp.plan = &plan
	return nil
}

// filterRemainingCases filters out completed test cases from the plan
func (rp *resumePhase) filterRemainingCases() error {
	// Get completed targets.
	completed, err := rp.session.Store.GetCompletedTargets(rp.ctx, rp.session.ID)
	if err != nil {
		return fmt.Errorf("get completed targets: %w", err)
	}

	// Filter out completed cases.
	var remaining []agent.TestCase
	for _, tc := range rp.plan.Cases {
		if !completed[tc.Target] {
			remaining = append(remaining, tc)
		}
	}

	rp.session.Logger.Info("resuming from saved plan",
		zap.Int("total_cases", len(rp.plan.Cases)),
		zap.Int("completed", len(completed)),
		zap.Int("remaining", len(remaining)),
	)

	if len(remaining) == 0 {
		rp.session.Logger.Info("all cases already completed")
		// Build minimal summary for already completed session
		rp.summary = &SessionSummary{
			Goal:       rp.session.Goal,
			Duration:   time.Since(rp.startTime).String(),
			DurationMs: time.Since(rp.startTime).Milliseconds(),
		}
		return fmt.Errorf("all cases already completed")
	}

	// Build a reduced plan with only remaining cases.
	resumePlan := &agent.TestPlan{
		Goal:       rp.plan.Goal,
		Cases:      remaining,
		ProjectURL: rp.plan.ProjectURL,
	}
	rp.plan = resumePlan

	return nil
}

// buildSummary constructs the session summary for resumed portion
func (rp *resumePhase) buildSummary() {
	if rp.plan == nil {
		return
	}

	rp.summary = FromResults(
		rp.session.Goal,
		rp.session.resolveBaseURL(),
		len(rp.plan.Cases),
		rp.results,
		rp.verdicts,
		rp.reflections,
		0, // tokens filled in finalize
		time.Since(rp.startTime),
	)

	// Include coverage contract and assessment if present
	rp.summary.Contract = rp.session.Contract
	rp.summary.Assessment = rp.session.Assessment
}
