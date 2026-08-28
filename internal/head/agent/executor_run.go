package agent

import (
	"context"
	"fmt"
	"strings"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/types"
)

// isDeprioritized reports whether a case was deprioritized by Scout validation
// (priority < 0) and should be skipped rather than executed. A zero priority
// is the struct zero value (an unset but legitimate case), not a signal.
func isDeprioritized(tc *TestCase) bool {
	return tc.Priority < 0
}

// isEnvironmental reports whether a failed StepResult is an environmental
// failure (target unreachable) rather than a logic/assertion failure. A lazy
// fallback must NOT be activated for environmental failures: if the target is
// unreachable, the fallback cannot succeed either. The ReAct loop builds the
// unreachable result via buildFailedResultForUnreachableTarget, which sets
// Error="target unreachable: ..." with a nil Result, so check both the Result
// (types.IsEnvironmentalFailure) and the Error string.
func isEnvironmental(r StepResult) bool {
	if r.Result != nil && types.IsEnvironmentalFailure(r.Result) {
		return true
	}
	if r.Error != nil && strings.Contains(strings.ToLower(r.Error.Error()), "target unreachable") {
		return true
	}
	return false
}

// ExecutePlan runs all TestCases in the plan sequentially and returns results.
func (r *ReActLoop) ExecutePlan(ctx context.Context, plan *TestPlan, sessionID string) ([]StepResult, error) {
	defer r.processMgr.StopAll()

	// Set sessionID on recovery for memory_usage attribution
	if r.recovery != nil {
		r.recovery.SetSessionID(sessionID)
	}

	var results []StepResult
	consecutiveFailures := 0
	remainingCases := 0
	// A1 Phase 2: index lazy fallback cases by primary ID. They are skipped in
	// the main loop and activated only when their primary case fails.
	fallbacksByPrimary := map[string][]*TestCase{}
	for i := range plan.Cases {
		if tc := &plan.Cases[i]; tc.FallbackFor != "" {
			fb := &plan.Cases[i]
			fallbacksByPrimary[fb.FallbackFor] = append(fallbacksByPrimary[fb.FallbackFor], fb)
		}
	}
	for i := range plan.Cases {
		tc := &plan.Cases[i]
		remainingCases = len(plan.Cases) - i
		if tc.FallbackFor != "" {
			// Lazy fallback: pre-scanned into fallbacksByPrimary; activated only
			// by its primary's failure below. Do not execute or record here.
			continue
		}
		if isDeprioritized(tc) {
			r.logger.Info("skipping deprioritized case",
				zap.String("case_id", tc.ID),
				zap.Float64("priority", tc.Priority),
			)
			r.emitProgress(ProgressEvent{Type: "case_start", CaseID: tc.ID})
			results = append(results, StepResult{TestCase: tc, Status: StepSkipped})
			r.emitProgress(ProgressEvent{Type: "case_complete", CaseID: tc.ID, Status: StepSkipped})
			continue
		}
		r.logger.Info("executing test case",
			zap.String("case_id", tc.ID),
			zap.String("target", tc.Target),
		)
		r.emitProgress(ProgressEvent{Type: "case_start", CaseID: tc.ID})
		result := r.executeStep(ctx, tc, sessionID)
		results = append(results, result)
		r.logger.Info("test case completed",
			zap.String("case_id", tc.ID),
			zap.String("status", string(result.Status)),
			zap.Int("attempts", result.Attempts),
			zap.NamedError("error", result.Error),
		)
		r.emitProgress(ProgressEvent{Type: "case_complete", CaseID: tc.ID, Status: result.Status, Attempt: result.Attempts})

		// Escalation checkpoint: budget warning.
		if r.checkBudgetWarning(ctx, remainingCases, sessionID) {
			budget := r.driver.Budget()
			usedPct := float64(budget.SessionTotal-budget.Remaining()) / float64(budget.SessionTotal) * 100
			return results, fmt.Errorf("execution aborted: budget warning at %.0f%% usage", usedPct)
		}

		// Track consecutive failures for systemic failure escalation. Skips
		// do not count: a skip is a decision not to assert (empty-list param
		// chains, deprioritized cases), not a failure signal — the http sweep
		// produces long runs of consecutive legitimate skips.
		if result.Status == StepFailed {
			consecutiveFailures++
		} else {
			consecutiveFailures = 0
		}
		if r.checkSystemicFailure(ctx, consecutiveFailures, sessionID) {
			return results, fmt.Errorf("execution aborted after %d consecutive failures", consecutiveFailures)
		}

		// A1 Phase 2: activate the primary's lazy fallback on a non-environmental
		// failure. The fallback runs the deterministic runSteps path (no LLM);
		// its result is excluded from consecutiveFailures and budget checks
		// above, which already ran for the primary. Recovered is set iff the
		// fallback itself passed; the primary's fail verdict is unchanged.
		if result.Status == StepFailed && !isEnvironmental(result) {
			for _, fb := range fallbacksByPrimary[tc.ID] {
				fbResult := r.executeStep(ctx, fb, sessionID)
				fbResult.Recovered = fbResult.Status == StepPassed
				results = append(results, fbResult)
				r.emitProgress(ProgressEvent{Type: "case_complete", CaseID: fb.ID, Status: fbResult.Status})
			}
		}
	}
	// Log rule engine hit rate for observability.
	if hits, misses := r.engine.Stats(); hits+misses > 0 {
		r.logger.Info("rule engine stats",
			zap.Int64("hits", hits),
			zap.Int64("misses", misses),
			zap.Float64("hit_rate", float64(hits)/float64(hits+misses)),
		)
	}

	r.emitProgress(ProgressEvent{Type: "plan_complete", Attempt: len(results)})
	return results, nil
}
