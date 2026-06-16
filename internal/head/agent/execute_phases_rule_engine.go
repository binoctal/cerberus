package agent

import (
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/types"
)

// tryRuleEngine attempts to execute the test case using the rule engine.
// Returns a successful result if the rule engine handles the case, nil otherwise.
func (se *stepExecution) tryRuleEngine() *StepResult {
	r := se.loop
	action, matched := r.engine.Match(*se.tc)
	if !matched {
		return nil
	}

	// Handle background process: start it and return immediately.
	if se.tc.Background {
		if procAct, isProc := action.(types.ProcessExecAction); isProc {
			mp := &ManagedProcess{
				Name:    se.tc.ID,
				Cmd:     procAct.Command,
				Args:    procAct.Args,
				WorkDir: procAct.WorkDir,
				Health:  se.tc.WaitFor,
				Timeout: 30 * time.Second,
			}
			if err := r.processMgr.Start(se.ctx, mp); err != nil {
				result := StepResult{
					TestCase:  se.tc,
					Status:    StepFailed,
					TraceID:   se.traceID,
					Attempts:  1,
					Duration:  time.Since(se.start),
					Error:     err,
				}
				return &result
			}
			result := StepResult{
				TestCase:  se.tc,
				Status:    StepPassed,
				TraceID:   se.traceID,
				Attempts:  1,
				Duration:  time.Since(se.start),
				Action:    action,
				Evidence:  []Evidence{{Type: "background_process", Content: fmt.Sprintf("started %s", procAct.Command)}},
			}
			return &result
		}
	}

	// Check for destructive action.
	if r.checkDestructiveRisk(se.ctx, action, se.sessionID) {
		result := StepResult{
			TestCase:  se.tc,
			Status:    StepSkipped,
			TraceID:   se.traceID,
			Attempts:  0,
			Duration:  time.Since(se.start),
			Error:     fmt.Errorf("skipped destructive action: %s %s", action.GetActionType(), action.Target()),
		}
		return &result
	}

	result := r.executor.Execute(se.ctx, action)
	r.recordEvidence(se.ctx, se.traceID, "rule_engine", action, result)
	if result.Success() {
		stepResult := StepResult{
			TestCase:  se.tc,
			Status:    StepPassed,
			TraceID:   se.traceID,
			Attempts:  1,
			Duration:  time.Since(se.start),
			Action:    action,
			Result:    result,
			Evidence:  []Evidence{{Type: evidenceType(result), Content: result.Evidence().Content}},
		}
		return &stepResult
	}

	// Rule engine action failed — fall through to ReAct loop.
	r.logger.Info("rule engine action failed, entering ReAct loop",
		zap.String("target", se.tc.Target),
		zap.String("summary", result.Summary()),
	)
	return nil
}
