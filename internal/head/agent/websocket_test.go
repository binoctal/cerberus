package agent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/escalation"
	"github.com/binoctal/cerberus/internal/policy"
	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/sandbox"
	"github.com/binoctal/cerberus/internal/types"
)

// newWSTestServer starts an httptest server whose /ws path Accepts a
// connection and hands it to handler. Returns the ws:// URL. Used across
// executor tests.
func newWSTestServer(t *testing.T, handler func(conn *websocket.Conn)) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("accept: %v", err)
			return
		}
		defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()
		handler(conn)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
}

// newWSRelayServer starts an httptest server whose /ws path accepts multiple
// connections, identifies each by its ?type= query param (web|bridge), and
// relays every frame from one connection to the other — modeling a broker / DO
// that forwards web<->bridge. It returns the ws:// URL and an accept counter.
// Race-clean: the hub map is guarded by mu; forwarding looks up the peer under
// the lock, then writes outside it. Used by the multi-connection Steps test.
func newWSRelayServer(t *testing.T) (string, *atomic.Int32) {
	t.Helper()
	var accepts atomic.Int32
	var mu sync.Mutex
	hub := map[string]*websocket.Conn{} // role -> conn

	// forward writes data to the connection whose role differs from fromRole
	// (the single peer in a two-role relay). No peer yet -> drop (the test only
	// sends after both connections are up).
	forward := func(fromRole string, mt websocket.MessageType, data []byte) {
		mu.Lock()
		var target *websocket.Conn
		for role, c := range hub {
			if role != fromRole {
				target = c
				break
			}
		}
		mu.Unlock()
		if target == nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = target.Write(ctx, mt, data)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		accepts.Add(1)
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("accept: %v", err)
			return
		}
		defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()
		role := r.URL.Query().Get("type")
		mu.Lock()
		hub[role] = conn
		mu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for {
			mt, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			forward(role, mt, data)
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws", &accepts
}

func newWSExecutor() *WebSocketExecutor {
	return NewWebSocketExecutor(zap.NewNop(), nil)
}

// protocolIndexForURL builds a WSProtocolIndex mapping the host of url to p.
// Used by tests that need to exercise the protocol-aware path without spinning
// up a full project.Config.
func protocolIndexForURL(t *testing.T, rawURL string, p *project.Protocol) *WSProtocolIndex {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return &WSProtocolIndex{ByHost: map[string]*project.Protocol{u.Host: p}}
}

func TestWSConnectPersistsAndDisconnectCloses(t *testing.T) {
	connects := 0
	url := newWSTestServer(t, func(conn *websocket.Conn) {
		connects++
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, _, _ = conn.Read(ctx) // block until closed
	})

	ex := newWSExecutor()
	ctx := context.Background()

	res := ex.Execute(ctx, types.WSConnectAction{URL: url, ConnectionID: "c1"})
	if !res.Success() {
		t.Fatalf("connect failed: %+v", res)
	}

	// Same connection_id reused: must NOT dial again.
	res2 := ex.Execute(ctx, types.WSDisconnectAction{ConnectionID: "c1"})
	if !res2.Success() {
		t.Fatalf("disconnect failed: %+v", res2)
	}
	if connects != 1 {
		t.Fatalf("server saw %d connects, want 1", connects)
	}
}

func TestWSConnectCtxCancelClosesConnection(t *testing.T) {
	closed := make(chan struct{})
	url := newWSTestServer(t, func(conn *websocket.Conn) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, _, _ = conn.Read(ctx)
		close(closed) // server read returns when client closes
	})

	ex := newWSExecutor()
	ctx, cancel := context.WithCancel(context.Background())
	res := ex.Execute(ctx, types.WSConnectAction{URL: url, ConnectionID: "c1"})
	if !res.Success() {
		t.Fatalf("connect failed: %+v", res)
	}

	cancel()
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("connection not closed after ctx cancel")
	}
}

// TestWSConnectSendsHeaders asserts that a.Headers is actually applied to the
// WebSocket handshake request. The shared newWSTestServer helper hands the
// handler a *websocket.Conn, so it cannot observe request headers; this test
// therefore spins up its own httptest server and inspects r.Header BEFORE
// calling websocket.Accept.
func TestWSConnectSendsHeaders(t *testing.T) {
	seen := make(chan string, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		select {
		case seen <- r.Header.Get("X-Test-Auth"):
		default:
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("accept: %v", err)
			return
		}
		defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, _, _ = conn.Read(ctx)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"

	ex := newWSExecutor()
	res := ex.Execute(context.Background(), types.WSConnectAction{
		URL:          url,
		ConnectionID: "c1",
		Headers:      map[string]string{"X-Test-Auth": "secret"},
	})
	if !res.Success() {
		t.Fatalf("connect failed: %+v", res)
	}
	select {
	case h := <-seen:
		if h != "secret" {
			t.Fatalf("server saw X-Test-Auth=%q, want %q", h, "secret")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server never observed the handshake header (a.Headers was dropped)")
	}
}

// TestWSConnectAutoIDsUnique verifies that when many parallel cases omit
// ConnectionID, the singleton executor mints distinct ids (spec D3: parallel
// cases cannot collide on a shared id). Run with -race.
func TestWSConnectAutoIDsUnique(t *testing.T) {
	url := newWSTestServer(t, func(conn *websocket.Conn) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _, _ = conn.Read(ctx) // block until test ends
	})

	ex := newWSExecutor()
	const n = 100
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			res := ex.Execute(context.Background(), types.WSConnectAction{URL: url})
			if !res.Success() {
				t.Errorf("connect failed: %+v", res)
			}
		}()
	}
	wg.Wait()

	ex.mu.Lock()
	got := len(ex.conns)
	ex.mu.Unlock()
	if got != n {
		t.Fatalf("executor holds %d connections, want %d (auto-id collision)", got, n)
	}
}

// TestWSConnectEchoesAutoConnectionID verifies that when ConnectionID is
// omitted, doConnect echoes the auto-assigned id back in WSResult.ConnectionID
// (so the Steer LLM can reuse it on ws_send/ws_receive instead of guessing and
// hitting an instant "unknown connection_id" failure — the 2026-07-21 dogfood's
// Finding 4). An explicit ConnectionID is echoed verbatim. Run with -race.
func TestWSConnectEchoesAutoConnectionID(t *testing.T) {
	url := newWSTestServer(t, func(conn *websocket.Conn) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, _, _ = conn.Read(ctx) // block until test ends
	})

	ex := newWSExecutor()
	ctx := context.Background()

	// Omitted ConnectionID -> auto-assigned id, echoed in the result.
	r1, ok := ex.Execute(ctx, types.WSConnectAction{URL: url}).(types.WSResult)
	if !ok {
		t.Fatalf("connect result not WSResult")
	}
	if !r1.Success() {
		t.Fatalf("connect failed: %+v", r1)
	}
	if r1.ConnectionID == "" {
		t.Fatal("auto-assigned ConnectionID not echoed in WSResult (Steer LLM cannot reuse it)")
	}

	// A second omitted-id connect mints a distinct id, also echoed.
	r2, ok := ex.Execute(ctx, types.WSConnectAction{URL: url}).(types.WSResult)
	if !ok {
		t.Fatalf("connect result not WSResult")
	}
	if r2.ConnectionID == "" || r2.ConnectionID == r1.ConnectionID {
		t.Fatalf("second auto id not distinct/echoed: %q vs %q", r2.ConnectionID, r1.ConnectionID)
	}

	// An explicit ConnectionID is echoed verbatim.
	r3, ok := ex.Execute(ctx, types.WSConnectAction{URL: url, ConnectionID: "mine"}).(types.WSResult)
	if !ok {
		t.Fatalf("connect result not WSResult")
	}
	if r3.ConnectionID != "mine" {
		t.Fatalf("explicit ConnectionID not echoed: %q", r3.ConnectionID)
	}

	// The echoed auto id is usable: a receive on it must not fail with
	// "unknown connection_id" (it times out instead, proving the connection was
	// found). Timeout=1 keeps the test fast.
	recv, ok := ex.Execute(ctx, types.WSReceiveAction{ConnectionID: r1.ConnectionID, Type: "anything", Timeout: 1}).(types.WSResult)
	if !ok {
		t.Fatalf("receive result not WSResult")
	}
	if strings.Contains(recv.Err, "unknown connection_id") {
		t.Fatalf("echoed auto id not usable on receive: %v", recv.Err)
	}
}

func TestWSSendReusesConnection(t *testing.T) {
	url := newWSTestServer(t, func(conn *websocket.Conn) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		for {
			if _, _, err := conn.Read(ctx); err != nil {
				return
			}
		}
	})

	ex := newWSExecutor()
	ctx := context.Background()
	if !ex.Execute(ctx, types.WSConnectAction{URL: url, ConnectionID: "c1"}).Success() {
		t.Fatal("connect failed")
	}

	res := ex.Execute(ctx, types.WSSendAction{ConnectionID: "c1", Message: `{"type":"ping"}`})
	if !res.Success() {
		t.Fatalf("send failed: %+v", res)
	}
	// Unknown id fails, does not dial.
	res2 := ex.Execute(ctx, types.WSSendAction{ConnectionID: "nope", Message: "x"})
	if res2.Success() {
		t.Fatal("send on unknown connection_id should fail")
	}
}

func TestWSReceiveMatchesByType(t *testing.T) {
	url := newWSTestServer(t, func(conn *websocket.Conn) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		// Server pushes a heartbeat then the awaited response.
		write := func(m map[string]any) {
			b, _ := json.Marshal(m)
			_ = conn.Write(ctx, websocket.MessageText, b)
		}
		write(map[string]any{"type": "heartbeat"})
		write(map[string]any{"type": "permission:response", "payload": map[string]any{"approved": true}})
		_, _, _ = conn.Read(ctx)
	})

	ex := newWSExecutor()
	ctx := context.Background()
	ex.Execute(ctx, types.WSConnectAction{URL: url, ConnectionID: "c1"})

	res := ex.Execute(ctx, types.WSReceiveAction{ConnectionID: "c1", Type: "permission:response", Timeout: 2})
	ws, ok := res.(types.WSResult)
	if !ok {
		t.Fatalf("result type %T, want WSResult", res)
	}
	if !ws.OK {
		t.Fatalf("receive failed: %+v", ws)
	}
	if !strings.Contains(ws.MatchedMessage, "permission:response") {
		t.Fatalf("matched message wrong: %s", ws.MatchedMessage)
	}
	if !strings.Contains(strings.Join(ws.SeenMessages, ""), "heartbeat") {
		t.Fatalf("non-match not captured in seen: %v", ws.SeenMessages)
	}
}

func TestWSReceiveTimeout(t *testing.T) {
	url := newWSTestServer(t, func(conn *websocket.Conn) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		write := func(m map[string]any) {
			b, _ := json.Marshal(m)
			_ = conn.Write(ctx, websocket.MessageText, b)
		}
		write(map[string]any{"type": "unrelated"})
		_, _, _ = conn.Read(ctx)
	})
	ex := newWSExecutor()
	ctx := context.Background()
	ex.Execute(ctx, types.WSConnectAction{URL: url, ConnectionID: "c1"})

	res := ex.Execute(ctx, types.WSReceiveAction{ConnectionID: "c1", Type: "never", Timeout: 1})
	if res.Success() {
		t.Fatalf("expected timeout failure, got success: %+v", res)
	}
}

// TestIsIntermediateStep is a table-driven unit test of the predicate that
// splits a realtime round-trip into intermediate steps (whose success must
// neither pass the case nor consume recovery) and the single decisive step
// (a matching WSReceive, whose success passes the case). It generalizes the
// old isNoopWait: pure waits, WSConnect/WSSend/WSDisconnect, and a
// non-decisive WSReceive are all intermediate; a Decisive WSReceive and a
// wait that probes a selector/state are not.
func TestIsIntermediateStep(t *testing.T) {
	cases := []struct {
		name string
		a    types.TypedAction
		want bool
	}{
		{"ws_connect", types.WSConnectAction{URL: "ws://x"}, true},
		{"ws_send", types.WSSendAction{ConnectionID: "c", Message: "m"}, true},
		{"ws_disconnect", types.WSDisconnectAction{ConnectionID: "c"}, true},
		{"ws_receive decisive=false", types.WSReceiveAction{ConnectionID: "c", Type: "t"}, true},
		{"ws_receive decisive=true", types.WSReceiveAction{ConnectionID: "c", Type: "t", Decisive: true}, false},
		{"pure wait", types.WaitAction{Duration: "1s"}, true},
		{"wait with selector", types.WaitAction{Duration: "1s", Selector: "#x"}, false},
		{"wait with state", types.WaitAction{Duration: "1s", WaitForState: "visible"}, false},
		{"http action is not intermediate", types.HTTPAction{Method: "GET", URL: "http://x"}, false},
	}
	for _, c := range cases {
		if got := isIntermediateStep(c.a); got != c.want {
			t.Fatalf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

// TestReActLoop_IntermediateStepSkipsRecovery drives the ReAct loop with an
// intermediate action (WSConnect) that succeeds, and asserts the Phase-7
// recovery guard does NOT invoke tryRecovery. Without the guard, every
// intermediate success would fall through to tryRecovery — burning a spurious
// recovery LLM call per step and setting recoverySkipped, which mislabels a
// non-passing case as StepSkipped instead of StepFailed (spec D2).
//
// The countingRecovery doubles as a probe: if the guard skips recovery on
// intermediate+success, calls stays 0 across all MaxSteerAttempts.
func TestReActLoop_IntermediateStepSkipsRecovery(t *testing.T) {
	url := newWSTestServer(t, func(conn *websocket.Conn) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _, _ = conn.Read(ctx) // block until the case ends
	})

	// Steer always emits WSConnect (intermediate). It succeeds against the
	// httptest WS server, so the pass-gate sees Success && isIntermediate —
	// which must NOT fire and must NOT consume recovery.
	steerJSON, _ := json.Marshal(SteerOutput{
		Reasoning: "open the websocket before receiving",
		Envelope: types.ActionEnvelope{
			Type: types.ActionWSConnect,
			Raw:  mustJSON(types.WSConnectAction{URL: url, ConnectionID: "c1"}),
		},
	})

	loop, s := testLoop(t, map[string]string{"default": string(steerJSON)}, nil)
	rec := &countingRecovery{}
	loop.recovery = rec
	sessionID := createTestSession(t, s)

	plan := &TestPlan{
		Goal: "intermediate step must not pass or recover",
		Cases: []TestCase{
			{ID: "t1", Name: "connect", Target: "verify ws", Expectation: "connected"},
		},
	}
	results, err := loop.ExecutePlan(context.Background(), plan, sessionID)
	if err != nil {
		t.Fatalf("ExecutePlan error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].Status == StepPassed {
		t.Fatalf("WSConnect is intermediate — its success must not pass the case")
	}
	if rec.calls != 0 {
		t.Fatalf("Phase-7 guard must skip recovery on intermediate+success; Recover was called %d time(s)", rec.calls)
	}
}

// countingRecovery is a recoverer test double that counts Recover() calls and
// returns a benign decision (no skip, no action) so the loop continues. The
// Phase-7 guard test uses it as a probe for whether tryRecovery was invoked.
type countingRecovery struct {
	calls int
}

func (c *countingRecovery) Recover(context.Context, TestCase, types.ExecutorResult, int) (RecoverDecision, error) {
	c.calls++
	return RecoverDecision{}, nil
}
func (c *countingRecovery) SetSessionID(string) {}
func (c *countingRecovery) SetProject(string)   {}

// TestConnectionNamespacingByCaseID verifies M0 D3: two parallel cases that
// both pass the same LLM-supplied connection_id (e.g. "c1") do not collide on
// the singleton executor's connection table. The caseID carried on the per-case
// context namespaces the table key to <caseID>:<connectionID>. Disconnect in
// one case must not touch the other case's connection. Run with -race.
func TestConnectionNamespacingByCaseID(t *testing.T) {
	// connects is written from the httptest server goroutine and read from the
	// test goroutine, so it must be atomic to be -race-clean.
	var connects atomic.Int32
	url := newWSTestServer(t, func(conn *websocket.Conn) {
		connects.Add(1)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, _, _ = conn.Read(ctx)
	})
	ex := newWSExecutor()

	// Two different case contexts, same LLM-supplied connection_id "c1".
	ctxA := context.WithValue(context.Background(), caseIDKey{}, "case-A")
	ctxB := context.WithValue(context.Background(), caseIDKey{}, "case-B")

	if !ex.Execute(ctxA, types.WSConnectAction{URL: url, ConnectionID: "c1"}).Success() {
		t.Fatal("connect A failed")
	}
	if !ex.Execute(ctxB, types.WSConnectAction{URL: url, ConnectionID: "c1"}).Success() {
		t.Fatal("connect B failed")
	}
	if got := connects.Load(); got != 2 {
		t.Fatalf("server saw %d connects, want 2 (namespacing failed)", got)
	}
	// Disconnect in case A must not touch case B's connection.
	ex.Execute(ctxA, types.WSDisconnectAction{ConnectionID: "c1"})
	if !ex.Execute(ctxB, types.WSSendAction{ConnectionID: "c1", Message: `{"type":"ping"}`}).Success() {
		t.Fatal("case B connection lost after case A disconnect (namespacing failed)")
	}
}

// TestMultiExecutorRoutesWSReceiveAndWSDisconnect proves that the LLM-facing
// ws_receive and ws_disconnect actions are routable through the MultiExecutor
// (registry -> ApplyTo -> executors map). The plan gap left them out of
// wsPlugin.ActionTypes(), so before the fix they fell through to the
// "no executor for action type" error even though ex.Execute handled them
// (unit tests passed only because they bypassed routing by calling ex.Execute
// directly). Reaching the "unknown connection_id" error path inside
// doReceive/doDisconnect is positive proof of routing: that string exists
// nowhere else in the codebase.
func TestMultiExecutorRoutesWSReceiveAndWSDisconnect(t *testing.T) {
	logger := zap.NewNop()
	registry := NewPluginRegistry(logger)
	registry.RegisterExecutor(&wsPlugin{executor: NewWebSocketExecutor(logger, nil)})

	multi := NewMultiExecutor(
		policy.NewDefaultActionPolicy("."),
		sandbox.NoOpSandbox{},
		escalation.NoOpGate{},
		logger,
	)
	registry.ApplyTo(multi)

	// Sanity: the action types are bound to the WS executor.
	for _, at := range []types.ActionType{
		types.ActionWSConnect, types.ActionWSSend,
		types.ActionWSReceive, types.ActionWSDisconnect,
	} {
		if _, ok := multi.executors[at]; !ok {
			t.Fatalf("MultiExecutor has no executor registered for %s", at)
		}
	}

	ctx := context.Background()

	// Dispatch a WSReceive through the MultiExecutor (NOT via ex.Execute).
	// Using a bogus connection_id means doReceive must return the
	// "unknown connection_id" error — reachable only if routing worked.
	got := multi.Execute(ctx, types.WSReceiveAction{ConnectionID: "no-such-conn", Type: "x"})
	wsRecv, ok := got.(types.WSResult)
	if !ok {
		t.Fatalf("ws_receive: result type %T, want WSResult (routing dropped or misrouted)", got)
	}
	if wsRecv.OK {
		t.Fatalf("ws_receive: expected OK=false on unknown connection_id, got success")
	}
	if !strings.Contains(wsRecv.Err, "unknown connection_id") {
		t.Fatalf("ws_receive: error %q does not prove doReceive was reached", wsRecv.Err)
	}

	// Same for WSDisconnect.
	got = multi.Execute(ctx, types.WSDisconnectAction{ConnectionID: "no-such-conn"})
	wsDisc, ok := got.(types.WSResult)
	if !ok {
		t.Fatalf("ws_disconnect: result type %T, want WSResult (routing dropped or misrouted)", got)
	}
	if wsDisc.OK {
		t.Fatalf("ws_disconnect: expected OK=false on unknown connection_id, got success")
	}
	if !strings.Contains(wsDisc.Err, "unknown connection_id") {
		t.Fatalf("ws_disconnect: error %q does not prove doDisconnect was reached", wsDisc.Err)
	}
}

// TestReceiveSerializedPerConnection drives two concurrent ws_receive calls
// against the SAME connection_id. coder/websocket forbids concurrent Read on
// one conn (it would race / error), so the executor must serialize them with a
// per-entry read mutex. Run with -race: without the guard this test either
// fails the receive with a "concurrent read" error or trips the race detector.
func TestReceiveSerializedPerConnection(t *testing.T) {
	url := newWSTestServer(t, func(conn *websocket.Conn) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		for i := 0; i < 20; i++ {
			_ = conn.Write(ctx, websocket.MessageText, []byte(`{"type":"ping"}`))
		}
		_, _, _ = conn.Read(ctx)
	})
	ex := newWSExecutor()
	ctx := context.Background()
	ex.Execute(ctx, types.WSConnectAction{URL: url, ConnectionID: "c1"})

	// Two concurrent receives on the SAME connection must serialize, not race.
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			r := ex.Execute(ctx, types.WSReceiveAction{ConnectionID: "c1", Type: "ping", Timeout: 2})
			if !r.Success() {
				errs <- fmt.Errorf("receive failed: %v", r)
				return
			}
			errs <- nil
		}()
	}
	for i := 0; i < 2; i++ {
		select {
		case e := <-errs:
			if e != nil {
				t.Fatal(e)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("receive timed out")
		}
	}
}

// newWSTestServerCapture starts a WS server that records each upgrade
// request's raw query string; returns the ws url and a getter for the most
// recent query. Tests use it to observe exactly what the executor dialed
// (server-side) without relying on any value reported in WSResult.
func newWSTestServerCapture(t *testing.T) (string, func() string) {
	t.Helper()
	var mu sync.Mutex
	var queries []string
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		queries = append(queries, r.URL.RawQuery)
		mu.Unlock()
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()
		_, _, _ = conn.Read(r.Context())
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	return wsURL, func() string {
		mu.Lock()
		defer mu.Unlock()
		if len(queries) == 0 {
			return ""
		}
		return queries[len(queries)-1]
	}
}

// TestConnectInjectsQueryToken proves the resolved raw token reaches the
// dialed url when strategy=query, and that the secret never leaks into
// WSResult.URL (the result carries the pre-injection url).
func TestConnectInjectsQueryToken(t *testing.T) {
	wsURL, latestQuery := newWSTestServerCapture(t)
	p := &project.Protocol{Auth: &project.ProtocolAuth{Strategy: "query", Param: "token", CredentialRef: "web"}}
	idx := &WSProtocolIndex{
		ByHost:      map[string]*project.Protocol{hostOf(t, wsURL): p},
		ActorTokens: map[string]string{"web": "JWT-VALUE"},
	}
	ex := NewWebSocketExecutor(zap.NewNop(), idx)
	res := ex.Execute(context.Background(), types.WSConnectAction{URL: wsURL + "?type=web", ConnectionID: "c1", CredentialRef: "web"})
	ws, ok := res.(types.WSResult)
	if !ok || !ws.Success() {
		t.Fatalf("connect failed: %+v", res)
	}
	// The DIALED url carries the token (observable via the captured upgrade query).
	if !strings.Contains(latestQuery(), "token=JWT-VALUE") {
		t.Fatalf("query missing injected token: %s", latestQuery())
	}
	// The RESULT url is the pre-injection url (no secret).
	if strings.Contains(ws.URL, "JWT-VALUE") {
		t.Fatalf("result url leaks token: %s", ws.URL)
	}
}

// TestConnectStripsLLMSuppliedToken proves that any value the LLM emitted at
// auth.param is stripped before the resolved token is injected — exactly one
// correct credential reaches the server.
func TestConnectStripsLLMSuppliedToken(t *testing.T) {
	wsURL, latestQuery := newWSTestServerCapture(t)
	p := &project.Protocol{Auth: &project.ProtocolAuth{Strategy: "query", Param: "token", CredentialRef: "web"}}
	idx := &WSProtocolIndex{
		ByHost:      map[string]*project.Protocol{hostOf(t, wsURL): p},
		ActorTokens: map[string]string{"web": "REAL"},
	}
	ex := NewWebSocketExecutor(zap.NewNop(), idx)
	ex.Execute(context.Background(), types.WSConnectAction{URL: wsURL + "?type=web&token=LLM-WRONG", ConnectionID: "c1", CredentialRef: "web"})
	q := latestQuery()
	if strings.Contains(q, "LLM-WRONG") {
		t.Fatalf("LLM-supplied token not stripped: %s", q)
	}
	if !strings.Contains(q, "token=REAL") {
		t.Fatalf("resolved token not injected: %s", q)
	}
}

// TestConnectFailsWhenAuthUnresolvable proves a declared auth whose actor has
// no resolved token fails the connect with a non-secret error BEFORE dialing.
// It also verifies the failure path's WSResult.URL strips the declared param:
// a custom param name (e.g. "jwt") not in the default redaction denylist must
// not echo back any LLM-supplied value (spec D6 — secrets never reach results).
func TestConnectFailsWhenAuthUnresolvable(t *testing.T) {
	wsURL, _ := newWSTestServerCapture(t)
	p := &project.Protocol{Auth: &project.ProtocolAuth{Strategy: "query", Param: "token", CredentialRef: "ghost"}}
	idx := &WSProtocolIndex{ByHost: map[string]*project.Protocol{hostOf(t, wsURL): p}, ActorTokens: map[string]string{"web": "X"}}
	ex := NewWebSocketExecutor(zap.NewNop(), idx)
	res := ex.Execute(context.Background(), types.WSConnectAction{URL: wsURL, ConnectionID: "c1"})
	if res.Success() {
		t.Fatalf("want failure for unresolvable auth, got %+v", res)
	}

	// Failure path must also strip a custom-named (non-denylist) param so the
	// LLM-supplied value at auth.param never leaks via WSResult.URL.
	jwtProto := &project.Protocol{Auth: &project.ProtocolAuth{Strategy: "query", Param: "jwt", CredentialRef: "ghost"}}
	jwtIdx := &WSProtocolIndex{ByHost: map[string]*project.Protocol{hostOf(t, wsURL): jwtProto}, ActorTokens: map[string]string{"web": "X"}}
	jwtEx := NewWebSocketExecutor(zap.NewNop(), jwtIdx)
	jwtRes := jwtEx.Execute(context.Background(), types.WSConnectAction{URL: wsURL + "?jwt=LLM-SECRET", ConnectionID: "c2"})
	ws, ok := jwtRes.(types.WSResult)
	if !ok {
		t.Fatalf("result type %T, want WSResult", jwtRes)
	}
	if ws.Success() {
		t.Fatalf("want failure for unresolvable auth, got %+v", ws)
	}
	if strings.Contains(ws.URL, "LLM-SECRET") {
		t.Fatalf("auth-failure WSResult.URL leaks LLM-supplied param: %s", ws.URL)
	}
}

// TestReceiveMatchesByTypePath proves that when a service declares
// type_path: data.event, the executor matches incoming messages by the nested
// routing key (not the M0 top-level "type" field). Without protocol-aware
// matching the nested {"data":{"event":"go"}} frame would never match a.Type
// "go" and the test would time out.
func TestReceiveMatchesByTypePath(t *testing.T) {
	url := newWSTestServer(t, func(conn *websocket.Conn) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = conn.Write(ctx, websocket.MessageText, []byte(`{"data":{"event":"go"}}`))
		_, _, _ = conn.Read(ctx)
	})
	ex := NewWebSocketExecutor(zap.NewNop(), protocolIndexForURL(t, url, &project.Protocol{TypePath: "data.event"}))
	ctx := context.Background()
	ex.Execute(ctx, types.WSConnectAction{URL: url, ConnectionID: "c1"})
	res := ex.Execute(ctx, types.WSReceiveAction{ConnectionID: "c1", Type: "go", Timeout: 2})
	ws, ok := res.(types.WSResult)
	if !ok || !ws.OK {
		t.Fatalf("receive failed: %+v", res)
	}
	if !strings.Contains(ws.MatchedMessage, "go") {
		t.Fatalf("did not match via type_path: %s", ws.MatchedMessage)
	}
}

// TestConnectRoleUsesRoleCredentialAndParams proves that when ws_connect names
// a role, the role's credential_ref is authoritative (not the protocol's
// default-actor) and the role's discriminator params are strip-then-injected
// onto the dial url (any LLM-supplied value at that key is replaced).
func TestConnectRoleUsesRoleCredentialAndParams(t *testing.T) {
	wsURL, latestQuery := newWSTestServerCapture(t)
	p := &project.Protocol{
		Auth: &project.ProtocolAuth{Strategy: "query", Param: "token", CredentialRef: "default-actor"},
		Roles: map[string]*project.ProtocolRole{
			"web": {CredentialRef: "web-actor", Params: map[string]string{"type": "web"}},
		},
	}
	idx := &WSProtocolIndex{
		ByHost:      map[string]*project.Protocol{hostOf(t, wsURL): p},
		ActorTokens: map[string]string{"web-actor": "WEB-JWT", "default-actor": "DEF"},
	}
	ex := NewWebSocketExecutor(zap.NewNop(), idx)
	res := ex.Execute(context.Background(), types.WSConnectAction{URL: wsURL + "?type=WRONG", ConnectionID: "c1", Role: "web"})
	if !res.Success() {
		t.Fatalf("connect failed: %+v", res)
	}
	q := latestQuery()
	if !strings.Contains(q, "token=WEB-JWT") || strings.Contains(q, "DEF") {
		t.Fatalf("role credential not used: %s", q)
	}
	if !strings.Contains(q, "type=web") || strings.Contains(q, "type=WRONG") {
		t.Fatalf("role param not strip-then-injected: %s", q)
	}
}

// TestConnectUnknownRoleFails proves that an unknown role name fails the
// connect (no dial) — the role must be declared in the protocol. The failure
// path's WSResult.URL is secret-free (auth.param stripped if any).
func TestConnectUnknownRoleFails(t *testing.T) {
	wsURL, latestQuery := newWSTestServerCapture(t)
	p := &project.Protocol{Roles: map[string]*project.ProtocolRole{"web": {}}}
	idx := &WSProtocolIndex{ByHost: map[string]*project.Protocol{hostOf(t, wsURL): p}}
	ex := NewWebSocketExecutor(zap.NewNop(), idx)
	res := ex.Execute(context.Background(), types.WSConnectAction{URL: wsURL, ConnectionID: "c1", Role: "ghost"})
	if res.Success() {
		t.Fatalf("unknown role should fail: %+v", res)
	}
	if latestQuery() != "" {
		t.Fatalf("unknown role should not dial: %s", latestQuery())
	}
}

// TestConnectRoleHandshakeSuccess proves that when a role declares a handshake,
// doConnect auto-awaits the declared await_type via a readMu-guarded receive
// loop and includes the handshake message (plus any non-matching frames that
// arrived first) in SeenMessages.
func TestConnectRoleHandshakeSuccess(t *testing.T) {
	wsURL := newWSTestServer(t, func(conn *websocket.Conn) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = conn.Write(ctx, websocket.MessageText, []byte(`{"type":"devices:sync"}`))
		_, _, _ = conn.Read(ctx)
	})
	p := &project.Protocol{TypePath: "type",
		Roles: map[string]*project.ProtocolRole{"web": {Handshake: &project.RoleHandshake{AwaitType: "devices:sync", Timeout: 2}}}}
	ex := NewWebSocketExecutor(zap.NewNop(), protocolIndexForURL(t, wsURL, p))
	res := ex.Execute(context.Background(), types.WSConnectAction{URL: wsURL, ConnectionID: "c1", Role: "web"})
	ws, ok := res.(types.WSResult)
	if !ok || !ws.Success() {
		t.Fatalf("connect failed: %+v", res)
	}
	if !strings.Contains(strings.Join(ws.SeenMessages, ""), "devices:sync") {
		t.Fatalf("handshake message not in evidence: %v", ws.SeenMessages)
	}
}

// TestConnectRoleHandshakeTimeoutFailsAndCleansUp proves that when the awaited
// handshake message does not arrive within role.Handshake.Timeout, doConnect
// fails AND tears down the connection (closes the socket, removes the entry)
// so a subsequent ws_send on that id fails as unknown.
func TestConnectRoleHandshakeTimeoutFailsAndCleansUp(t *testing.T) {
	wsURL := newWSTestServer(t, func(conn *websocket.Conn) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_, _, _ = conn.Read(ctx) // never sends devices:sync
	})
	p := &project.Protocol{TypePath: "type",
		Roles: map[string]*project.ProtocolRole{"web": {Handshake: &project.RoleHandshake{AwaitType: "devices:sync", Timeout: 1}}}}
	ex := NewWebSocketExecutor(zap.NewNop(), protocolIndexForURL(t, wsURL, p))
	ctx := context.Background()
	res := ex.Execute(ctx, types.WSConnectAction{URL: wsURL, ConnectionID: "c1", Role: "web"})
	if res.Success() {
		t.Fatalf("handshake timeout should fail: %+v", res)
	}
	// Connection must be cleaned up: a subsequent send fails as unknown id.
	if ex.Execute(ctx, types.WSSendAction{ConnectionID: "c1", Message: `{"type":"x"}`}).Success() {
		t.Fatal("connection should be removed after handshake timeout")
	}
}

// TestConnectRoleHandshakeOptionalSucceedsOnTimeout is the headline proof of F2
// (optional/best-effort handshake): a role whose handshake is Optional:true
// SUCCEEDS even when the awaited message never arrives within the timeout — and
// the connection STAYS USABLE. A follow-up ws_send + ws_receive on the SAME
// connection_id must succeed, proving the pump kept the conn alive across the
// handshake timeout (readMu released, entry not deleted, pump still running).
// This mirrors TestWSReceiveTimeoutLeavesConnectionAlive but for the handshake
// path, which historically closed+deleted the conn on any non-match.
func TestConnectRoleHandshakeOptionalSucceedsOnTimeout(t *testing.T) {
	url := newWSTestServer(t, func(conn *websocket.Conn) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		// Echo loop: server sends nothing unsolicited, so the optional handshake
		// awaiting "devices:sync" times out. It only writes back what the client
		// sends, proving the conn survived the handshake-timeout.
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
	p := &project.Protocol{TypePath: "type",
		Roles: map[string]*project.ProtocolRole{"web": {Handshake: &project.RoleHandshake{AwaitType: "devices:sync", Timeout: 1, Optional: true}}}}
	ex := NewWebSocketExecutor(zap.NewNop(), protocolIndexForURL(t, url, p))
	ctx := context.Background()

	res := ex.Execute(ctx, types.WSConnectAction{URL: url, ConnectionID: "c1", Role: "web"})
	ws, ok := res.(types.WSResult)
	if !ok || !ws.OK {
		t.Fatalf("optional handshake should succeed on timeout, got %+v", res)
	}
	if ws.ConnectionID != "c1" {
		t.Fatalf("connection id not echoed: %q", ws.ConnectionID)
	}

	// Headline: the connection is STILL ALIVE. A post-handshake send + receive
	// on the SAME connection_id must succeed (conn kept under the pump).
	if !ex.Execute(ctx, types.WSSendAction{ConnectionID: "c1", Message: `{"type":"echo"}`}).Success() {
		t.Fatal("send after optional-handshake-timeout failed (conn was closed/deleted)")
	}
	recv := ex.Execute(ctx, types.WSReceiveAction{ConnectionID: "c1", Type: "echo", Timeout: 2})
	if !recv.Success() {
		t.Fatalf("receive after optional-handshake-timeout failed (conn not alive): %+v", recv)
	}
}

// TestConnectRoleHandshakeOptionalCapturesWhenPresent proves the best-effort
// path still CAPTURES the awaited message when it does arrive: connect returns
// OK:true and SeenMessages contains the awaited type (plus any non-matching
// frames seen first). Optional only changes the timeout outcome, not the match.
func TestConnectRoleHandshakeOptionalCapturesWhenPresent(t *testing.T) {
	wsURL := newWSTestServer(t, func(conn *websocket.Conn) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = conn.Write(ctx, websocket.MessageText, []byte(`{"type":"hello"}`))
		_ = conn.Write(ctx, websocket.MessageText, []byte(`{"type":"devices:sync"}`))
		_, _, _ = conn.Read(ctx)
	})
	p := &project.Protocol{TypePath: "type",
		Roles: map[string]*project.ProtocolRole{"web": {Handshake: &project.RoleHandshake{AwaitType: "devices:sync", Timeout: 2, Optional: true}}}}
	ex := NewWebSocketExecutor(zap.NewNop(), protocolIndexForURL(t, wsURL, p))
	res := ex.Execute(context.Background(), types.WSConnectAction{URL: wsURL, ConnectionID: "c1", Role: "web"})
	ws, ok := res.(types.WSResult)
	if !ok || !ws.OK {
		t.Fatalf("optional handshake should succeed when message arrives, got %+v", res)
	}
	joined := strings.Join(ws.SeenMessages, "")
	if !strings.Contains(joined, "devices:sync") {
		t.Fatalf("awaited handshake message not captured in evidence: %v", ws.SeenMessages)
	}
	if !strings.Contains(joined, "hello") {
		t.Fatalf("non-matching preamble not captured in evidence: %v", ws.SeenMessages)
	}
}

// TestInjectAuthQueryBadURLFails locks in the M1 bad-url guard that Task 4
// regressed: with strategy=query, a malformed dial url must fail fast inside
// injectAuth with "ws auth: bad url" rather than silently falling back through
// setQueryParam/stripQuery (which return the raw url on parse error) and
// surfacing later as a less-specific websocket.Dial error. Tested directly
// because, in the Execute flow, a malformed a.URL already short-circuits at
// resolveProtocol (nil proto -> injectAuth is skipped); this unit test covers
// the guard against any future caller that invokes injectAuth directly.
//
// The resolved real token must NOT appear in either returned url on the error
// path (M1 secret hygiene).
func TestInjectAuthQueryBadURLFails(t *testing.T) {
	// "ws://[" + ":" yields "ws://[:" which url.Parse rejects with a
	// "missing ']' in host" error. Constructed at runtime so staticcheck's
	// SA1007 invalid-URL literal check does not flag the deliberate malformed
	// input. The resolved token ("REAL-SECRET") is never written to either
	// returned url because the guard fires before setQueryParam/stripQuery.
	malformedDialURL := "ws://[" + ":" + "::1"

	idx := &WSProtocolIndex{ActorTokens: map[string]string{"web": "REAL-SECRET"}}
	ex := NewWebSocketExecutor(zap.NewNop(), idx)
	auth := &project.ProtocolAuth{Strategy: "query", Param: "token"}
	opts := &websocket.DialOptions{}

	dialURL, preInjectionURL, err := ex.injectAuth(context.Background(), malformedDialURL, "web", auth, opts)
	if err == nil {
		t.Fatalf("injectAuth: expected bad-url error, got nil (dialURL=%q preInjectionURL=%q)", dialURL, preInjectionURL)
	}
	if !strings.Contains(err.Error(), "ws auth: bad url") {
		t.Fatalf("injectAuth: error %q does not contain \"ws auth: bad url\"", err.Error())
	}
	if dialURL != "" {
		t.Fatalf("injectAuth: error path must return empty dialURL, got %q", dialURL)
	}
	if preInjectionURL != "" {
		t.Fatalf("injectAuth: error path must return empty preInjectionURL, got %q", preInjectionURL)
	}
	if strings.Contains(err.Error(), "REAL-SECRET") {
		t.Fatalf("injectAuth: error leaks resolved token: %s", err.Error())
	}
}

func TestReceiveAssertPass(t *testing.T) {
	wsURL := newWSTestServer(t, func(conn *websocket.Conn) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = conn.Write(ctx, websocket.MessageText, []byte(`{"type":"approval","payload":{"approved":true}}`))
		_, _, _ = conn.Read(ctx)
	})
	ex := newWSExecutor()
	ctx := context.Background()
	ex.Execute(ctx, types.WSConnectAction{URL: wsURL, ConnectionID: "c1"})
	res := ex.Execute(ctx, types.WSReceiveAction{
		ConnectionID: "c1", Type: "approval",
		Assert: map[string]any{"payload.approved": true},
	})
	ws, ok := res.(types.WSResult)
	if !ok || !ws.Success() {
		t.Fatalf("receive failed: %+v", res)
	}
	if !strings.Contains(ws.MatchedMessage, "approval") {
		t.Fatalf("matched message not returned: %s", ws.MatchedMessage)
	}
}

func TestReceiveAssertValueMismatchFails(t *testing.T) {
	wsURL := newWSTestServer(t, func(conn *websocket.Conn) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = conn.Write(ctx, websocket.MessageText, []byte(`{"type":"approval","payload":{"approved":false}}`))
		_, _, _ = conn.Read(ctx)
	})
	ex := newWSExecutor()
	ctx := context.Background()
	ex.Execute(ctx, types.WSConnectAction{URL: wsURL, ConnectionID: "c1"})
	res := ex.Execute(ctx, types.WSReceiveAction{
		ConnectionID: "c1", Type: "approval",
		Assert: map[string]any{"payload.approved": true},
	})
	ws, ok := res.(types.WSResult)
	if ok && ws.Success() {
		t.Fatalf("receive should fail on assertion mismatch: %+v", res)
	}
	if !strings.Contains(ws.Err, "payload.approved") || !strings.Contains(ws.Err, "true") || !strings.Contains(ws.Err, "false") {
		t.Fatalf("err should name path/expected/actual: %q", ws.Err)
	}
	if !strings.Contains(ws.MatchedMessage, "approval") {
		t.Fatalf("matched message should still be evidence on assert failure: %s", ws.MatchedMessage)
	}
}

func TestReceiveAssertMissingPathFails(t *testing.T) {
	wsURL := newWSTestServer(t, func(conn *websocket.Conn) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = conn.Write(ctx, websocket.MessageText, []byte(`{"type":"x"}`))
		_, _, _ = conn.Read(ctx)
	})
	ex := newWSExecutor()
	ctx := context.Background()
	ex.Execute(ctx, types.WSConnectAction{URL: wsURL, ConnectionID: "c1"})
	res := ex.Execute(ctx, types.WSReceiveAction{
		ConnectionID: "c1", Type: "x",
		Assert: map[string]any{"payload.approved": true},
	})
	ws, _ := res.(types.WSResult)
	if ws.Success() {
		t.Fatalf("absent assert path should fail: %+v", res)
	}
	if !strings.Contains(ws.Err, "payload.approved") || !strings.Contains(ws.Err, "<missing>") {
		t.Fatalf("err should report missing path: %q", ws.Err)
	}
}

func TestReceiveAssertNumericNormalization(t *testing.T) {
	wsURL := newWSTestServer(t, func(conn *websocket.Conn) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = conn.Write(ctx, websocket.MessageText, []byte(`{"type":"x","n":5}`))
		_, _, _ = conn.Read(ctx)
	})
	ex := newWSExecutor()
	ctx := context.Background()
	ex.Execute(ctx, types.WSConnectAction{URL: wsURL, ConnectionID: "c1"})
	// Expected int 5 must match the JSON-decoded float64 5.
	res := ex.Execute(ctx, types.WSReceiveAction{
		ConnectionID: "c1", Type: "x",
		Assert: map[string]any{"n": 5},
	})
	if !res.Success() {
		t.Fatalf("numeric assert should pass with normalization: %+v", res)
	}
}

func TestReceiveAssertMultipleReportsFirstSorted(t *testing.T) {
	wsURL := newWSTestServer(t, func(conn *websocket.Conn) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = conn.Write(ctx, websocket.MessageText, []byte(`{"type":"x","a":1,"z":1}`))
		_, _, _ = conn.Read(ctx)
	})
	ex := newWSExecutor()
	ctx := context.Background()
	ex.Execute(ctx, types.WSConnectAction{URL: wsURL, ConnectionID: "c1"})
	// Both fail; sorted order reports "a" before "z".
	res := ex.Execute(ctx, types.WSReceiveAction{
		ConnectionID: "c1", Type: "x",
		Assert: map[string]any{"z": 99, "a": 99},
	})
	ws, _ := res.(types.WSResult)
	if ws.Success() {
		t.Fatalf("assertions should fail: %+v", res)
	}
	// Both fail; sorted order reports "a" before "z".
	if !strings.Contains(ws.Err, "assert a") {
		t.Fatalf("should report the sorted-first failing path 'a': %q", ws.Err)
	}
	if strings.Contains(ws.Err, "assert z") {
		t.Fatalf("should not report 'z' (sorted-first is 'a'): %q", ws.Err)
	}
}

// capturedFrame records the opcode and payload of one frame read by a test
// server, used to prove doSend wrote the right WS frame type.
type capturedFrame struct {
	mt   websocket.MessageType
	data []byte
}

func TestSendBinaryFrameOpcodeAndPayload(t *testing.T) {
	got := make(chan capturedFrame, 1)
	url := newWSTestServer(t, func(conn *websocket.Conn) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if mt, data, err := conn.Read(ctx); err == nil {
			got <- capturedFrame{mt, data}
		}
		_, _, _ = conn.Read(ctx) // block until close
	})
	ex := NewWebSocketExecutor(zap.NewNop(), protocolIndexForURL(t, url, &project.Protocol{Framing: "binary"}))
	ctx := context.Background()
	if !ex.Execute(ctx, types.WSConnectAction{URL: url, ConnectionID: "c1"}).Success() {
		t.Fatal("connect failed")
	}
	// "hello" base64 = "aGVsbG8="
	res := ex.Execute(ctx, types.WSSendAction{ConnectionID: "c1", Message: "aGVsbG8="})
	if !res.Success() {
		t.Fatalf("send failed: %+v", res)
	}
	select {
	case f := <-got:
		if f.mt != websocket.MessageBinary {
			t.Fatalf("opcode = %v, want MessageBinary", f.mt)
		}
		if string(f.data) != "hello" {
			t.Fatalf("payload = %q, want %q", f.data, "hello")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not observe the frame")
	}
}

func TestSendTextFrameOpcode(t *testing.T) {
	got := make(chan capturedFrame, 1)
	url := newWSTestServer(t, func(conn *websocket.Conn) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if mt, data, err := conn.Read(ctx); err == nil {
			got <- capturedFrame{mt, data}
		}
		_, _, _ = conn.Read(ctx)
	})
	ex := NewWebSocketExecutor(zap.NewNop(), protocolIndexForURL(t, url, &project.Protocol{Framing: "text"}))
	ctx := context.Background()
	if !ex.Execute(ctx, types.WSConnectAction{URL: url, ConnectionID: "c1"}).Success() {
		t.Fatal("connect failed")
	}
	if !ex.Execute(ctx, types.WSSendAction{ConnectionID: "c1", Message: "PING"}).Success() {
		t.Fatal("send failed")
	}
	select {
	case f := <-got:
		if f.mt != websocket.MessageText {
			t.Fatalf("opcode = %v, want MessageText", f.mt)
		}
		if string(f.data) != "PING" {
			t.Fatalf("payload = %q, want PING", f.data)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not observe the frame")
	}
}

func TestSendBinaryInvalidBase64Fails(t *testing.T) {
	url := newWSTestServer(t, func(conn *websocket.Conn) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, _, _ = conn.Read(ctx)
	})
	ex := NewWebSocketExecutor(zap.NewNop(), protocolIndexForURL(t, url, &project.Protocol{Framing: "binary"}))
	ctx := context.Background()
	if !ex.Execute(ctx, types.WSConnectAction{URL: url, ConnectionID: "c1"}).Success() {
		t.Fatal("connect failed")
	}
	res := ex.Execute(ctx, types.WSSendAction{ConnectionID: "c1", Message: "@@@@"})
	ws, ok := res.(types.WSResult)
	if !ok || ws.OK {
		t.Fatalf("want base64 error, got %+v", res)
	}
	if !strings.Contains(ws.Err, "base64") {
		t.Fatalf("err should mention base64: %q", ws.Err)
	}
}

func TestReceiveTextExactMatch(t *testing.T) {
	url := newWSTestServer(t, func(conn *websocket.Conn) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = conn.Write(ctx, websocket.MessageText, []byte("PENDING"))
		_ = conn.Write(ctx, websocket.MessageText, []byte("READY"))
		_, _, _ = conn.Read(ctx)
	})
	ex := NewWebSocketExecutor(zap.NewNop(), protocolIndexForURL(t, url, &project.Protocol{Framing: "text"}))
	ctx := context.Background()
	ex.Execute(ctx, types.WSConnectAction{URL: url, ConnectionID: "c1"})
	res := ex.Execute(ctx, types.WSReceiveAction{ConnectionID: "c1", Type: "READY", Timeout: 2})
	ws, ok := res.(types.WSResult)
	if !ok || !ws.OK {
		t.Fatalf("receive failed: %+v", res)
	}
	if ws.MatchedMessage != "READY" {
		t.Fatalf("matched = %q, want READY (whole-frame exact)", ws.MatchedMessage)
	}
	if !strings.Contains(strings.Join(ws.SeenMessages, ""), "PENDING") {
		t.Fatalf("non-match not captured in seen: %v", ws.SeenMessages)
	}
}

func TestReceiveBinaryExactMatch(t *testing.T) {
	want := []byte{0x00, 0xff, 0x10}
	url := newWSTestServer(t, func(conn *websocket.Conn) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = conn.Write(ctx, websocket.MessageBinary, []byte{0x01, 0x02}) // non-match
		_ = conn.Write(ctx, websocket.MessageBinary, want)
		_, _, _ = conn.Read(ctx)
	})
	ex := NewWebSocketExecutor(zap.NewNop(), protocolIndexForURL(t, url, &project.Protocol{Framing: "binary"}))
	ctx := context.Background()
	ex.Execute(ctx, types.WSConnectAction{URL: url, ConnectionID: "c1"})
	res := ex.Execute(ctx, types.WSReceiveAction{ConnectionID: "c1", Type: base64.StdEncoding.EncodeToString(want), Timeout: 2})
	ws, ok := res.(types.WSResult)
	if !ok || !ws.OK {
		t.Fatalf("receive failed: %+v", res)
	}
	if ws.MatchedMessage != base64.StdEncoding.EncodeToString(want) {
		t.Fatalf("matched = %q, want base64 of %x", ws.MatchedMessage, want)
	}
}

func TestReceiveBinaryInvalidTypeFails(t *testing.T) {
	url := newWSTestServer(t, func(conn *websocket.Conn) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, _, _ = conn.Read(ctx) // fast-fail happens before any read
	})
	ex := NewWebSocketExecutor(zap.NewNop(), protocolIndexForURL(t, url, &project.Protocol{Framing: "binary"}))
	ctx := context.Background()
	ex.Execute(ctx, types.WSConnectAction{URL: url, ConnectionID: "c1"})
	res := ex.Execute(ctx, types.WSReceiveAction{ConnectionID: "c1", Type: "@@@@", Timeout: 2})
	ws, ok := res.(types.WSResult)
	if !ok || ws.OK {
		t.Fatalf("want fast error, got %+v", res)
	}
	if !strings.Contains(ws.Err, "base64") {
		t.Fatalf("err should mention base64: %q", ws.Err)
	}
}

func TestReceiveAssertRejectedUnderTextFraming(t *testing.T) {
	url := newWSTestServer(t, func(conn *websocket.Conn) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = conn.Write(ctx, websocket.MessageText, []byte("READY"))
		_, _, _ = conn.Read(ctx)
	})
	ex := NewWebSocketExecutor(zap.NewNop(), protocolIndexForURL(t, url, &project.Protocol{Framing: "text"}))
	ctx := context.Background()
	ex.Execute(ctx, types.WSConnectAction{URL: url, ConnectionID: "c1"})
	res := ex.Execute(ctx, types.WSReceiveAction{ConnectionID: "c1", Type: "READY", Assert: map[string]any{"x": 1}, Timeout: 2})
	ws, ok := res.(types.WSResult)
	if !ok || ws.OK {
		t.Fatalf("want assert-rejected error, got %+v", res)
	}
	if !strings.Contains(ws.Err, "assert requires json framing") {
		t.Fatalf("err: %q", ws.Err)
	}
}

// TestConnectRoleHandshakeTextFraming proves D5: the roles handshake loop matches
// await_type via the framing-aware predicate, so a text-framed protocol with a
// mandatory handshake still completes. The non-matching "WELCOME" frame must
// accumulate into SeenMessages, and "READY" must satisfy await_type by exact
// whole-frame equality (not JSON type_path).
func TestConnectRoleHandshakeTextFraming(t *testing.T) {
	url := newWSTestServer(t, func(conn *websocket.Conn) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = conn.Write(ctx, websocket.MessageText, []byte("WELCOME"))
		_ = conn.Write(ctx, websocket.MessageText, []byte("READY"))
		_, _, _ = conn.Read(ctx)
	})
	p := &project.Protocol{Framing: "text", Roles: map[string]*project.ProtocolRole{
		"web": {Handshake: &project.RoleHandshake{AwaitType: "READY", Timeout: 2}},
	}}
	ex := NewWebSocketExecutor(zap.NewNop(), protocolIndexForURL(t, url, p))
	res := ex.Execute(context.Background(), types.WSConnectAction{URL: url, ConnectionID: "c1", Role: "web"})
	ws, ok := res.(types.WSResult)
	if !ok || !ws.Success() {
		t.Fatalf("connect failed: %+v", res)
	}
	if !strings.Contains(strings.Join(ws.SeenMessages, ""), "WELCOME") {
		t.Fatalf("non-matching handshake frame not captured: %v", ws.SeenMessages)
	}
}

// TestConnectRoleHandshakeBinaryFraming proves the handshake loop honors binary
// framing: await_type is base64, matched by exact whole-frame bytes.
func TestConnectRoleHandshakeBinaryFraming(t *testing.T) {
	ready := []byte{0x01, 0x02}
	url := newWSTestServer(t, func(conn *websocket.Conn) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = conn.Write(ctx, websocket.MessageBinary, ready)
		_, _, _ = conn.Read(ctx)
	})
	p := &project.Protocol{Framing: "binary", Roles: map[string]*project.ProtocolRole{
		"web": {Handshake: &project.RoleHandshake{AwaitType: base64.StdEncoding.EncodeToString(ready), Timeout: 2}},
	}}
	ex := NewWebSocketExecutor(zap.NewNop(), protocolIndexForURL(t, url, p))
	res := ex.Execute(context.Background(), types.WSConnectAction{URL: url, ConnectionID: "c1", Role: "web"})
	if !res.Success() {
		t.Fatalf("binary handshake should complete: %+v", res)
	}
}

// TestConnectRoleHeadersInjected proves a role's declared header is
// strip-then-injected onto the dial: any LLM-supplied value at the same key is
// removed and exactly the role's value reaches the server.
func TestConnectRoleHeadersInjected(t *testing.T) {
	seen := make(chan string, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		select {
		case seen <- r.Header.Get("X-Role"):
		default:
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("accept: %v", err)
			return
		}
		defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()
		_, _, _ = conn.Read(r.Context())
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"

	p := &project.Protocol{Roles: map[string]*project.ProtocolRole{
		"web": {Headers: map[string]string{"X-Role": "web"}},
	}}
	ex := NewWebSocketExecutor(zap.NewNop(), protocolIndexForURL(t, wsURL, p))
	// LLM also supplies X-Role (wrong) — must be stripped to the role's value.
	res := ex.Execute(context.Background(), types.WSConnectAction{
		URL: wsURL, ConnectionID: "c1", Role: "web",
		Headers: map[string]string{"X-Role": "LLM-WRONG"},
	})
	if !res.Success() {
		t.Fatalf("connect failed: %+v", res)
	}
	select {
	case h := <-seen:
		if h != "web" {
			t.Fatalf("server saw X-Role=%q, want %q (LLM value not stripped)", h, "web")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server never observed the role header")
	}
}

// TestConnectRoleSubprotocolsInjected proves a role's declared subprotocol is
// offered, and an LLM-supplied duplicate is stripped (offered exactly once).
func TestConnectRoleSubprotocolsInjected(t *testing.T) {
	seen := make(chan string, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		select {
		case seen <- r.Header.Get("Sec-WebSocket-Protocol"):
		default:
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("accept: %v", err)
			return
		}
		defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()
		_, _, _ = conn.Read(r.Context())
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"

	p := &project.Protocol{Roles: map[string]*project.ProtocolRole{
		"web": {Subprotocols: []string{"web.v1"}},
	}}
	ex := NewWebSocketExecutor(zap.NewNop(), protocolIndexForURL(t, wsURL, p))
	// LLM also offers web.v1 (duplicate) — must be stripped to exactly one offer.
	res := ex.Execute(context.Background(), types.WSConnectAction{
		URL: wsURL, ConnectionID: "c1", Role: "web",
		Subprotocols: []string{"web.v1"},
	})
	if !res.Success() {
		t.Fatalf("connect failed: %+v", res)
	}
	select {
	case offered := <-seen:
		if !strings.Contains(offered, "web.v1") {
			t.Fatalf("role subprotocol not offered: %q", offered)
		}
		if c := strings.Count(offered, "web.v1"); c != 1 {
			t.Fatalf("role subprotocol offered %d times, want 1 (LLM duplicate not stripped): %q", c, offered)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server never observed offered subprotocols")
	}
}

// TestWSReceiveTimeoutLeavesConnectionAlive is the headline proof of the
// per-connection read pump: a ws_receive that times out (no matching frame)
// does NOT close the connection. A subsequent ws_send + ws_receive on the SAME
// connection_id must succeed. Under the old "read with a timeout context" model,
// coder/websocket registered context.AfterFunc(ctx, func(){ c.close() }) on the
// read, so the timeout closed the conn and the post-timeout send failed.
func TestWSReceiveTimeoutLeavesConnectionAlive(t *testing.T) {
	url := newWSTestServer(t, func(conn *websocket.Conn) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		// Echo loop: the server sends nothing unsolicited, so the first receive
		// (awaiting "anything") times out. It only writes back what the client
		// sends, proving the conn survives the receive-timeout.
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
	ex := newWSExecutor()
	ctx := context.Background()

	if !ex.Execute(ctx, types.WSConnectAction{URL: url, ConnectionID: "c1"}).Success() {
		t.Fatal("connect failed")
	}

	// First receive times out: server sends nothing, so no frame matches.
	r1 := ex.Execute(ctx, types.WSReceiveAction{ConnectionID: "c1", Type: "anything", Timeout: 1})
	if r1.Success() {
		t.Fatalf("first receive should time out, got success: %+v", r1)
	}

	// Headline assertion: the connection is STILL ALIVE. A post-timeout send +
	// receive on the SAME connection_id must succeed.
	if !ex.Execute(ctx, types.WSSendAction{ConnectionID: "c1", Message: `{"type":"echo"}`}).Success() {
		t.Fatal("send after receive-timeout failed (conn closed by read-timeout)")
	}
	r2 := ex.Execute(ctx, types.WSReceiveAction{ConnectionID: "c1", Type: "echo", Timeout: 2})
	ws, ok := r2.(types.WSResult)
	if !ok || !ws.OK {
		t.Fatalf("receive after receive-timeout failed (conn not alive): %+v", r2)
	}
}

// TestWSReceiveAfterPeerCloseReturnsError proves the pump-exit path: when the
// peer closes the connection mid-case, the pump goroutine exits (sets pumpErr,
// closes done), and a subsequent ws_receive returns an error promptly rather
// than hanging on a dead channel.
func TestWSReceiveAfterPeerCloseReturnsError(t *testing.T) {
	peerClose := make(chan struct{})
	url := newWSTestServer(t, func(conn *websocket.Conn) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		// Wait for the test to signal, then close (peer close from client's view).
		select {
		case <-peerClose:
		case <-ctx.Done():
			return
		}
		_ = conn.Close(websocket.StatusNormalClosure, "peer done")
	})
	ex := newWSExecutor()
	ctx := context.Background()
	if !ex.Execute(ctx, types.WSConnectAction{URL: url, ConnectionID: "c1"}).Success() {
		t.Fatal("connect failed")
	}

	// Close the peer connection, then a receive must return an error (not hang).
	close(peerClose)

	done := make(chan types.ExecutorResult, 1)
	go func() {
		done <- ex.Execute(ctx, types.WSReceiveAction{ConnectionID: "c1", Type: "anything", Timeout: 5})
	}()
	select {
	case r := <-done:
		if r.Success() {
			t.Fatalf("receive after peer close should fail, got success: %+v", r)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("receive hung after peer close (pump did not exit / done not observed)")
	}
}

// TestWSParallelDifferentConnections closes the opus-review M2 gap: there was no
// explicit test for concurrent cases on DIFFERENT connections running in parallel
// (same-conn serialization is covered by TestReceiveSerializedPerConnection). N
// goroutines each open their OWN connection (distinct id) and run
// connect→send→receive against an echo server, concurrently, for many iterations.
//
// A server-side barrier forces all N connections to be accepted simultaneously
// before any echoes proceed: a sequential open would leave the first handler
// stuck at the barrier forever (the test would time out, not pass). Under -race
// this also proves each connection's own pump + readMu keeps frame reads
// corruption-free across parallel connections.
func TestWSParallelDifferentConnections(t *testing.T) {
	const n = 8
	const iters = 20

	// wsBarrier synchronizes one iteration's batch of connections: every handler
	// signals arrived, then blocks on release until all N are open concurrently.
	type wsBarrier struct {
		arrived chan struct{}
		release chan struct{}
	}
	var barrierMu sync.Mutex
	var cur *wsBarrier

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		barrierMu.Lock()
		b := cur
		barrierMu.Unlock()
		b.arrived <- struct{}{}
		select {
		case <-b.release:
		case <-ctx.Done():
			return
		}
		// Echo loop: write back exactly what was read so receive can match.
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
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"

	for iter := 0; iter < iters; iter++ {
		b := &wsBarrier{arrived: make(chan struct{}, n), release: make(chan struct{})}
		barrierMu.Lock()
		cur = b
		barrierMu.Unlock()

		ex := newWSExecutor()
		ctx, cancel := context.WithCancel(context.Background())

		var wg sync.WaitGroup
		wg.Add(n)
		for i := 0; i < n; i++ {
			go func(i int) {
				defer wg.Done()
				id := fmt.Sprintf("c%d", i)
				// assert (not require) inside a goroutine: it records failure
				// without calling runtime.Goexit, so wg.Done still runs and the
				// barrier wait below is not left deadlocked on a failed worker.
				if !assert.True(t,
					ex.Execute(ctx, types.WSConnectAction{URL: wsURL, ConnectionID: id}).Success(),
					"iter %d: connect %s failed", iter, id) {
					return
				}
				if !assert.True(t,
					ex.Execute(ctx, types.WSSendAction{
						ConnectionID: id, Message: fmt.Sprintf(`{"type":"ping-%d"}`, i),
					}).Success(),
					"iter %d: send %s failed", iter, id) {
					return
				}
				assert.True(t,
					ex.Execute(ctx, types.WSReceiveAction{
						ConnectionID: id, Type: fmt.Sprintf("ping-%d", i), Timeout: 3,
					}).Success(),
					"iter %d: receive %s failed", iter, id)
			}(i)
		}

		// Wait for all N connections to arrive at the barrier (proves they are
		// open concurrently), then release them to echo.
		for i := 0; i < n; i++ {
			select {
			case <-b.arrived:
			case <-time.After(3 * time.Second):
				t.Fatalf("iter %d: only %d/%d connections arrived concurrently (no parallelism)", iter, i, n)
			}
		}
		close(b.release)
		wg.Wait()
		cancel() // tear down every connection (pumps + watchers exit) before next iter
	}
}

// TestWSPumpNoGoroutineLeak proves the read pump (and the ctx-cancel watcher
// store starts alongside it) exits when its owning case context is cancelled —
// no goroutine leak across a batch of connections. It runs N connects (each
// starts a pump + a watcher), cancels every case context, and polls the
// goroutine count back down to near-baseline. A bounded polling wait (not a
// single fixed sleep) keeps it stable on slow/scheduling-loaded hosts.
func TestWSPumpNoGoroutineLeak(t *testing.T) {
	url := newWSTestServer(t, func(conn *websocket.Conn) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for {
			mt, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			_ = conn.Write(ctx, mt, data)
		}
	})

	// Baseline AFTER the server is up so its listener goroutine is already
	// counted; a GC settles any transient runtime goroutines.
	runtime.GC()
	baseline := runtime.NumGoroutine()

	const n = 20
	ex := newWSExecutor()
	cancels := make([]context.CancelFunc, n)
	for i := 0; i < n; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		cancels[i] = cancel
		id := fmt.Sprintf("c%d", i)
		require.True(t, ex.Execute(ctx, types.WSConnectAction{URL: url, ConnectionID: id}).Success(),
			"connect %d failed", i)
	}

	// Sanity: each connect started a pump + a watcher (+ a server-side handler),
	// so the count must have risen well above baseline.
	require.Greater(t, runtime.NumGoroutine(), baseline,
		"pumps/watchers did not start")

	// Cancel every case context. Each watcher closes its conn (pump's Read
	// errors → pump exits) and deletes the entry; the server-side handler exits
	// on the client close. All spawned goroutines should drain back to baseline.
	for _, c := range cancels {
		c()
	}

	// Poll until the count returns to near-baseline, with a bounded deadline.
	// The +2 slack absorbs runtime/scheduler noise; the deadline bounds the wait
	// so a real leak fails fast instead of hanging.
	const slack = 2
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= baseline+slack {
			return // pass
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("goroutine leak: baseline=%d, allowed<=%d, now=%d",
		baseline, baseline+slack, runtime.NumGoroutine())
}

// TestWSReceiveAfterPeerCloseAllConsumersError extends
// TestWSReceiveAfterPeerCloseReturnsError: after the peer closes, EVERY
// subsequent receive on that connection must return the pump error promptly
// (none may hang). Because consumers serialize on readMu, this also confirms the
// closed `done` channel wakes each queued consumer in turn. Run under -race.
func TestWSReceiveAfterPeerCloseAllConsumersError(t *testing.T) {
	peerClosed := make(chan struct{})
	url := newWSTestServer(t, func(conn *websocket.Conn) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		select {
		case <-peerClosed:
		case <-ctx.Done():
			return
		}
		_ = conn.Close(websocket.StatusNormalClosure, "peer done")
	})
	ex := newWSExecutor()
	ctx := context.Background()
	require.True(t, ex.Execute(ctx, types.WSConnectAction{URL: url, ConnectionID: "c1"}).Success(),
		"connect failed")

	// Trigger the peer close; the pump observes it and exits (done closed).
	close(peerClosed)

	// Fire several receives concurrently. They serialize on readMu; each must
	// observe the pump exit and return an error (not hang).
	const n = 4
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() {
			r := ex.Execute(ctx, types.WSReceiveAction{ConnectionID: "c1", Type: "anything", Timeout: 5})
			if r.Success() {
				errs <- fmt.Errorf("receive after peer close should fail, got success: %+v", r)
				return
			}
			errs <- nil
		}()
	}
	for i := 0; i < n; i++ {
		select {
		case e := <-errs:
			assert.NoError(t, e)
		case <-time.After(3 * time.Second):
			t.Fatal("receive hung after peer close (pump exit not observed by all consumers)")
		}
	}
}

func TestWSReceiveAliasesMatch(t *testing.T) {
	url := newWSTestServer(t, func(conn *websocket.Conn) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		// Server emits the BATCHED form, not the primary the client awaits. Write
		// it right after accept; the client's read pump buffers it whenever it
		// arrives, so no handshake/read ordering is needed.
		_ = conn.Write(ctx, websocket.MessageText,
			[]byte(`{"type":"session:output-batch","payload":{"lines":["x"]}}`))
		_, _, _ = conn.Read(ctx) // block until close
	})
	ex := newWSExecutor()
	ctx := context.Background()
	_ = ex.Execute(ctx, types.WSConnectAction{URL: url, ConnectionID: "c1"}) // establish
	res := ex.Execute(ctx, types.WSReceiveAction{
		ConnectionID: "c1", Type: "session:output", Aliases: []string{"session:output-batch"},
		Assert: map[string]any{"payload.lines": []any{"x"}}, Timeout: 2,
	})
	require.True(t, res.Success(), "receive should match the alias type; got %+v", res)
}
