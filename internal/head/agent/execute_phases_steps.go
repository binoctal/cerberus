package agent

import (
	"fmt"
	"time"

	"github.com/binoctal/cerberus/internal/types"
)

// stepToAction converts a declarative TestStep into the typed WS action the
// shared executor already dispatches. Every step carries its own connection_id,
// so a case may address several connections. The connect step dials tc.Target;
// role drives protocol auth + handshake exactly as a Steer-emitted ws_connect.
func stepToAction(tc *TestCase, s TestStep) (types.TypedAction, error) {
	switch s.Action {
	case "ws_connect":
		return types.WSConnectAction{URL: tc.Target, Role: s.Role, ConnectionID: s.ConnectionID}, nil
	case "ws_send":
		return types.WSSendAction{ConnectionID: s.ConnectionID, Message: s.Message}, nil
	case "ws_receive":
		return types.WSReceiveAction{ConnectionID: s.ConnectionID, Type: s.Type,
			Aliases: s.Aliases, Assert: s.Asserts, Timeout: s.Timeout, Decisive: true}, nil
	case "ws_disconnect":
		return types.WSDisconnectAction{ConnectionID: s.ConnectionID}, nil
	default:
		return nil, fmt.Errorf("steps: unknown action %q", s.Action)
	}
}

// runSteps executes a deterministic multi-step WS case: each step runs via the
// shared executor under the case context (caseIDKey already set by executeStep).
// Steps citing the SAME connection_id share one connection; steps citing
// DIFFERENT connection_ids open distinct connections in the same case (the table
// is keyed <caseID>:<connectionID>), enabling multi-connection / cross-socket
// relay orchestration. The first failed step short-circuits the case. The
// decisive verdict is the final ws_receive assert; a completed chain is a real
// upgraded exchange for the Examiner.
func (se *stepExecution) runSteps() StepResult {
	r := se.loop
	var evidence []Evidence
	var lastAction types.TypedAction
	var lastResult types.ExecutorResult
	for _, s := range se.tc.Steps {
		action, err := stepToAction(se.tc, s)
		if err != nil {
			return se.failureResult(err, 1)
		}
		result := r.executor.Execute(se.ctx, action)
		r.recordEvidence(se.ctx, se.traceID, "steps", action, result)
		evidence = append(evidence, Evidence{Type: evidenceType(result), Content: fmt.Sprintf("%s: %s", s.Action, result.Summary())})
		lastAction, lastResult = action, result
		if !result.Success() {
			return StepResult{TestCase: se.tc, Status: StepFailed, TraceID: se.traceID,
				Attempts: 1, Duration: time.Since(se.start), Action: action, Result: result, Evidence: evidence}
		}
	}
	return StepResult{TestCase: se.tc, Status: StepPassed, TraceID: se.traceID,
		Attempts: 1, Duration: time.Since(se.start), Action: lastAction, Result: lastResult, Evidence: evidence}
}
