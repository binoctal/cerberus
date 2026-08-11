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
	"github.com/binoctal/cerberus/internal/types"
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
		wsIdx:    wsIdx, // wire http_request resolution (runSteps reads loop.wsIdx)
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

// TestRunStepsMultiConnection proves a single Steps case can orchestrate TWO
// connections (web + bridge) and relay frames across them. The in-process relay
// server forwards web<->bridge frames (modeling a broker/DO). The web role
// carries an OPTIONAL handshake that times out but leaves the connection alive,
// so step 6 receiving on c-web also proves optional-handshake-survival across a
// multi-connection case. accepts==2 proves the two steps opened two DISTINCT
// connections in one case (not one shared connection).
func TestRunStepsMultiConnection(t *testing.T) {
	wsURL, accepts := newWSRelayServer(t)

	p := &project.Protocol{TypePath: "type",
		Roles: map[string]*project.ProtocolRole{
			"web": {
				Params:    map[string]string{"type": "web"},
				Handshake: &project.RoleHandshake{AwaitType: "devices:sync", Optional: true, Timeout: 1},
			},
			"bridge": {
				Params: map[string]string{"type": "bridge"},
			},
		}}
	wsIdx := protocolIndexForURL(t, wsURL, p)

	tc := &TestCase{
		ID:     "tc-multi-conn",
		Target: wsURL,
		Steps: []TestStep{
			{Action: "ws_connect", Role: "web", ConnectionID: "c-web"},
			{Action: "ws_connect", Role: "bridge", ConnectionID: "c-bridge"},
			{Action: "ws_send", ConnectionID: "c-web", Message: `{"type":"echo:web"}`},
			{Action: "ws_receive", ConnectionID: "c-bridge", Type: "echo:web", Timeout: 2},
			{Action: "ws_send", ConnectionID: "c-bridge", Message: `{"type":"echo:bridge"}`},
			{Action: "ws_receive", ConnectionID: "c-web", Type: "echo:bridge", Timeout: 2},
		},
	}

	se := newStepExecutionWithIdx(t, tc, wsIdx)
	result := se.runSteps()

	require.Equal(t, StepPassed, result.Status, "multi-connection relay case should pass")
	require.Len(t, result.Evidence, 6, "one evidence entry per step")
	require.Equal(t, int32(2), accepts.Load(),
		"the case must open two distinct connections (accepts=%d)", accepts.Load())
}

// TestRunStepsOptionalHandshakeSuppressesAwaitForLaterReceive is the deterministic
// proof of the optional-handshake suppress-auto-await fix. The server pushes the
// role's AwaitType frame to the client ON CONNECT (modeling a broker that
// broadcasts current presence / initial-sync at join). The case then explicitly
// ws_receive's that same type as the decisive assertion.
//
// WITHOUT the suppress fix, doConnect's optional-handshake auto-await MATCHES and
// CONSUMES the connect-time push, so the later ws_receive finds nothing and times
// out — a false fail (the server demonstrably sent the frame). WITH the fix,
// runSteps sees the later receive on the same connection/type and tells the
// connect to suppress its auto-await; the frame stays buffered and the explicit
// receive catches it. Deterministic, no wall-clock dependency, single connection.
//
// Scope: suppression is OPTIONAL-handshake only. A mandatory handshake's connect
// consumes the AwaitType by design (the redundant receive is dropped at assembly),
// so mandatory is covered by TestConnectRoleHandshakeSuccess and unchanged here.
func TestRunStepsOptionalHandshakeSuppressesAwaitForLaterReceive(t *testing.T) {
	url := newWSTestServer(t, func(conn *websocket.Conn) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		// Presence push on join: the frame the role awaits AND the case receives.
		_ = conn.Write(ctx, websocket.MessageText, []byte(`{"type":"presence:online"}`))
		_, _, _ = conn.Read(ctx) // keep the socket open
	})

	p := &project.Protocol{TypePath: "type",
		Roles: map[string]*project.ProtocolRole{
			"web": {Handshake: &project.RoleHandshake{AwaitType: "presence:online", Optional: true, Timeout: 2}},
		}}
	wsIdx := protocolIndexForURL(t, url, p)

	tc := &TestCase{
		ID:     "tc-opt-suppress",
		Target: url,
		Steps: []TestStep{
			{Action: "ws_connect", Role: "web", ConnectionID: "c1"},
			{Action: "ws_receive", ConnectionID: "c1", Type: "presence:online", Timeout: 2},
		},
	}

	se := newStepExecutionWithIdx(t, tc, wsIdx)
	result := se.runSteps()

	require.Equal(t, StepPassed, result.Status,
		"later ws_receive must catch the connect-time push; without suppress the connect consumed it")
	ws, ok := result.Result.(types.WSResult)
	require.True(t, ok, "final step result must be a WSResult")
	require.Contains(t, ws.MatchedMessage, "presence:online",
		"the explicit receive must have matched the buffered push")
}

// TestSuppressAwaitTypesMap unit-tests the runSteps pre-scan that decides which
// await types a ws_connect must skip because a later ws_receive on the SAME
// connection will assert them (Type + Aliases). Receives on OTHER connections
// and non-receive steps do not contribute. Pure and deterministic.
func TestSuppressAwaitTypesMap(t *testing.T) {
	steps := []TestStep{
		{Action: "ws_connect", ConnectionID: "c1", Role: "web"},
		{Action: "ws_connect", ConnectionID: "c2"},
		{Action: "ws_receive", ConnectionID: "c1", Type: "presence:online", Aliases: []string{"presence:join"}},
		{Action: "ws_send", ConnectionID: "c2", Message: `{"type":"x"}`},
		{Action: "ws_receive", ConnectionID: "c2", Type: "echo"},
	}
	got := suppressAwaitTypes(steps)
	require.ElementsMatch(t, []string{"presence:online", "presence:join"}, got["c1"],
		"c1 connect must suppress the type + aliases its later receive asserts")
	require.ElementsMatch(t, []string{"echo"}, got["c2"], "c2 connect must suppress its later receive type")
	// A connection with no later receive is absent (no suppression).
	_, ok := got["c3"]
	require.False(t, ok, "connection with no later receive must have no suppress set")
}

// TestRunStepsMandatoryHandshakeNotSuppressed locks the scope boundary: even when
// a later ws_receive on the same connection asserts the AwaitType, a MANDATORY
// handshake's connect must STILL run its auto-await and consume the frame (the
// redundant receive is dropped at assembly, not here). The server pushes the
// AwaitType on connect; the mandatory connect consumes it (SeenMessages carries
// it), so the case does NOT suppress. This preserves the mandatory/optional
// semantics split (see cccmemory ws-handshake-optional-vs-mandatory-semantics).
func TestRunStepsMandatoryHandshakeNotSuppressed(t *testing.T) {
	url := newWSTestServer(t, func(conn *websocket.Conn) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = conn.Write(ctx, websocket.MessageText, []byte(`{"type":"presence:online"}`))
		_, _, _ = conn.Read(ctx)
	})
	p := &project.Protocol{TypePath: "type",
		Roles: map[string]*project.ProtocolRole{
			"web": {Handshake: &project.RoleHandshake{AwaitType: "presence:online", Optional: false, Timeout: 2}},
		}}
	wsIdx := protocolIndexForURL(t, url, p)
	tc := &TestCase{
		ID:     "tc-mandatory-no-suppress",
		Target: url,
		Steps: []TestStep{
			{Action: "ws_connect", Role: "web", ConnectionID: "c1"},
			{Action: "ws_receive", ConnectionID: "c1", Type: "presence:online", Timeout: 1},
		},
	}
	se := newStepExecutionWithIdx(t, tc, wsIdx)
	result := se.runSteps()

	// The mandatory connect consumed the push, so the later receive times out and
	// the case FAILS — proving suppress did NOT fire for a mandatory handshake.
	require.NotEqual(t, StepPassed, result.Status,
		"mandatory handshake must not be suppressed; the connect should consume the AwaitType")
}

func TestStepToActionReceiveAliases(t *testing.T) {
	action, err := stepToAction(&TestCase{Target: "ws://x"}, TestStep{
		Action: "ws_receive", ConnectionID: "c1", Type: "session:output",
		Aliases: []string{"session:output-batch"}, Timeout: 2,
	})
	require.NoError(t, err)
	wr, ok := action.(types.WSReceiveAction)
	require.True(t, ok)
	require.Equal(t, "session:output", wr.Type)
	require.Equal(t, []string{"session:output-batch"}, wr.Aliases)
}

// TestStepToActionReceiveMatchAll proves the deterministic Steps runner
// propagates TestStep.MatchAll to WSReceiveAction.MatchAll. Without this wiring,
// match_all is unreachable in the Steps path (the primary WS flow mechanism) —
// the field would silently default false and a match-all receive would behave as
// first-match. (The executor-level match_all tests bypass stepToAction, so they
// cannot catch this gap.)
func TestStepToActionReceiveMatchAll(t *testing.T) {
	action, err := stepToAction(&TestCase{Target: "ws://x"}, TestStep{
		Action: "ws_receive", ConnectionID: "c1", Type: "event",
		MatchAll: true, Asserts: map[string]any{"payload.ok": true}, Timeout: 2,
	})
	require.NoError(t, err)
	wr, ok := action.(types.WSReceiveAction)
	require.True(t, ok)
	require.True(t, wr.MatchAll, "stepToAction must propagate TestStep.MatchAll to WSReceiveAction.MatchAll")
}

// TestRunStepsMatchAllBatch is the end-to-end proof that match_all works through
// the deterministic Steps runner: a server sends one batch frame, the protocol
// declares the batch, and a ws_receive step with MatchAll=true collects every
// decomposed item. Exercises stepToAction -> runSteps -> executor -> match_all.
func TestRunStepsMatchAllBatch(t *testing.T) {
	url := newWSTestServer(t, func(conn *websocket.Conn) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = conn.Write(ctx, websocket.MessageText,
			[]byte(`{"type":"event-batch","payload":{"events":[`+
				`{"id":"a","ok":true},{"id":"b","ok":true},{"id":"c","ok":true}]}}`))
		_, _, _ = conn.Read(ctx) // block until close
	})
	p := &project.Protocol{TypePath: "type", Batches: map[string]*project.ProtocolBatch{
		"event-batch": {ItemType: "event", ItemsPath: "payload.events"},
	}}
	wsIdx := protocolIndexForURL(t, url, p)
	tc := &TestCase{
		ID:     "tc-matchall-batch",
		Target: url,
		Steps: []TestStep{
			{Action: "ws_connect", ConnectionID: "c1"},
			{Action: "ws_receive", ConnectionID: "c1", Type: "event", MatchAll: true,
				Asserts: map[string]any{"payload.ok": true}, Timeout: 2},
		},
	}
	se := newStepExecutionWithIdx(t, tc, wsIdx)
	result := se.runSteps()

	require.Equal(t, StepPassed, result.Status,
		"match-all receive over a decomposed batch should pass the Steps case")
	ws, ok := result.Result.(types.WSResult)
	require.True(t, ok, "final step result must be a WSResult")
	require.Equal(t, 3, ws.MatchedCount, "match-all via Steps must collect all 3 items")
}

// TestStepToActionWSConnectURL covers the per-step dial URL fallback: an empty
// TestStep.URL falls back to tc.Target (the common case), while a non-empty
// TestStep.URL overrides it so a single case can dial peers at different
// endpoints (cross-endpoint multi-party relay).
func TestStepToActionWSConnectURL(t *testing.T) {
	t.Run("empty_url_falls_back_to_target", func(t *testing.T) {
		action, err := stepToAction(&TestCase{Target: "ws://case-target/ws"}, TestStep{
			Action: "ws_connect", Role: "web", ConnectionID: "c1",
		})
		require.NoError(t, err)
		wc, ok := action.(types.WSConnectAction)
		require.True(t, ok)
		require.Equal(t, "ws://case-target/ws", wc.URL, "empty step URL must fall back to tc.Target")
		require.Equal(t, "web", wc.Role)
		require.Equal(t, "c1", wc.ConnectionID)
	})

	t.Run("non_empty_url_overrides_target", func(t *testing.T) {
		action, err := stepToAction(&TestCase{Target: "ws://case-target/ws"}, TestStep{
			Action: "ws_connect", Role: "web", ConnectionID: "c1", URL: "ws://peer-other/endpoint",
		})
		require.NoError(t, err)
		wc, ok := action.(types.WSConnectAction)
		require.True(t, ok)
		require.Equal(t, "ws://peer-other/endpoint", wc.URL, "non-empty step URL must override tc.Target")
	})
}

// TestRunStepsCrossEndpoint proves a single Steps case can dial TWO different
// endpoints in one scenario: two ws_connect steps carry explicit per-step URLs
// pointing at two distinct httptest WS servers. Each server is an echo broker
// for its own connection; a frame round-trips on each. accepts==2 across both
// servers proves the case opened two sockets at two endpoints (not one shared
// connection to tc.Target). The protocol index maps BOTH servers' hosts so
// role/param resolution works on each.
func TestRunStepsCrossEndpoint(t *testing.T) {
	var acceptsA, acceptsB atomic.Int32
	echoA := newWSTestServer(t, func(conn *websocket.Conn) {
		acceptsA.Add(1)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		for {
			mt, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			if err := conn.Write(ctx, mt, data); err != nil {
				return
			}
		}
	})
	echoB := newWSTestServer(t, func(conn *websocket.Conn) {
		acceptsB.Add(1)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		for {
			mt, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			if err := conn.Write(ctx, mt, data); err != nil {
				return
			}
		}
	})

	// Index maps BOTH servers' hosts to the protocol so role params resolve on
	// each endpoint independently (host-based lookup, per WSProtocolIndex.ByHost).
	p := &project.Protocol{TypePath: "type",
		Roles: map[string]*project.ProtocolRole{
			"web":    {Params: map[string]string{"type": "web"}},
			"bridge": {Params: map[string]string{"type": "bridge"}},
		}}
	wsIdx := &WSProtocolIndex{
		ByHost: map[string]*project.Protocol{
			hostOf(t, echoA): p,
			hostOf(t, echoB): p,
		},
	}

	// tc.Target is a closed port; both steps MUST dial their explicit URL or the
	// case fails to connect at all — proving the per-step URL is authoritative.
	tc := &TestCase{
		ID:     "tc-cross-endpoint",
		Target: closedPortURL(t),
		Steps: []TestStep{
			{Action: "ws_connect", Role: "web", ConnectionID: "c-web", URL: echoA + "?type=web"},
			{Action: "ws_connect", Role: "bridge", ConnectionID: "c-bridge", URL: echoB + "?type=bridge"},
			{Action: "ws_send", ConnectionID: "c-web", Message: `{"type":"ping:web"}`},
			{Action: "ws_receive", ConnectionID: "c-web", Type: "ping:web", Timeout: 2},
			{Action: "ws_send", ConnectionID: "c-bridge", Message: `{"type":"ping:bridge"}`},
			{Action: "ws_receive", ConnectionID: "c-bridge", Type: "ping:bridge", Timeout: 2},
		},
	}

	se := newStepExecutionWithIdx(t, tc, wsIdx)
	result := se.runSteps()

	require.Equal(t, StepPassed, result.Status, "cross-endpoint case should pass")
	require.Len(t, result.Evidence, 6, "one evidence entry per step")
	require.Equal(t, int32(1), acceptsA.Load(), "server A must accept exactly one connection")
	require.Equal(t, int32(1), acceptsB.Load(), "server B must accept exactly one connection")
}

// TestStepToActionReceiveExpectAbsent proves the deterministic Steps runner
// propagates TestStep.ExpectAbsent to WSReceiveAction.ExpectAbsent. Without this
// wiring, the sender-exclusion probe flag is unreachable in the Steps path.
func TestStepToActionReceiveExpectAbsent(t *testing.T) {
	action, err := stepToAction(&TestCase{Target: "ws://x"}, TestStep{
		Action: "ws_receive", ConnectionID: "c1", Type: "workflow:task_progress",
		Timeout: 2, ExpectAbsent: true,
	})
	require.NoError(t, err)
	wr, ok := action.(types.WSReceiveAction)
	require.True(t, ok)
	require.True(t, wr.ExpectAbsent, "stepToAction must propagate TestStep.ExpectAbsent to WSReceiveAction.ExpectAbsent")
}

// TestStepEvidenceExpectAbsentThreaded verifies the probe flag lands on the
// trace Evidence so the examiner can recognize a negative probe and derive a
// measured Dimension.Excluded.
func TestStepEvidenceExpectAbsentThreaded(t *testing.T) {
	ev := stepEvidence(TestStep{
		Action: "ws_receive", ConnectionID: "c1", Type: "workflow:task_progress", ExpectAbsent: true,
	}, types.WSResult{OK: true})
	require.True(t, ev.ExpectAbsent, "ExpectAbsent must thread onto Evidence")
	require.Equal(t, "ws_receive", ev.Action)
	require.Equal(t, "c1", ev.ConnectionID)
	require.Equal(t, "workflow:task_progress", ev.MatchedType)
}

func TestResolveHTTPStep(t *testing.T) {
	idx := &WSProtocolIndex{
		ByHost:          map[string]*project.Protocol{"localhost:8989": nil},
		ActorPathParams: map[string]map[string]string{"bridge-actor": {"deviceId": "device_xyz"}},
		ActorHTTPTokens: map[string]string{"web-actor": "JWT-1"},
	}
	proto := &project.Protocol{Roles: map[string]*project.ProtocolRole{
		"web":    {CredentialRef: "web-actor"},
		"bridge": {CredentialRef: "bridge-actor"},
	}}
	idx.ByHost["localhost:8989"] = proto

	t.Run("auth + url placeholder", func(t *testing.T) {
		s := TestStep{
			Action: "http_request", Method: "POST",
			URL:      "http://localhost:8989/api/devices/{{bridge.deviceId}}/restart",
			AuthRole: "web", ExpectStatus: 200,
		}
		a, err := resolveHTTPStep(idx, s)
		if err != nil {
			t.Fatalf("resolveHTTPStep: %v", err)
		}
		ha, ok := a.(types.HTTPAction)
		if !ok {
			t.Fatalf("got %T, want HTTPAction", a)
		}
		if ha.URL != "http://localhost:8989/api/devices/device_xyz/restart" {
			t.Fatalf("URL = %q", ha.URL)
		}
		if ha.Headers["Authorization"] != "Bearer JWT-1" {
			t.Fatalf("Authorization = %q", ha.Headers["Authorization"])
		}
		if ha.Method != "POST" {
			t.Fatalf("Method = %q", ha.Method)
		}
	})
	t.Run("explicit header overrides auth", func(t *testing.T) {
		s := TestStep{Action: "http_request", URL: "http://localhost:8989/x",
			AuthRole: "web", Headers: map[string]string{"Authorization": "Bearer OVERRIDE"}}
		a, err := resolveHTTPStep(idx, s)
		if err != nil {
			t.Fatalf("resolveHTTPStep: %v", err)
		}
		if a.(types.HTTPAction).Headers["Authorization"] != "Bearer OVERRIDE" {
			t.Fatalf("expected explicit override")
		}
	})
	t.Run("missing http token fails", func(t *testing.T) {
		s := TestStep{Action: "http_request", URL: "http://localhost:8989/x", AuthRole: "bridge"}
		_, err := resolveHTTPStep(idx, s)
		if err == nil {
			t.Fatal("expected error: bridge has no http token")
		}
	})
}
