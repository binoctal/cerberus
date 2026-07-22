package agent

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/store"
)

// newStepExecution builds a stepExecution wired to a real MultiExecutor (which
// dispatches to a WebSocketExecutor, the same executor Steer uses) and an
// in-memory store. It mirrors what executeStep does: sets caseIDKey on ctx and
// creates a trace. Used only by runSteps tests to exercise the deterministic
// multi-step path end-to-end against a live in-process WS server.
func newStepExecution(t *testing.T, tc *TestCase) *stepExecution {
	return newStepExecutionWithIdx(t, tc, nil)
}

// newStepExecutionWithIdx is like newStepExecution but wires a WSProtocolIndex
// into the executor so the role+handshake path is exercised through runSteps.
func newStepExecutionWithIdx(t *testing.T, tc *TestCase, wsIdx *WSProtocolIndex) *stepExecution {
	t.Helper()
	s, err := store.New(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	err = store.RunMigrations(context.Background(), s.DB(), "../../../migrations")
	require.NoError(t, err)

	executor := BuildMultiExecutor(".", nil, wsIdx, nil, zap.NewNop())
	loop := &ReActLoop{
		executor: executor,
		store:    s,
		logger:   zap.NewNop(),
	}

	// Create a real session row so CreateTrace's foreign key is satisfied.
	sess, err := s.CreateSession(context.Background(), "run", "test", "")
	require.NoError(t, err)

	// Carry the case identifier so the WS executor namespaces connection-table
	// keys by <caseID>:<connectionID> — exactly as executeStep does.
	ctx := context.WithValue(context.Background(), caseIDKey{}, tc.ID)
	traceID, err := s.CreateTrace(ctx, sess.ID, "agent", tc.Target)
	require.NoError(t, err)

	return &stepExecution{
		loop:    loop,
		ctx:     ctx,
		tc:      tc,
		traceID: traceID,
		start:   time.Now(),
	}
}

// closedPortURL returns a ws:// URL pointing at a TCP port that was just freed
// (nothing listens on it), so a connect attempt fails deterministically and
// fast rather than against a hard-coded port that might be in use.
func closedPortURL(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := l.Addr().String()
	require.NoError(t, l.Close())
	return "ws://" + addr + "/ws"
}

// TestRunSteps exercises the deterministic multi-step WS orchestrator
// (runSteps + stepToAction) end-to-end against an in-process coder/websocket
// server. Three sub-tests cover the pass path and both short-circuit paths.
func TestRunSteps(t *testing.T) {
	t.Run("pass", func(t *testing.T) {
		// The server accepts, reads the device:command, and replies with an
		// approved ack. connects proves all three steps reuse ONE connection
		// (the send/receive steps have no URL — they can only succeed if the
		// connect's shared connection is reused).
		var connects atomic.Int32
		url := newWSTestServer(t, func(conn *websocket.Conn) {
			connects.Add(1)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if _, _, err := conn.Read(ctx); err == nil {
				_ = conn.Write(ctx, websocket.MessageText,
					[]byte(`{"type":"device:ack","payload":{"approved":true}}`))
			}
			_, _, _ = conn.Read(ctx) // block until close
		})

		tc := &TestCase{
			ID:     "tc-pass",
			Target: url,
			Steps: []TestStep{
				{Action: "ws_connect", ConnectionID: "c1"},
				{Action: "ws_send", ConnectionID: "c1", Message: `{"type":"device:command"}`},
				{Action: "ws_receive", ConnectionID: "c1", Type: "device:ack",
					Asserts: map[string]any{"payload.approved": true}, Timeout: 2},
			},
		}

		se := newStepExecution(t, tc)
		result := se.runSteps()

		require.Equal(t, StepPassed, result.Status, "case should pass")
		require.Len(t, result.Evidence, 3, "one evidence entry per step")
		require.Equal(t, int32(1), connects.Load(),
			"all steps must reuse one connection (connects=%d)", connects.Load())
	})

	t.Run("connect_fail_short_circuits", func(t *testing.T) {
		tc := &TestCase{
			ID:     "tc-conn-fail",
			Target: closedPortURL(t),
			Steps: []TestStep{
				{Action: "ws_connect", ConnectionID: "c1"},
				{Action: "ws_send", ConnectionID: "c1", Message: `{"type":"device:command"}`},
				{Action: "ws_receive", ConnectionID: "c1", Type: "device:ack",
					Asserts: map[string]any{"payload.approved": true}, Timeout: 2},
			},
		}

		se := newStepExecution(t, tc)
		result := se.runSteps()

		require.Equal(t, StepFailed, result.Status, "connect failure should fail the case")
		require.Len(t, result.Evidence, 1,
			"only the connect step should be in evidence (send/receive never ran)")
		require.False(t, result.Result.Success(), "connect result should be a failure")
	})

	t.Run("assert_mismatch_short_circuits", func(t *testing.T) {
		// Server replies with approved:false — the type matches but the assert
		// fails. Connect and send succeed first, so evidence has 3 entries; the
		// failure is at the receive (assert) step.
		url := newWSTestServer(t, func(conn *websocket.Conn) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if _, _, err := conn.Read(ctx); err == nil {
				_ = conn.Write(ctx, websocket.MessageText,
					[]byte(`{"type":"device:ack","payload":{"approved":false}}`))
			}
			_, _, _ = conn.Read(ctx)
		})

		tc := &TestCase{
			ID:     "tc-assert-mismatch",
			Target: url,
			Steps: []TestStep{
				{Action: "ws_connect", ConnectionID: "c1"},
				{Action: "ws_send", ConnectionID: "c1", Message: `{"type":"device:command"}`},
				{Action: "ws_receive", ConnectionID: "c1", Type: "device:ack",
					Asserts: map[string]any{"payload.approved": true}, Timeout: 2},
			},
		}

		se := newStepExecution(t, tc)
		result := se.runSteps()

		require.Equal(t, StepFailed, result.Status,
			"assert mismatch should fail the case at the receive step")
		require.Len(t, result.Evidence, 3,
			"connect + send succeeded before the receive assert failed")
		require.False(t, result.Result.Success(), "receive result should be a failure")
	})
}

// TestRunStepsRoleProtocol covers the gap the opus whole-branch review flagged:
// TestRunSteps/pass wires the executor via BuildMultiExecutor(".", nil, …) (nil
// WS protocol index) and a connect step with NO Role, so runSteps was only
// exercised on the M0/M1 bare-connect path. These sub-tests drive runSteps
// through the real Role path — a declared protocol, a role with a discriminator
// param + mandatory handshake — exactly as Scout emits via wsStepsCase. They
// reuse the same in-process server + protocolIndexForURL patterns as the
// role-handshake tests in websocket_test.go.
func TestRunStepsRoleProtocol(t *testing.T) {
	t.Run("role_handshake_success", func(t *testing.T) {
		// Server accepts, sends the awaited handshake frame, reads the
		// client's command, and replies with an approved ack. connects proves
		// all three steps reuse ONE connection.
		var connects atomic.Int32
		wsURL := newWSTestServer(t, func(conn *websocket.Conn) {
			connects.Add(1)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			// Send the awaited handshake frame.
			_ = conn.Write(ctx, websocket.MessageText, []byte(`{"type":"devices:sync"}`))
			// Read the client's command, reply with an approved ack.
			if _, _, err := conn.Read(ctx); err == nil {
				_ = conn.Write(ctx, websocket.MessageText,
					[]byte(`{"type":"device:ack","payload":{"approved":true}}`))
			}
			_, _, _ = conn.Read(ctx) // block until close
		})

		// Protocol with json framing, a role "web" carrying a discriminator
		// param and a mandatory handshake — mirrors the auth-less role setup
		// of TestConnectRoleHandshakeSuccess so the role resolves trivially.
		p := &project.Protocol{TypePath: "type",
			Roles: map[string]*project.ProtocolRole{"web": {
				Params:    map[string]string{"type": "web"},
				Handshake: &project.RoleHandshake{AwaitType: "devices:sync", Timeout: 2},
			}}}
		wsIdx := protocolIndexForURL(t, wsURL, p)

		tc := &TestCase{
			ID:     "tc-role-hs",
			Target: wsURL,
			Steps: []TestStep{
				{Action: "ws_connect", Role: "web", ConnectionID: "c1"},
				{Action: "ws_send", ConnectionID: "c1", Message: `{"type":"device:command"}`},
				{Action: "ws_receive", ConnectionID: "c1", Type: "device:ack",
					Asserts: map[string]any{"payload.approved": true}, Timeout: 2},
			},
		}

		se := newStepExecutionWithIdx(t, tc, wsIdx)
		result := se.runSteps()

		require.Equal(t, StepPassed, result.Status, "role+handshake case should pass")
		require.Len(t, result.Evidence, 3, "one evidence entry per step")
		require.Equal(t, int32(1), connects.Load(),
			"all steps must reuse one connection (connects=%d)", connects.Load())
	})

	t.Run("handshake_timeout_short_circuits", func(t *testing.T) {
		// Server never sends the awaited handshake type — doConnect's handshake
		// loop times out, fails, and tears down the connection. runSteps must
		// short-circuit at the connect step: send/receive never run.
		wsURL := newWSTestServer(t, func(conn *websocket.Conn) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, _, _ = conn.Read(ctx) // never sends devices:sync
		})
		p := &project.Protocol{TypePath: "type",
			Roles: map[string]*project.ProtocolRole{"web": {
				Handshake: &project.RoleHandshake{AwaitType: "devices:sync", Timeout: 1},
			}}}
		wsIdx := protocolIndexForURL(t, wsURL, p)

		tc := &TestCase{
			ID:     "tc-hs-timeout",
			Target: wsURL,
			Steps: []TestStep{
				{Action: "ws_connect", Role: "web", ConnectionID: "c1"},
				{Action: "ws_send", ConnectionID: "c1", Message: `{"type":"device:command"}`},
				{Action: "ws_receive", ConnectionID: "c1", Type: "device:ack",
					Asserts: map[string]any{"payload.approved": true}, Timeout: 2},
			},
		}

		se := newStepExecutionWithIdx(t, tc, wsIdx)
		result := se.runSteps()

		require.Equal(t, StepFailed, result.Status,
			"handshake timeout should fail the case at the connect step")
		require.Len(t, result.Evidence, 1,
			"only the connect step should be in evidence (send/receive never ran)")
		require.False(t, result.Result.Success(), "connect result should be a failure")
	})
}
