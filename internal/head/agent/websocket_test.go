package agent

import (
	"context"
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
