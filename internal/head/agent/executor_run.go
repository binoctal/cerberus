package agent

import (
	"context"
	"fmt"

	"go.uber.org/zap"
)

// ExecutePlan runs all TestCases in the plan sequentially and returns results.
func (r *ReActLoop) ExecutePlan(ctx context.Context, plan *TestPlan, sessionID string) ([]StepResult, error) {
	defer r.processMgr.StopAll()

	var results []StepResult
	consecutiveFailures := 0
	remainingCases := 0
	for i := range plan.Cases {
		tc := &plan.Cases[i]
		remainingCases = len(plan.Cases) - i
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
		)
		r.emitProgress(ProgressEvent{Type: "case_complete", CaseID: tc.ID, Status: result.Status, Attempt: result.Attempts})

		// Escalation checkpoint: budget warning.
		if r.checkBudgetWarning(ctx, remainingCases, sessionID) {
			budget := r.driver.Budget()
			usedPct := float64(budget.SessionTotal-budget.Remaining()) / float64(budget.SessionTotal) * 100
			return results, fmt.Errorf("execution aborted: budget warning at %.0f%% usage", usedPct)
		}

		// Track consecutive failures for systemic failure escalation.
		if result.Status == StepFailed || result.Status == StepSkipped {
			consecutiveFailures++
		} else {
			consecutiveFailures = 0
		}
		if r.checkSystemicFailure(ctx, consecutiveFailures, sessionID) {
			return results, fmt.Errorf("execution aborted after %d consecutive failures", consecutiveFailures)
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
