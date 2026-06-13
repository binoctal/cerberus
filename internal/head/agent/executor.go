package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/escalation"
	"github.com/binoctal/cerberus/internal/store"
	"github.com/binoctal/cerberus/internal/types"
	"go.uber.org/zap"
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

// NewReActLoopWithGate creates a ReAct execution loop with an explicit escalation gate.
func NewReActLoopWithGate(
	driver *ai.Driver,
	store *store.Store,
	engine *RuleEngine,
	executor TypedExecutor,
	config ReActConfig,
	gate escalation.Gate,
	logger *zap.Logger,
) *ReActLoop {
	return &ReActLoop{
		driver:     driver,
		store:      store,
		engine:     engine,
		executor:   executor,
		recovery:   NewRecovery(driver, store, config, logger),
		config:     config,
		gate:       gate,
		logger:     logger,
		processMgr: NewProcessManager(logger),
	}
}

// NewReActLoop creates a ReAct execution loop with a no-op escalation gate.
func NewReActLoop(
	driver *ai.Driver,
	store *store.Store,
	engine *RuleEngine,
	executor TypedExecutor,
	config ReActConfig,
	logger *zap.Logger,
) *ReActLoop {
	return NewReActLoopWithGate(driver, store, engine, executor, config, escalation.NoOpGate{}, logger)
}

// SetProgressChannel sets an optional channel for real-time progress events.
// Sends are non-blocking — events are dropped if the channel is full.
func (r *ReActLoop) SetProgressChannel(ch chan<- ProgressEvent) {
	r.progressCh = ch
}

// emitProgress sends a progress event non-blocking.
func (r *ReActLoop) emitProgress(event ProgressEvent) {
	if r.progressCh == nil {
		return
	}
	event.Timestamp = time.Now()
	select {
	case r.progressCh <- event:
	default:
	}
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
		if remainingCases > 5 {
			budget := r.driver.Budget()
			usedPct := float64(budget.SessionTotal-budget.Remaining()) / float64(budget.SessionTotal) * 100
			if usedPct >= 80 {
				decision := r.gate.Check(ctx, escalation.Event{
					Type:      "budget_warning",
					Message:   fmt.Sprintf("budget %.0f%% used, %d cases remaining", usedPct, remainingCases),
					SessionID: sessionID,
					Data:      map[string]any{"used_pct": usedPct, "remaining_cases": remainingCases},
				})
				if decision.Action == escalation.DecisionAbort {
					return results, fmt.Errorf("execution aborted: budget warning at %.0f%% usage", usedPct)
				}
			}
		}

		// Track consecutive failures for systemic failure escalation.
		if result.Status == StepFailed || result.Status == StepSkipped {
			consecutiveFailures++
		} else {
			consecutiveFailures = 0
		}
		if consecutiveFailures >= 5 {
			decision := r.gate.Check(ctx, escalation.Event{
				Type:      "systemic_failure",
				Message:   fmt.Sprintf("%d consecutive failures detected", consecutiveFailures),
				SessionID: sessionID,
			})
			if decision.Action == escalation.DecisionAbort {
				return results, fmt.Errorf("execution aborted after %d consecutive failures", consecutiveFailures)
			}
			consecutiveFailures = 0
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

// executeStep runs the ReAct loop for a single TestCase.
func (r *ReActLoop) executeStep(ctx context.Context, tc *TestCase, sessionID string) StepResult {
	start := time.Now()

	// Apply per-case timeout.
	if r.config.PerCaseTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.config.PerCaseTimeout)
		defer cancel()
	}

	// Create a trace for this step.
	traceID, err := r.store.CreateTrace(ctx, sessionID, "agent", tc.Target)
	if err != nil {
		r.logger.Error("create trace", zap.Error(err))
		return StepResult{
			TestCase: tc, Status: StepFailed, Error: fmt.Errorf("create trace: %w", err),
			Attempts: 0, Duration: time.Since(start),
		}
	}
	defer func() {
		if err := r.store.FinishTrace(ctx, traceID, string(StepPassed)); err != nil {
			r.logger.Error("finish trace", zap.Error(err))
		}
	}()

	// Phase 1: Try rule engine (zero tokens).
	if action, matched := r.engine.Match(*tc); matched {
		// Handle background process: start it and return immediately.
		if tc.Background {
			if procAct, isProc := action.(types.ProcessExecAction); isProc {
				mp := &ManagedProcess{
					Name:    tc.ID,
					Cmd:     procAct.Command,
					Args:    procAct.Args,
					WorkDir: procAct.WorkDir,
					Health:  tc.WaitFor,
					Timeout: 30 * time.Second,
				}
				if err := r.processMgr.Start(ctx, mp); err != nil {
					return StepResult{
						TestCase: tc, Status: StepFailed, TraceID: traceID,
						Attempts: 1, Duration: time.Since(start), Error: err,
					}
				}
				return StepResult{
					TestCase: tc, Status: StepPassed, TraceID: traceID,
					Attempts: 1, Duration: time.Since(start), Action: action,
					Evidence: []Evidence{{Type: "background_process", Content: fmt.Sprintf("started %s", procAct.Command)}},
				}
			}
		}

		// Escalation checkpoint: destructive risk.
		if isDestructiveAction(action) {
			decision := r.gate.Check(ctx, escalation.Event{
				Type:      "destructive_risk",
				Message:   fmt.Sprintf("destructive action detected: %s %s", action.GetActionType(), action.Target()),
				SessionID: sessionID,
			})
			if decision.Action == escalation.DecisionSkipCase {
				return StepResult{
					TestCase: tc, Status: StepSkipped, TraceID: traceID,
					Attempts: 0, Duration: time.Since(start),
					Error: fmt.Errorf("skipped destructive action: %s %s", action.GetActionType(), action.Target()),
				}
			}
		}
		result := r.executor.Execute(ctx, action)
		r.recordEvidence(ctx, traceID, "rule_engine", action, result)
		if result.Success() {
			return StepResult{
				TestCase: tc, Status: StepPassed, TraceID: traceID,
				Attempts: 1, Duration: time.Since(start), Action: action, Result: result,
				Evidence: []Evidence{{Type: evidenceType(result), Content: result.Evidence().Content}},
			}
		}
		// Rule engine action failed — fall through to ReAct loop.
		r.logger.Info("rule engine action failed, entering ReAct loop",
			zap.String("target", tc.Target),
			zap.String("summary", result.Summary()),
		)
	}

	// Phase 2: ReAct loop (max MaxSteerAttempts).
	var lastResult types.ExecutorResult
	var lastAction types.TypedAction
	var recoverySkipped bool
	consecutiveTimeouts := 0
	recoverAttempts := 0
	for attempt := 1; attempt <= r.config.MaxSteerAttempts; attempt++ {
		// Steer: AI decides next action.
		action, err := r.steer(ctx, tc, lastResult, attempt)
		if err != nil {
			r.logger.Warn("steer failed",
				zap.Int("attempt", attempt),
				zap.Error(err),
			)
			if err.Error() == "token budget exhausted" {
				return StepResult{
					TestCase: tc, Status: StepSkipped, TraceID: traceID,
					Attempts: attempt, Duration: time.Since(start), Error: err,
				}
			}
			lastResult = types.ErrorResult{Err: err.Error()}
			lastAction = action
			continue
		}

		// Observe: Execute the action.
		if isDestructiveAction(action) {
			decision := r.gate.Check(ctx, escalation.Event{
				Type:      "destructive_risk",
				Message:   fmt.Sprintf("destructive action detected: %s %s", action.GetActionType(), action.Target()),
				SessionID: sessionID,
			})
			if decision.Action == escalation.DecisionSkipCase {
				return StepResult{
					TestCase: tc, Status: StepSkipped, TraceID: traceID,
					Attempts: attempt, Duration: time.Since(start),
					Error: fmt.Errorf("skipped destructive action: %s %s", action.GetActionType(), action.Target()),
				}
			}
		}
		result := r.executor.Execute(ctx, action)
		r.recordEvidence(ctx, traceID, "steer_attempt", action, result)
		lastResult = result
		lastAction = action

		// Escalation checkpoint: target unreachable.
		if !result.Success() {
			consecutiveTimeouts++
		} else {
			consecutiveTimeouts = 0
		}
		if consecutiveTimeouts >= 3 {
			decision := r.gate.Check(ctx, escalation.Event{
				Type:      "target_unreachable",
				Message:   fmt.Sprintf("target %s unreachable after %d consecutive failures", tc.Target, consecutiveTimeouts),
				SessionID: sessionID,
				Data:      map[string]any{"target": tc.Target, "consecutive_failures": consecutiveTimeouts},
			})
			if decision.Action == escalation.DecisionAbort {
				return StepResult{
					TestCase: tc, Status: StepFailed, TraceID: traceID,
					Attempts: attempt, Duration: time.Since(start),
					Error: fmt.Errorf("target unreachable: %s", tc.Target),
				}
			}
			consecutiveTimeouts = 0
		}

		r.logger.Info("steer attempt",
			zap.Int("attempt", attempt),
			zap.String("action_type", string(action.GetActionType())),
			zap.String("target", action.Target()),
			zap.Bool("success", result.Success()),
			zap.Duration("latency", result.Duration()),
		)

		if result.Success() {
			if err := r.store.FinishTrace(ctx, traceID, string(StepPassed)); err != nil {
			r.logger.Error("finish trace", zap.Error(err))
		}
			return StepResult{
				TestCase: tc, Status: StepPassed, TraceID: traceID,
				Attempts: attempt, Duration: time.Since(start),
				Action: action, Result: result,
				Evidence: []Evidence{{Type: evidenceType(result), Content: result.Evidence().Content}},
			}
		}

		// Attempt recovery before next steer (capped by MaxRecoverAttempts).
		if attempt < r.config.MaxSteerAttempts && recoverAttempts < r.config.MaxRecoverAttempts {
			recoverAttempts++
			recResult, recErr := r.recovery.Recover(ctx, *tc, result, attempt)
			if recErr != nil {
				r.logger.Warn("recovery failed", zap.Error(recErr))
			}
			if recResult.Skip {
				recoverySkipped = true
				break
			}
			if recResult.Action != nil {
				recExecResult := r.executor.Execute(ctx, recResult.Action)
				r.recordEvidence(ctx, traceID, "recovery", recResult.Action, recExecResult)
				lastResult = recExecResult
				lastAction = recResult.Action
				if recExecResult.Success() {
					if err := r.store.FinishTrace(ctx, traceID, string(StepPassed)); err != nil {
			r.logger.Error("finish trace", zap.Error(err))
		}
					return StepResult{
						TestCase: tc, Status: StepPassed, TraceID: traceID,
						Attempts: attempt + 1, Duration: time.Since(start),
						Action: recResult.Action, Result: recExecResult,
						Evidence: []Evidence{{Type: evidenceType(recExecResult), Content: recExecResult.Evidence().Content}},
					}
				}
			}
		}
	}

	// All attempts exhausted or recovery skipped.
	status := StepFailed
	if recoverySkipped {
		status = StepSkipped
	}
	if err := r.store.FinishTrace(ctx, traceID, string(status)); err != nil {
		r.logger.Error("finish trace", zap.Error(err))
	}

	var evContent string
	if lastResult != nil {
		evContent = lastResult.Evidence().Content
	}
	return StepResult{
		TestCase: tc, Status: status, TraceID: traceID,
		Attempts: r.config.MaxSteerAttempts, Duration: time.Since(start),
		Action: lastAction, Result: lastResult,
		Evidence: []Evidence{{Type: evidenceType(lastResult), Content: evContent}},
	}
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
func isParseError(err error) bool {
	return err != nil && (contains(err.Error(), "parse output") || contains(err.Error(), "json"))
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
