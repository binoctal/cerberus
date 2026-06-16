package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/escalation"
	"github.com/binoctal/cerberus/internal/store"
	"github.com/binoctal/cerberus/internal/types"
)

// recoverer is the interface for the Recover decision point.
type recoverer interface {
	Recover(ctx context.Context, tc TestCase, result types.ExecutorResult, attempt int) (RecoverDecision, error)
}

// RecoverDecision holds the recovery decision output.
type RecoverDecision struct {
	Action types.TypedAction
	Skip   bool
}

// ReActLoop executes test steps using a Reason-Act-Observe cycle.
type ReActLoop struct {
	driver     *ai.Driver
	store      *store.Store
	engine     *RuleEngine
	executor   TypedExecutor
	recovery   recoverer
	config     ReActConfig
	logger     *zap.Logger
	gate       escalation.Gate
	processMgr *ProcessManager
	progressCh chan<- ProgressEvent
}

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

// steer calls the LLM to decide the next action.
func (r *ReActLoop) steer(ctx context.Context, tc *TestCase, prevResult types.ExecutorResult, attempt int) (types.TypedAction, error) {
	observationCtx := formatResultContext(tc, prevResult, attempt)

	prompt := ai.NewPrompt().
		System(promptSteerSystem).
		Context(observationCtx).
		Task(fmt.Sprintf("Test case: %s\nTarget: %s\nExpectation: %s\nAttempt: %d/%d",
			tc.Name, tc.Target, tc.Expectation, attempt, r.config.MaxSteerAttempts)).
		Output(promptSteerOutput).
		Build()

	var out SteerOutput
	if err := r.driver.Decide(ctx, prompt, &out); err != nil {
		if isParseError(err) {
			r.logger.Warn("steer parse failed, using fallback", zap.Error(err))
			return FallbackParseAction(err.Error(), tc.Target), nil
		}
		return nil, fmt.Errorf("steer attempt %d: %w", attempt, err)
	}

	action, err := types.UnmarshalAction(out.Envelope)
	if err != nil {
		return nil, fmt.Errorf("unmarshal steer action: %w", err)
	}
	return action, nil
}

// recordEvidence stores an observation as evidence linked to the trace.
func (r *ReActLoop) recordEvidence(ctx context.Context, traceID int64, phase string, action types.TypedAction, result types.ExecutorResult) {
	content, _ := json.Marshal(map[string]any{
		"phase":    phase,
		"type":     string(action.GetActionType()),
		"target":   action.Target(),
		"success":  result.Success(),
		"summary":  result.Summary(),
		"evidence": result.Evidence(),
	})
	_, err := r.store.CreateEvidence(ctx, traceID, "agent_observation", string(content))
	if err != nil {
		r.logger.Warn("record evidence", zap.Error(err))
	}
}

// formatResultContext builds context for the Steer prompt.
func formatResultContext(tc *TestCase, result types.ExecutorResult, attempt int) string {
	if attempt == 1 {
		return fmt.Sprintf("Target: %s\nMethod: %s", tc.Target, tc.Method)
	}
	if result != nil {
		return fmt.Sprintf("Target: %s\nMethod: %s\nPrevious: %s",
			tc.Target, tc.Method, result.Summary())
	}
	return fmt.Sprintf("Target: %s\nMethod: %s", tc.Target, tc.Method)
}

func evidenceType(result types.ExecutorResult) string {
	if result == nil {
		return "none"
	}
	return result.Evidence().Type
}

// isParseError checks if the error is from structured output parsing.
// It looks for common patterns that indicate JSON/structured parsing failures.
func isParseError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	// Check for structured output parsing errors from AI driver
	if contains(msg, "parse output") {
		return true
	}
	// Check for JSON unmarshaling errors (these are wrapped in "parse output" errors,
	// but we check directly as a safety measure)
	if contains(msg, "unmarshal") || contains(msg, "invalid character") || contains(msg, "unexpected end") {
		return true
	}
	// Check for JSON syntax/format errors (standalone or with error)
	if contains(msg, "json") && (contains(msg, "syntax") || contains(msg, "error") || contains(msg, "format") || contains(msg, "invalid")) {
		return true
	}
	return false
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		(len(s) > 0 && findSubstr(s, sub)))
}

func findSubstr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// isDestructiveAction checks if a TypedAction is potentially destructive.
func isDestructiveAction(action types.TypedAction) bool {
	if action == nil {
		return false
	}
	switch a := action.(type) {
	case types.HTTPAction:
		upper := strings.ToUpper(a.Method)
		return upper == "DELETE" || upper == "DROP"
	case types.ProcessExecAction:
		destructive := []string{"rm", "rmdir", "dropdb", "truncate"}
		for _, d := range destructive {
			if a.Command == d {
				return true
			}
		}
	case types.FileWriteAction:
		return true
	}
	return false
}
