package agent

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/project"
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
	conn     *websocket.Conn
	ctx      context.Context   // per-case ctx; cancellation closes the conn
	readMu   sync.Mutex        // serializes concurrent Reads on this conn
	protocol *project.Protocol // service protocol resolved at connect; nil = M0
}

// WebSocketExecutor handles persistent WebSocket connections and primitives.
// It is a singleton shared across parallel cases (spec D3), so auto-generated
// connection ids draw from a monotonic counter to avoid collisions.
type WebSocketExecutor struct {
	logger *zap.Logger
	mu     sync.RWMutex
	conns  map[string]*wsEntry
	seq    uint64
	idx    *WSProtocolIndex
}

// NewWebSocketExecutor creates a WebSocket executor. A nil idx preserves M0
// behavior (top-level "type" matching, no auto auth injection).
func NewWebSocketExecutor(logger *zap.Logger, idx *WSProtocolIndex) *WebSocketExecutor {
	return &WebSocketExecutor{logger: logger, conns: make(map[string]*wsEntry), idx: idx}
}

// resolveProtocol returns the declared protocol for a dial URL's host, or nil
// when none is declared (M0 behavior). A nil index short-circuits to the M0
// path without a map lookup.
func (e *WebSocketExecutor) resolveProtocol(rawURL string) *project.Protocol {
	if e.idx == nil {
		return nil
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil
	}
	return e.idx.ByHost[u.Host]
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

func (e *WebSocketExecutor) store(id string, conn *websocket.Conn, ctx context.Context, proto *project.Protocol) {
	e.mu.Lock()
	e.conns[id] = &wsEntry{conn: conn, ctx: ctx, protocol: proto}
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

func (e *WebSocketExecutor) lookup(id string) (*wsEntry, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	entry, ok := e.conns[id]
	return entry, ok
}

func (e *WebSocketExecutor) doConnect(ctx context.Context, a types.WSConnectAction, start time.Time) types.ExecutorResult {
	// Resolve the protocol for this dial URL's host once, before building
	// opts / dialing. When the protocol declares auth, the executor is
	// authoritative over the LLM: any caller-supplied value at auth.param is
	// stripped and the resolved raw token is injected in its place (exactly
	// one correct credential reaches the server). doReceive reads the
	// stashed proto back to switch to the declared type_path.
	proto := e.resolveProtocol(a.URL)
	opts := &websocket.DialOptions{}
	headers := http.Header{}
	for k, v := range a.Headers {
		headers.Set(k, v)
	}
	opts.HTTPHeader = headers
	if len(a.Subprotocols) > 0 {
		opts.Subprotocols = a.Subprotocols
	}
	dialURL := wsURL(a.URL)
	// preInjectionURL is the secret-free url returned in WSResult. With auth
	// it is computed from the pre-injection dial url (query stripped of
	// auth.param); without auth it is just the caller-supplied url.
	var preInjectionURL string
	if proto != nil && proto.Auth != nil {
		var authErr error
		dialURL, preInjectionURL, authErr = e.injectAuth(ctx, dialURL, a, proto.Auth, opts)
		if authErr != nil {
			// Auth-resolution failure: strip the declared param from the
			// echoed url, symmetric with the dial-error and success paths.
			// Without this, an LLM-supplied value at a custom param name
			// not in the default redaction denylist would leak via WSResult.URL.
			return types.WSResult{OK: false, URL: stripQuery(a.URL, proto.Auth.Param), Err: authErr.Error(), Latency: time.Since(start)}
		}
	} else {
		preInjectionURL = a.URL
	}
	conn, _, err := websocket.Dial(ctx, dialURL, opts)
	if err != nil {
		return types.WSResult{OK: false, URL: preInjectionURL, Err: err.Error(), Latency: time.Since(start)}
	}
	id := a.ConnectionID
	if id == "" {
		id = fmt.Sprintf("ws-%d", atomic.AddUint64(&e.seq, 1))
	}
	// Namespace the table key by caseID so parallel cases passing the same
	// LLM-supplied connection_id do not collide. The user-facing id (returned
	// in any future result field) remains the un-namespaced id.
	key := caseNamespace(ctx, id)
	e.store(key, conn, ctx, proto)
	return types.WSResult{OK: true, URL: preInjectionURL, Latency: time.Since(start)}
}

// injectAuth resolves the declared credential, strips any existing value at
// auth.param, and injects the resolved value. It returns the dial url (which
// may carry the secret, depending on strategy) and the pre-injection url
// (without the secret) for WSResult. The action's CredentialRef, when set,
// overrides the protocol's. url.QueryEscape is applied automatically by
// url.Values.Encode for the query strategy.
func (e *WebSocketExecutor) injectAuth(ctx context.Context, dialURL string, a types.WSConnectAction, auth *project.ProtocolAuth, opts *websocket.DialOptions) (string, string, error) {
	actor := auth.CredentialRef
	if a.CredentialRef != "" {
		actor = a.CredentialRef
	}
	token, ok := e.tokenFor(actor)
	if !ok {
		return "", "", fmt.Errorf("ws auth: no token for actor %q", actor)
	}
	switch auth.Strategy {
	case "query":
		u, err := url.Parse(dialURL)
		if err != nil {
			return "", "", fmt.Errorf("ws auth: bad url: %w", err)
		}
		q := u.Query()
		q.Del(auth.Param) // strip any LLM-supplied value
		q.Set(auth.Param, token)
		u.RawQuery = q.Encode()
		return u.String(), stripQuery(dialURL, auth.Param), nil
	case "header":
		if opts.HTTPHeader == nil {
			opts.HTTPHeader = http.Header{}
		}
		opts.HTTPHeader.Del(auth.Param)
		opts.HTTPHeader.Set(auth.Param, token)
		return dialURL, dialURL, nil
	case "subprotocol":
		opts.Subprotocols = removeString(opts.Subprotocols, auth.Param)
		opts.Subprotocols = append(opts.Subprotocols, token)
		return dialURL, dialURL, nil
	}
	return "", "", fmt.Errorf("ws auth: unknown strategy %q", auth.Strategy)
}

// tokenFor returns the cached raw token for actor and a boolean indicating
// whether a non-empty token was found. A nil index returns ("", false).
func (e *WebSocketExecutor) tokenFor(actor string) (string, bool) {
	if e.idx == nil {
		return "", false
	}
	t, ok := e.idx.ActorTokens[actor]
	return t, ok && t != ""
}

// stripQuery returns rawURL with the named query parameter removed. It is the
// pre-injection url used for WSResult (no secret). Parse failures fall back to
// the input verbatim so a malformed url never becomes a security surface.
func stripQuery(rawURL, param string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	q := u.Query()
	q.Del(param)
	u.RawQuery = q.Encode()
	return u.String()
}

// removeString returns ss with all occurrences of s removed. It reuses the
// underlying array, so callers must not retain the original slice.
func removeString(ss []string, s string) []string {
	out := ss[:0]
	for _, x := range ss {
		if x != s {
			out = append(out, x)
		}
	}
	return out
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
	entry, ok := e.lookup(caseNamespace(ctx, a.ConnectionID))
	if !ok {
		return types.WSResult{OK: false, Err: fmt.Sprintf("unknown connection_id: %s", a.ConnectionID), Latency: time.Since(start)}
	}
	conn := entry.conn
	writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := conn.Write(writeCtx, websocket.MessageText, []byte(a.Message)); err != nil {
		return types.WSResult{OK: false, Err: fmt.Sprintf("write: %v", err), Latency: time.Since(start)}
	}
	return types.WSResult{OK: true, Latency: time.Since(start)}
}

func (e *WebSocketExecutor) doReceive(ctx context.Context, a types.WSReceiveAction, start time.Time) types.ExecutorResult {
	entry, ok := e.lookup(caseNamespace(ctx, a.ConnectionID))
	if !ok {
		return types.WSResult{OK: false, Err: fmt.Sprintf("unknown connection_id: %s", a.ConnectionID), Latency: time.Since(start)}
	}
	// coder/websocket forbids concurrent Read on one conn (it would corrupt
	// the frame stream). Serialize Reads per connection via readMu — different
	// connections still run in parallel because each entry has its own mutex.
	entry.readMu.Lock()
	defer entry.readMu.Unlock()
	conn, connCtx := entry.conn, entry.ctx
	// type_path selects the routing key for this connection's protocol. Empty
	// (no protocol, or protocol with no type_path) falls back to M0's
	// top-level "type" field via extractTypePath's default.
	path := "type"
	if entry.protocol != nil && entry.protocol.TypePath != "" {
		path = entry.protocol.TypePath
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
		if t, ok := extractTypePath(data, path); ok && t == a.Type {
			return types.WSResult{OK: true, MatchedMessage: string(data), SeenMessages: seen, Latency: time.Since(start)}
		}
		seen = append(seen, string(data))
	}
}
