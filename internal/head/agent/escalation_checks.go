package agent

import (
	"context"
	"fmt"

	"github.com/binoctal/cerberus/internal/escalation"
	"github.com/binoctal/cerberus/internal/types"
)

// checkDestructiveRisk checks if an action is destructive and escalates if needed.
// Returns true if the case should be skipped.
func (r *ReActLoop) checkDestructiveRisk(ctx context.Context, action types.TypedAction, sessionID string) bool {
	if !isDestructiveAction(action) {
		return false
	}

	decision := r.gate.Check(ctx, escalation.Event{
		Type:      "destructive_risk",
		Message:   fmt.Sprintf("destructive action detected: %s %s", action.GetActionType(), action.Target()),
		SessionID: sessionID,
	})

	return decision.Action == escalation.DecisionSkipCase
}

// checkBudgetWarning checks if budget is running low and escalates if needed.
// Returns true if execution should be aborted.
func (r *ReActLoop) checkBudgetWarning(ctx context.Context, remainingCases int, sessionID string) bool {
	if remainingCases <= 5 {
		return false
	}

	budget := r.driver.Budget()
	usedPct := float64(budget.SessionTotal-budget.Remaining()) / float64(budget.SessionTotal) * 100
	if usedPct < 80 {
		return false
	}

	decision := r.gate.Check(ctx, escalation.Event{
		Type:      "budget_warning",
		Message:   fmt.Sprintf("budget %.0f%% used, %d cases remaining", usedPct, remainingCases),
		SessionID: sessionID,
		Data:      map[string]any{"used_pct": usedPct, "remaining_cases": remainingCases},
	})

	return decision.Action == escalation.DecisionAbort
}

// checkSystemicFailure checks for consecutive failures and escalates if needed.
// Returns true if execution should be aborted.
func (r *ReActLoop) checkSystemicFailure(ctx context.Context, consecutiveFailures int, sessionID string) bool {
	if consecutiveFailures < 5 {
		return false
	}

	decision := r.gate.Check(ctx, escalation.Event{
		Type:      "systemic_failure",
		Message:   fmt.Sprintf("%d consecutive failures detected", consecutiveFailures),
		SessionID: sessionID,
	})

	return decision.Action == escalation.DecisionAbort
}

// checkTargetUnreachable checks for consecutive timeouts and escalates if needed.
// Returns true if execution should be aborted.
func (r *ReActLoop) checkTargetUnreachable(ctx context.Context, target string, consecutiveTimeouts int, sessionID string) bool {
	if consecutiveTimeouts < 3 {
		return false
	}

	decision := r.gate.Check(ctx, escalation.Event{
		Type:      "target_unreachable",
		Message:   fmt.Sprintf("target %s unreachable after %d consecutive failures", target, consecutiveTimeouts),
		SessionID: sessionID,
		Data:      map[string]any{"target": target, "consecutive_failures": consecutiveTimeouts},
	})

	return decision.Action == escalation.DecisionAbort
}
