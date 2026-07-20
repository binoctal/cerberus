package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"go.uber.org/zap"

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
		defer conn.Close(websocket.StatusNormalClosure, "")
		handler(conn)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
}

func newWSExecutor() *WebSocketExecutor {
	return NewWebSocketExecutor(zap.NewNop())
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
		select { case seen <- r.Header.Get("X-Test-Auth"): default: }
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("accept: %v", err)
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, _, _ = conn.Read(ctx)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"

	ex := newWSExecutor()
	res := ex.Execute(context.Background(), types.WSConnectAction{
		URL:           url,
		ConnectionID:  "c1",
		Headers:       map[string]string{"X-Test-Auth": "secret"},
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
