package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/types"

	"github.com/coder/websocket"
)

// caseIDKey is the per-case identifier carried on the per-case context, used to
// namespace connection-table keys so parallel cases cannot collide on a shared
// LLM-supplied connection_id.
type caseIDKey struct{}

// caseNamespace reads the caseID from ctx (defaulting to "_default" when absent,
// e.g. in unit tests that use context.Background()) and returns the namespaced
// connection-table key: <caseID>:<connectionID>.
func caseNamespace(ctx context.Context, connectionID string) string {
	v, _ := ctx.Value(caseIDKey{}).(string)
	if v == "" {
		v = "_default"
	}
	return v + ":" + connectionID
}

// wsEntry is a persisted WebSocket connection owned by a case context.
type wsEntry struct {
	conn *websocket.Conn
	ctx  context.Context // per-case ctx; cancellation closes the conn
}

// WebSocketExecutor handles persistent WebSocket connections and primitives.
// It is a singleton shared across parallel cases (spec D3), so auto-generated
// connection ids draw from a monotonic counter to avoid collisions.
type WebSocketExecutor struct {
	logger *zap.Logger
	mu     sync.RWMutex
	conns  map[string]*wsEntry
	seq    uint64
}

// NewWebSocketExecutor creates a WebSocket executor.
func NewWebSocketExecutor(logger *zap.Logger) *WebSocketExecutor {
	return &WebSocketExecutor{logger: logger, conns: make(map[string]*wsEntry)}
}

// Execute dispatches WebSocket actions.
func (e *WebSocketExecutor) Execute(ctx context.Context, action types.TypedAction) types.ExecutorResult {
	start := time.Now()
	switch a := action.(type) {
	case types.WSConnectAction:
		return e.doConnect(ctx, a, start)
	case types.WSSendAction:
		return e.doSend(ctx, a, start)
	case types.WSReceiveAction:
		return e.doReceive(ctx, a, start)
	case types.WSDisconnectAction:
		return e.doDisconnect(ctx, a, start)
	default:
		return types.ErrorResult{Err: fmt.Sprintf("ws executor: unsupported action %T", action)}
	}
}

// wsURL converts http(s) URLs to ws(s).
func wsURL(u string) string {
	u = strings.Replace(u, "https://", "wss://", 1)
	u = strings.Replace(u, "http://", "ws://", 1)
	return u
}

func (e *WebSocketExecutor) store(id string, conn *websocket.Conn, ctx context.Context) {
	e.mu.Lock()
	e.conns[id] = &wsEntry{conn: conn, ctx: ctx}
	e.mu.Unlock()
	// Close the connection when the owning case context is cancelled.
	go func() {
		<-ctx.Done()
		_ = conn.Close(websocket.StatusNormalClosure, "ctx done")
		e.mu.Lock()
		delete(e.conns, id)
		e.mu.Unlock()
	}()
}

func (e *WebSocketExecutor) lookup(id string) (*websocket.Conn, context.Context, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	entry, ok := e.conns[id]
	if !ok {
		return nil, nil, false
	}
	return entry.conn, entry.ctx, true
}

func (e *WebSocketExecutor) doConnect(ctx context.Context, a types.WSConnectAction, start time.Time) types.ExecutorResult {
	opts := &websocket.DialOptions{}
	headers := http.Header{}
	for k, v := range a.Headers {
		headers.Set(k, v)
	}
	opts.HTTPHeader = headers
	if len(a.Subprotocols) > 0 {
		opts.Subprotocols = a.Subprotocols
	}
	conn, _, err := websocket.Dial(ctx, wsURL(a.URL), opts)
	if err != nil {
		return types.WSResult{OK: false, URL: a.URL, Err: err.Error(), Latency: time.Since(start)}
	}
	id := a.ConnectionID
	if id == "" {
		id = fmt.Sprintf("ws-%d", atomic.AddUint64(&e.seq, 1))
	}
	// Namespace the table key by caseID so parallel cases passing the same
	// LLM-supplied connection_id do not collide. The user-facing id (returned
	// in any future result field) remains the un-namespaced id.
	key := caseNamespace(ctx, id)
	e.store(key, conn, ctx)
	return types.WSResult{OK: true, URL: a.URL, Latency: time.Since(start)}
}

func (e *WebSocketExecutor) doDisconnect(ctx context.Context, a types.WSDisconnectAction, start time.Time) types.ExecutorResult {
	key := caseNamespace(ctx, a.ConnectionID)
	e.mu.Lock()
	entry, ok := e.conns[key]
	if ok {
		_ = entry.conn.Close(websocket.StatusNormalClosure, "disconnect")
		delete(e.conns, key)
	}
	e.mu.Unlock()
	if !ok {
		return types.WSResult{OK: false, Err: fmt.Sprintf("unknown connection_id: %s", a.ConnectionID), Latency: time.Since(start)}
	}
	return types.WSResult{OK: true, Latency: time.Since(start)}
}

func (e *WebSocketExecutor) doSend(ctx context.Context, a types.WSSendAction, start time.Time) types.ExecutorResult {
	conn, _, ok := e.lookup(caseNamespace(ctx, a.ConnectionID))
	if !ok {
		return types.WSResult{OK: false, Err: fmt.Sprintf("unknown connection_id: %s", a.ConnectionID), Latency: time.Since(start)}
	}
	writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := conn.Write(writeCtx, websocket.MessageText, []byte(a.Message)); err != nil {
		return types.WSResult{OK: false, Err: fmt.Sprintf("write: %v", err), Latency: time.Since(start)}
	}
	return types.WSResult{OK: true, Latency: time.Since(start)}
}

// messageType reads the top-level "type" field of a JSON text message.
// Returns ("", false) for non-JSON or non-object messages.
func messageType(data []byte) (string, bool) {
	var probe struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return "", false
	}
	return probe.Type, true
}

func (e *WebSocketExecutor) doReceive(ctx context.Context, a types.WSReceiveAction, start time.Time) types.ExecutorResult {
	conn, connCtx, ok := e.lookup(caseNamespace(ctx, a.ConnectionID))
	if !ok {
		return types.WSResult{OK: false, Err: fmt.Sprintf("unknown connection_id: %s", a.ConnectionID), Latency: time.Since(start)}
	}
	timeout := time.Duration(a.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	readCtx, cancel := context.WithTimeout(connCtx, timeout)
	defer cancel()

	var seen []string
	for {
		_, data, err := conn.Read(readCtx)
		if err != nil {
			// Peer close or timeout: no matching message arrived.
			return types.WSResult{OK: false, Err: fmt.Sprintf("receive: %v", err), SeenMessages: seen, Latency: time.Since(start)}
		}
		if t, _ := messageType(data); t == a.Type {
			return types.WSResult{OK: true, MatchedMessage: string(data), SeenMessages: seen, Latency: time.Since(start)}
		}
		seen = append(seen, string(data))
	}
}
