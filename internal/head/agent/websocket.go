package agent

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"sort"
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
	// Resolve role + effective credential_ref + discriminator params. `role`
	// stays in scope so the handshake block (Task 5) can read role.Handshake.
	var role *project.ProtocolRole
	var roleParams map[string]string
	var credentialRef string
	if a.Role != "" {
		if proto != nil {
			role = proto.Roles[a.Role]
		}
		if role == nil {
			// Unknown role: fail before dial. The echoed url strips the
			// auth.param slot (if any) so no LLM-supplied secret leaks.
			return types.WSResult{OK: false, URL: stripQuery(a.URL, maybeAuthParam(proto)), Err: fmt.Sprintf("ws connect: unknown role %q", a.Role), Latency: time.Since(start)}
		}
		credentialRef = role.CredentialRef
		roleParams = role.Params
	}
	if credentialRef == "" {
		credentialRef = a.CredentialRef
	}
	if credentialRef == "" && proto != nil && proto.Auth != nil {
		credentialRef = proto.Auth.CredentialRef
	}
	// preInjectionURL is the secret-free url returned in WSResult. With auth
	// it is computed from the pre-injection dial url (query stripped of
	// auth.param); without auth it is just the caller-supplied url.
	var preInjectionURL string
	if proto != nil && proto.Auth != nil {
		var authErr error
		dialURL, preInjectionURL, authErr = e.injectAuth(ctx, dialURL, credentialRef, proto.Auth, opts)
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
	// Role discriminator params (strip-then-inject onto the dial url).
	for k, v := range roleParams {
		dialURL = setQueryParam(dialURL, k, v)
	}
	// After role-param injection, recompute preInjectionURL from the final dial
	// url so the result reflects what was actually dialed (role params present,
	// token stripped via auth.param). No-op for non-role connects.
	if len(roleParams) > 0 {
		if ap := maybeAuthParam(proto); ap != "" {
			preInjectionURL = stripQuery(dialURL, ap)
		} else {
			preInjectionURL = dialURL
		}
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
	// Auto-handshake (role with handshake declared). The handshake is
	// non-decisive: a match means "connection ready", not "case passed", so
	// ws_connect stays an intermediate step. The Read loop is guarded by the
	// entry's readMu alone (mirroring doReceive); only the timeout cleanup
	// takes e.mu, never both at once. The Read runs against a fresh timeout
	// context derived from role.Handshake.Timeout, independent of the case ctx.
	var seen []string
	if role != nil && role.Handshake != nil {
		hsTimeout := time.Duration(role.Handshake.Timeout) * time.Second
		hsCtx, hsCancel := context.WithTimeout(ctx, hsTimeout)
		entry, ok := e.lookup(key)
		if !ok {
			hsCancel()
			return types.WSResult{OK: false, URL: preInjectionURL, Err: "ws handshake: connection vanished", Latency: time.Since(start)}
		}
		entry.readMu.Lock()
		matched := false
		hsFraming := framingOf(entry)
		path := "type"
		if proto.TypePath != "" {
			path = proto.TypePath
		}
		for {
			_, data, rerr := entry.conn.Read(hsCtx)
			if rerr != nil {
				break // timeout or peer close
			}
			seen = append(seen, frameForResult(hsFraming, data))
			if matchType(hsFraming, data, role.Handshake.AwaitType, path) {
				matched = true
				break
			}
		}
		entry.readMu.Unlock()
		hsCancel()
		if !matched {
			// Mandatory handshake did not complete: close + remove the
			// connection so a subsequent ws_send on this id fails as unknown.
			e.mu.Lock()
			if ent, ok := e.conns[key]; ok {
				_ = ent.conn.Close(websocket.StatusNormalClosure, "handshake timeout")
				delete(e.conns, key)
			}
			e.mu.Unlock()
			return types.WSResult{OK: false, URL: preInjectionURL, Err: fmt.Sprintf("ws handshake: timed out awaiting %q", role.Handshake.AwaitType), SeenMessages: seen, Latency: time.Since(start)}
		}
	}
	return types.WSResult{OK: true, URL: preInjectionURL, SeenMessages: seen, Latency: time.Since(start)}
}

// injectAuth resolves the declared credential for the already-resolved actor,
// strips any existing value at auth.param, and injects the resolved value. It
// returns the dial url (which may carry the secret, depending on strategy) and
// the pre-injection url (without the secret) for WSResult. The caller picks the
// authoritative credential_ref (protocol default, action override, or role) and
// passes it as `actor`. url.QueryEscape is applied automatically by
// url.Values.Encode for the query strategy.
func (e *WebSocketExecutor) injectAuth(ctx context.Context, dialURL string, actor string, auth *project.ProtocolAuth, opts *websocket.DialOptions) (string, string, error) {
	token, ok := e.tokenFor(actor)
	if !ok {
		return "", "", fmt.Errorf("ws auth: no token for actor %q", actor)
	}
	switch auth.Strategy {
	case "query":
		// M1 guard: explicit bad-url early-fail. setQueryParam/stripQuery fall
		// back to the raw url on parse error, so without this guard a malformed
		// dial url would silently slip through to websocket.Dial and surface as
		// a less-specific dial error. Restoring the guard is strict improvement.
		if _, err := url.Parse(dialURL); err != nil {
			return "", "", fmt.Errorf("ws auth: bad url: %w", err)
		}
		return setQueryParam(dialURL, auth.Param, token), stripQuery(dialURL, auth.Param), nil
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

// maybeAuthParam returns the protocol's auth.param (the token slot to strip
// from echoed urls) or "" when there is no declared auth. Used by failure-path
// url scrubbing and post-role-param preInjectionURL recompute.
func maybeAuthParam(p *project.Protocol) string {
	if p != nil && p.Auth != nil {
		return p.Auth.Param
	}
	return ""
}

// setQueryParam removes any existing key then sets it to val on the url's query
// string, returning the rewritten url. Falls back to rawURL on parse error so a
// malformed url never becomes a security surface.
func setQueryParam(rawURL, key, val string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	q := u.Query()
	q.Del(key)
	q.Set(key, val)
	u.RawQuery = q.Encode()
	return u.String()
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

// checkAsserts evaluates field-level assertions against data in sorted path
// order (deterministic error reporting). On the first failure it returns the
// path, expected value, and actual ("<missing>" for an absent key); otherwise
// ok=true. Empty asserts is a no-op (M1 behavior).
func checkAsserts(data []byte, asserts map[string]any) (path string, expected, actual any, ok bool) {
	if len(asserts) == 0 {
		return "", nil, nil, true
	}
	paths := make([]string, 0, len(asserts))
	for k := range asserts {
		paths = append(paths, k)
	}
	sort.Strings(paths)
	for _, p := range paths {
		exp := asserts[p]
		got, found := extractPath(data, p)
		if !found {
			return p, exp, "<missing>", false
		}
		if !valueEqual(got, exp) {
			return p, exp, got, false
		}
	}
	return "", nil, nil, true
}

// valueEqual reports whether actual equals expected, with numeric
// normalization: JSON decodes all numbers to float64, so an expected integer 5
// and an actual float64 5 compare equal. Other types use reflect.DeepEqual.
func valueEqual(actual, expected any) bool {
	if af, ok := numericFloat(actual); ok {
		if bf, ok := numericFloat(expected); ok {
			return af == bf
		}
	}
	return reflect.DeepEqual(actual, expected)
}

// numericFloat returns v as a float64 when it is a JSON/YAML numeric type.
func numericFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int32:
		return float64(x), true
	case int64:
		return float64(x), true
	}
	return 0, false
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
	// Binary framing: the message is base64-encoded bytes; decode and write a
	// binary frame. json/text framing: write the message string as a text frame
	// (json and text sends are byte-identical on the wire).
	if framingOf(entry) == "binary" {
		decoded, err := base64.StdEncoding.DecodeString(a.Message)
		if err != nil {
			return types.WSResult{OK: false, Err: "send: message is not valid base64", Latency: time.Since(start)}
		}
		if err := conn.Write(writeCtx, websocket.MessageBinary, decoded); err != nil {
			return types.WSResult{OK: false, Err: fmt.Sprintf("write: %v", err), Latency: time.Since(start)}
		}
		return types.WSResult{OK: true, Latency: time.Since(start)}
	}
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
	framing := framingOf(entry)
	// type_path selects the routing key for json-framed protocols. Unused under
	// text/binary (matched by whole-frame equality) but read so the json path
	// stays identical to M1.
	path := "type"
	if entry.protocol != nil && entry.protocol.TypePath != "" {
		path = entry.protocol.TypePath
	}
	// assert path-walks JSON, so it is defined only for json framing. Under
	// text/binary the exact-match type already pins the frame; an assert is a
	// case-authoring error caught here (no read, no dial effect).
	if framing != "" && framing != "json" && len(a.Assert) > 0 {
		return types.WSResult{OK: false, Err: "receive: assert requires json framing", Latency: time.Since(start)}
	}
	// A binary type that is not valid base64 can never match; fail fast with a
	// clear error instead of waiting out the timeout.
	if framing == "binary" {
		if _, err := base64.StdEncoding.DecodeString(a.Type); err != nil {
			return types.WSResult{OK: false, Err: "receive: type is not valid base64", Latency: time.Since(start)}
		}
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
		if matchType(framing, data, a.Type, path) {
			// Matched. For json framing, evaluate field-level assertions (if any)
			// in sorted path order; the first failure fails the receive with a
			// precise message. The matched frame is evidence either way. No
			// asserts (or non-json framing) → arrival-only success.
			if framing == "" || framing == "json" {
				if p, exp, act, ok := checkAsserts(data, a.Assert); !ok {
					return types.WSResult{
						OK:             false,
						Err:            fmt.Sprintf("receive: assert %s: expected %v, got %v", p, exp, act),
						MatchedMessage: frameForResult(framing, data),
						SeenMessages:   seen,
						Latency:        time.Since(start),
					}
				}
			}
			return types.WSResult{OK: true, MatchedMessage: frameForResult(framing, data), SeenMessages: seen, Latency: time.Since(start)}
		}
		seen = append(seen, frameForResult(framing, data))
	}
}
