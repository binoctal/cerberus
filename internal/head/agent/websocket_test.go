package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
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
