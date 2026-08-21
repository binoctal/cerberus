package session

import (
	"fmt"
	"slices"
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

	// Reconstruct the pre-interruption evidence BEFORE the completed cases are
	// dropped: claims reconciliation must see the session's complete evidence,
	// not just this resume's slice (spec: reconcileClaims also runs on resume).
	rp.prior = rp.completedCaseResults(completed)

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
		// The claims gate arbitrates this exit path too: reconcile against the
		// full pre-interruption evidence so an unproven critical claim marks
		// the session incomplete (exit 3), never a silent completed.
		reconcileClaimsInto(rp.summary, rp.session.Config, rp.prior)
		backflowFindings(rp.session.ProjectDir, rp.session.Config, rp.prior, nil, rp.session.ID, rp.session.Logger)
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

// completedCaseResults reconstructs StepResults for plan cases finished
// before the interruption. The saved plan supplies each TestCase (Claim
// bindings, step roles and bodies — everything the claims tier match needs);
// the persisted verdicts supply the final status per target (a target with
// any passing verdict counts as passed). Best-effort: on a store error it
// returns nil and reconciliation degrades to this resume's results only.
func (rp *resumePhase) completedCaseResults(completed map[string]bool) []agent.StepResult {
	if len(completed) == 0 {
		return nil
	}
	verdicts, err := rp.session.Store.GetVerdicts(rp.ctx, rp.session.ID)
	if err != nil {
		rp.session.Logger.Warn("load verdicts for claims reconciliation", zap.Error(err))
		return nil
	}
	passing := map[string]bool{}
	for _, v := range verdicts {
		if v.Status == string(agent.StepPassed) {
			passing[v.Target] = true
		}
	}
	var out []agent.StepResult
	for i := range rp.plan.Cases {
		tc := rp.plan.Cases[i]
		if !completed[tc.Target] {
			continue
		}
		status := agent.StepFailed
		if passing[tc.Target] {
			status = agent.StepPassed
		}
		out = append(out, agent.StepResult{TestCase: &tc, Status: status})
	}
	return out
}

// buildSummary constructs the session summary for resumed portion
func (rp *resumePhase) buildSummary() {
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

	// Include coverage contract and assessment if present
	rp.summary.Contract = rp.session.Contract
	rp.summary.Assessment = rp.session.Assessment

	// Fidelity composition watermark (real vs self-played actors).
	rp.summary.RealActors, rp.summary.AllEmulated = FidelityComposition(rp.session.Config)

	// Claims ledger reconciliation against the session's COMPLETE evidence:
	// pre-interruption cases (rp.prior) plus this resume's results; the gate
	// flag turns into ErrClaimsGate at the end of Session.Resume.
	reconcileClaimsInto(rp.summary, rp.session.Config, slices.Concat(rp.prior, rp.results))

	// Findings backflow over the same complete evidence.
	backflowFindings(rp.session.ProjectDir, rp.session.Config, slices.Concat(rp.prior, rp.results), rp.verdicts, rp.session.ID, rp.session.Logger)
}
