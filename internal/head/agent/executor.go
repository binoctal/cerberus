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
	"go.uber.org/zap"
)

// recoverer is the interface for the Recover decision point.
type recoverer interface {
	Recover(ctx context.Context, tc TestCase, obs Observation, attempt int) (RecoverResult, error)
}

// ReActLoop executes test steps using a Reason-Act-Observe cycle.
type ReActLoop struct {
	driver   *ai.Driver
	store    *store.Store
	engine   *RuleEngine
	executor ActionExecutor
	recovery recoverer
	config   ReActConfig
	logger   *zap.Logger
	gate     escalation.Gate
}

// NewReActLoopWithGate creates a ReAct execution loop with an explicit escalation gate.
func NewReActLoopWithGate(
	driver *ai.Driver,
	store *store.Store,
	engine *RuleEngine,
	executor ActionExecutor,
	config ReActConfig,
	gate escalation.Gate,
	logger *zap.Logger,
) *ReActLoop {
	return &ReActLoop{
		driver:   driver,
		store:    store,
		engine:   engine,
		executor: executor,
		recovery: NewRecovery(driver, store, config, logger),
		config:   config,
		gate:     gate,
		logger:   logger,
	}
}

// NewReActLoop creates a ReAct execution loop with a no-op escalation gate.
func NewReActLoop(
	driver *ai.Driver,
	store *store.Store,
	engine *RuleEngine,
	executor ActionExecutor,
	config ReActConfig,
	logger *zap.Logger,
) *ReActLoop {
	return NewReActLoopWithGate(driver, store, engine, executor, config, escalation.NoOpGate{}, logger)
}

// ExecutePlan runs all TestCases in the plan sequentially and returns results.
func (r *ReActLoop) ExecutePlan(ctx context.Context, plan *TestPlan, sessionID string) ([]StepResult, error) {
	var results []StepResult
	consecutiveFailures := 0
	for i := range plan.Cases {
		tc := &plan.Cases[i]
		r.logger.Info("executing test case",
			zap.String("case_id", tc.ID),
			zap.String("target", tc.Target),
		)
		result := r.executeStep(ctx, tc, sessionID)
		results = append(results, result)
		r.logger.Info("test case completed",
			zap.String("case_id", tc.ID),
			zap.String("status", string(result.Status)),
			zap.Int("attempts", result.Attempts),
		)

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
			// DecisionContinue — reset counter and proceed.
			consecutiveFailures = 0
		}
	}
	return results, nil
}

// executeStep runs the ReAct loop for a single TestCase.
func (r *ReActLoop) executeStep(ctx context.Context, tc *TestCase, sessionID string) StepResult {
	start := time.Now()

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
		_ = r.store.FinishTrace(ctx, traceID, string(StepPassed))
	}()

	// Phase 1: Try rule engine (zero tokens).
	if action, matched := r.engine.Match(*tc); matched {
		// Escalation checkpoint: destructive risk.
		if isDestructive(action) {
			decision := r.gate.Check(ctx, escalation.Event{
				Type:      "destructive_risk",
				Message:   fmt.Sprintf("destructive action detected: %s %s", action.Method, action.Target),
				SessionID: sessionID,
			})
			if decision.Action == escalation.DecisionSkipCase {
				return StepResult{
					TestCase: tc, Status: StepSkipped, TraceID: traceID,
					Attempts: 0, Duration: time.Since(start),
					Error: fmt.Errorf("skipped destructive action: %s %s", action.Method, action.Target),
				}
			}
		}
		obs := r.executor.Execute(ctx, action)
		r.recordEvidence(ctx, traceID, "rule_engine", action, obs)
		if obs.Success {
			return StepResult{
				TestCase: tc, Status: StepPassed, TraceID: traceID,
				Attempts: 1, Duration: time.Since(start), LastAction: action, LastObs: obs,
				Evidence: []Evidence{{Type: obsContentType(obs), Content: obs.Body}},
			}
		}
		// Rule engine action failed — fall through to ReAct loop.
		r.logger.Info("rule engine action failed, entering ReAct loop",
			zap.String("target", tc.Target),
			zap.Int("status_code", obs.StatusCode),
		)
	}

	// Phase 2: ReAct loop (max MaxSteerAttempts).
	var lastObs Observation
	var lastAction Action
	var recoverySkipped bool
	for attempt := 1; attempt <= r.config.MaxSteerAttempts; attempt++ {
		// Steer: AI decides next action.
		action, err := r.steer(ctx, tc, lastObs, attempt)
		if err != nil {
			r.logger.Warn("steer failed",
				zap.Int("attempt", attempt),
				zap.Error(err),
			)
			// Budget exhausted — skip immediately.
			if err.Error() == "token budget exhausted" {
				return StepResult{
					TestCase: tc, Status: StepSkipped, TraceID: traceID,
					Attempts: attempt, Duration: time.Since(start), Error: err,
				}
			}
			// Other errors count as a failed attempt.
			lastObs = Observation{Error: err.Error()}
			lastAction = action
			continue
		}

		// Observe: Execute the action.
		// Escalation checkpoint: destructive risk.
		if isDestructive(action) {
			decision := r.gate.Check(ctx, escalation.Event{
				Type:      "destructive_risk",
				Message:   fmt.Sprintf("destructive action detected: %s %s", action.Method, action.Target),
				SessionID: sessionID,
			})
			if decision.Action == escalation.DecisionSkipCase {
				return StepResult{
					TestCase: tc, Status: StepSkipped, TraceID: traceID,
					Attempts: attempt, Duration: time.Since(start),
					Error: fmt.Errorf("skipped destructive action: %s %s", action.Method, action.Target),
				}
			}
		}
		obs := r.executor.Execute(ctx, action)
		r.recordEvidence(ctx, traceID, "steer_attempt", action, obs)
		lastObs = obs
		lastAction = action

		r.logger.Info("steer attempt",
			zap.Int("attempt", attempt),
			zap.String("action_type", string(action.Type)),
			zap.String("target", action.Target),
			zap.Bool("success", obs.Success),
			zap.Duration("latency", obs.Duration),
		)

		if obs.Success {
			_ = r.store.FinishTrace(ctx, traceID, string(StepPassed))
			return StepResult{
				TestCase: tc, Status: StepPassed, TraceID: traceID,
				Attempts: attempt, Duration: time.Since(start),
				LastAction: action, LastObs: obs,
				Evidence: []Evidence{{Type: obsContentType(obs), Content: obs.Body}},
			}
		}

		// Attempt recovery before next steer.
		if attempt < r.config.MaxSteerAttempts {
			recResult, recErr := r.recovery.Recover(ctx, *tc, obs, attempt)
			if recErr != nil {
				r.logger.Warn("recovery failed", zap.Error(recErr))
			}
			if recResult.Skip {
				recoverySkipped = true
				break
			}
			if recResult.Action.Type != "" {
				recObs := r.executor.Execute(ctx, recResult.Action)
				r.recordEvidence(ctx, traceID, "recovery", recResult.Action, recObs)
				lastObs = recObs
				lastAction = recResult.Action
				if recObs.Success {
					_ = r.store.FinishTrace(ctx, traceID, string(StepPassed))
					return StepResult{
						TestCase: tc, Status: StepPassed, TraceID: traceID,
						Attempts: attempt + 1, Duration: time.Since(start),
						LastAction: recResult.Action, LastObs: recObs,
						Evidence: []Evidence{{Type: obsContentType(recObs), Content: recObs.Body}},
					}
				}
			}
		}
	}

	// All attempts exhausted or recovery skipped.
	status := StepFailed
	if lastObs.StatusCode == 0 || recoverySkipped {
		status = StepSkipped
	}
	_ = r.store.FinishTrace(ctx, traceID, string(status))

	return StepResult{
		TestCase: tc, Status: status, TraceID: traceID,
		Attempts: r.config.MaxSteerAttempts, Duration: time.Since(start),
		LastAction: lastAction, LastObs: lastObs,
		Evidence: []Evidence{{Type: obsContentType(lastObs), Content: lastObs.Body}},
	}
}

// steer calls the LLM to decide the next action.
func (r *ReActLoop) steer(ctx context.Context, tc *TestCase, prevObs Observation, attempt int) (Action, error) {
	observationCtx := formatObservationContext(tc, prevObs, attempt)

	prompt := ai.NewPrompt().
		System(promptSteerSystem).
		Context(observationCtx).
		Task(fmt.Sprintf("Test case: %s\nTarget: %s\nExpectation: %s\nAttempt: %d/%d",
			tc.Name, tc.Target, tc.Expectation, attempt, r.config.MaxSteerAttempts)).
		Output(promptSteerOutput).
		Build()

	var out SteerOutput
	if err := r.driver.Decide(ctx, prompt, &out); err != nil {
		// Try fallback parse on structured output failure.
		if isParseError(err) {
			r.logger.Warn("steer parse failed, using fallback", zap.Error(err))
			return FallbackParseAction(err.Error(), tc.Target), nil
		}
		return Action{}, fmt.Errorf("steer attempt %d: %w", attempt, err)
	}

	return out.Action, nil
}

// recordEvidence stores an observation as evidence linked to the trace.
func (r *ReActLoop) recordEvidence(ctx context.Context, traceID int64, phase string, action Action, obs Observation) {
	content, _ := json.Marshal(map[string]any{
		"phase":   phase,
		"action":  action,
		"success": obs.Success,
		"status":  obs.StatusCode,
		"error":   obs.Error,
	})
	_, err := r.store.CreateEvidence(ctx, traceID, "agent_observation", string(content))
	if err != nil {
		r.logger.Warn("record evidence", zap.Error(err))
	}
}

// formatObservationContext builds context for the Steer prompt.
func formatObservationContext(tc *TestCase, obs Observation, attempt int) string {
	if attempt == 1 {
		return fmt.Sprintf("Target: %s\nMethod: %s", tc.Target, tc.Method)
	}
	return fmt.Sprintf("Target: %s\nMethod: %s\nPrevious Status: %d\nPrevious Error: %s",
		tc.Target, tc.Method, obs.StatusCode, obs.Error)
}

func obsContentType(obs Observation) string {
	if obs.StatusCode > 0 {
		return "http_response"
	}
	if obs.Error != "" {
		return "error"
	}
	return "observation"
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

// isDestructive checks if an action is potentially destructive (DELETE/DROP methods or targets).
func isDestructive(action Action) bool {
	upper := strings.ToUpper(action.Method)
	if upper == "DELETE" || upper == "DROP" {
		return true
	}
	lowerTarget := strings.ToLower(action.Target)
	return strings.Contains(lowerTarget, "/delete") || strings.Contains(lowerTarget, "/drop")
}
