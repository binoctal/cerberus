package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
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
