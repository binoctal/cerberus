package agent

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// newStepExecutionObs is newStepExecutionWithIdx plus an observed logger so
// tests can assert the per-step info lines runSteps emits.
func newStepExecutionObs(t *testing.T, tc *TestCase) (*stepExecution, *observer.ObservedLogs) {
	t.Helper()
	core, logs := observer.New(zap.InfoLevel)
	se := newStepExecutionWithIdxLogger(t, tc, nil, zap.New(core))
	return se, logs
}

// stepLogEntry returns the observed "case step" entry for the given 1-based
// step position, failing the test when it is missing.
func stepLogEntry(t *testing.T, logs *observer.ObservedLogs, step int) *observer.LoggedEntry {
	t.Helper()
	for _, e := range logs.FilterMessage("case step").All() {
		if v, ok := e.ContextMap()["step"].(int64); ok && int(v) == step {
			return &e
		}
	}
	t.Fatalf("no case step log entry for step %d", step)
	return nil
}

// TestRunStepsStepLogging pins the per-step info line (open-agents #23
// observability): every executed step logs its position, action, and outcome,
// and pre-execution failures (step resolution) are visible too — historically
// those early-returns produced zero evidence rows and no log at all.
func TestRunStepsStepLogging(t *testing.T) {
	t.Run("pass_logs_every_step", func(t *testing.T) {
		url := newWSTestServer(t, func(conn *websocket.Conn) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if _, _, err := conn.Read(ctx); err == nil {
				_ = conn.Write(ctx, websocket.MessageText,
					[]byte(`{"type":"device:ack","payload":{"approved":true}}`))
			}
			_, _, _ = conn.Read(ctx) // block until close
		})
		tc := &TestCase{
			ID:     "tc-log-pass",
			Target: url,
			Steps: []TestStep{
				{Action: "ws_connect", ConnectionID: "c1"},
				{Action: "ws_send", ConnectionID: "c1", Message: `{"type":"device:command"}`},
				{Action: "ws_receive", ConnectionID: "c1", Type: "device:ack",
					Asserts: map[string]any{"payload.approved": true}, Timeout: 2},
			},
		}

		se, logs := newStepExecutionObs(t, tc)
		result := se.runSteps()

		require.Equal(t, StepPassed, result.Status)
		entries := logs.FilterMessage("case step").All()
		require.Len(t, entries, 3, "one info line per step")

		first := stepLogEntry(t, logs, 1).ContextMap()
		require.Equal(t, tc.ID, first["case_id"])
		require.Equal(t, "ws_connect", first["action"])
		require.Equal(t, "c1", first["connection_id"])
		require.Equal(t, true, first["passed"])

		second := stepLogEntry(t, logs, 2).ContextMap()
		require.Equal(t, "ws_send", second["action"])

		third := stepLogEntry(t, logs, 3).ContextMap()
		require.Equal(t, "ws_receive", third["action"])
		require.Equal(t, "device:ack", third["type"])
	})

	t.Run("executor_failure_logs_step_and_short_circuit", func(t *testing.T) {
		tc := &TestCase{
			ID:     "tc-log-conn-fail",
			Target: closedPortURL(t),
			Steps: []TestStep{
				{Action: "ws_connect", ConnectionID: "c1"},
				{Action: "ws_send", ConnectionID: "c1", Message: `{"type":"device:command"}`},
			},
		}

		se, logs := newStepExecutionObs(t, tc)
		result := se.runSteps()

		require.Equal(t, StepFailed, result.Status)
		entries := logs.FilterMessage("case step").All()
		require.Len(t, entries, 1, "the failed connect logs exactly one line; the send never runs")
		require.Equal(t, false, entries[0].ContextMap()["passed"])
		require.NotEmpty(t, entries[0].ContextMap()["summary"], "failure summary must be visible")
	})

	t.Run("resolution_failure_logs_before_any_evidence", func(t *testing.T) {
		// Unknown action fails in stepToAction BEFORE the executor runs — the
		// historical blind spot: no evidence row, no log, case just "completed".
		tc := &TestCase{
			ID:     "tc-log-resolve-fail",
			Target: "ws://127.0.0.1:1/unused",
			Steps:  []TestStep{{Action: "bogus_action", ConnectionID: "c1"}},
		}

		se, logs := newStepExecutionObs(t, tc)
		result := se.runSteps()

		require.Equal(t, StepFailed, result.Status)
		require.Empty(t, result.Evidence, "resolution fails before any step executes")
		entries := logs.FilterMessage("case step").All()
		require.Len(t, entries, 1, "the resolution failure must be logged")
		ctx := entries[0].ContextMap()
		require.Equal(t, false, ctx["passed"])
		require.Contains(t, ctx["error"], "unknown action")
	})

	t.Run("http_gate_failure_logs_gate_error", func(t *testing.T) {
		// A 2xx-gated request whose status mismatches fails at the GATE, not the
		// executor — the log line must carry the gate reason.
		srv := newHTTPTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		})
		tc := &TestCase{
			ID:     "tc-log-gate-fail",
			Target: srv.URL,
			Steps: []TestStep{{
				Action: "http_request", Method: "GET", URL: srv.URL + "/missing",
				ExpectStatus: 200,
			}},
		}

		se, logs := newStepExecutionObs(t, tc)
		result := se.runSteps()

		require.Equal(t, StepFailed, result.Status)
		entries := logs.FilterMessage("case step").All()
		require.Len(t, entries, 1)
		ctx := entries[0].ContextMap()
		require.Equal(t, false, ctx["passed"])
		require.Equal(t, int64(404), ctx["status_code"])
	})
}
