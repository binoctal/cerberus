package agent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"slices"
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

// wsMsg is one inbound WebSocket frame buffered by the read pump.
type wsMsg struct {
	data   []byte
	binary bool
}

// wsEntry is a persisted WebSocket connection owned by a case context. The read
// pump goroutine owns conn.Read (single reader); consumers (handshake/receive)
// drain the msgs channel under readMu — they never call conn.Read directly, so
// concurrent reads are structurally impossible.
type wsEntry struct {
	conn       *websocket.Conn
	ctx        context.Context   // per-case ctx; cancellation closes the conn
	protocol   *project.Protocol // service protocol resolved at connect; nil = M0
	msgs       chan wsMsg        // buffered (256); pump pushes every inbound frame
	pumpErr    error             // set when the pump exits (read error / ctx done)
	done       chan struct{}     // closed when the pump has exited
	readMu     sync.Mutex        // serializes channel consumption (one consumer at a time)
	pending    wsMsg             // a frame peeked then put back by readMatchingAll
	hasPending bool              // pending holds a frame when true
}

// matchAllGrace is the idle gap that ends a MatchAll burst. It covers the pump's
// atomic batch push race (the consumer may grab item #1 mid-push and see an
// instantaneously-empty channel before #2 lands) and defines the burst boundary
// for back-to-back same-type batches. Every MatchAll receive pays one grace.
const matchAllGrace = 10 * time.Millisecond

// popBuffered returns the next already-buffered frame WITHOUT blocking: the
// pushback slot first (set by readMatchingAll when it ended a burst on a
// non-match), then one non-blocking channel read. Returns (zero, false) when
// nothing is buffered. Preserves frame order across pushback.
func (entry *wsEntry) popBuffered() (wsMsg, bool) {
	if entry.hasPending {
		m := entry.pending
		entry.hasPending = false
		entry.pending = wsMsg{}
		return m, true
	}
	select {
	case m := <-entry.msgs:
		return m, true
	default:
		return wsMsg{}, false
	}
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
	entry := &wsEntry{
		conn:     conn,
		ctx:      ctx,
		protocol: proto,
		msgs:     make(chan wsMsg, 256),
		done:     make(chan struct{}),
	}
	e.mu.Lock()
	e.conns[id] = entry
	e.mu.Unlock()
	// Read pump: a single goroutine owns conn.Read for the life of the entry.
	// It reads with entry.ctx only (NO timeout context) so a consumer's read
	// deadline never reaches conn.Read and therefore never closes the connection
	// — the coder/websocket read-timeout-closes-conn bug this pump fixes.
	// Consumers (handshake/receive) drain msgs under readMu with their own timeout.
	go entry.readPump()
	// Close the connection when the owning case context is cancelled.
	go func() {
		<-ctx.Done()
		_ = conn.Close(websocket.StatusNormalClosure, "ctx done")
		e.mu.Lock()
		delete(e.conns, id)
		e.mu.Unlock()
	}()
}

// readPump is the single reader for this connection. It owns conn.Read for the
// lifetime of the entry: NO other goroutine may call conn.Read (it would
// corrupt the frame stream). It reads with entry.ctx only — never a timeout
// context — so a frame arrival or ctx cancellation is the only thing that
// unblocks it; a consumer's timeout cannot close the connection. Each frame is
// pushed to the buffered msgs channel (back-pressure: the pump blocks on send
// when full, never drops). On read error (peer close or ctx cancel) it sets
// pumpErr and closes done so consumers detect the dead connection. Lifecycle
// closure stays with the ctx-cancel cleanup goroutine + doDisconnect.
func (entry *wsEntry) readPump() {
	defer close(entry.done)
	for {
		mt, data, err := entry.conn.Read(entry.ctx)
		if err != nil {
			entry.pumpErr = err
			return
		}
		// Batch decomposition happens here (before pushing) so every consumer —
		// handshake auto-await, ws_receive, decisive verdict, evidence — sees the
		// expanded item frames with no per-callsite change. expandBatch returns the
		// N item frames for a declared json batch, or the single original frame
		// otherwise (non-json, no protocol, non-batch type, missing/empty array).
		binary := mt == websocket.MessageBinary
		for _, m := range expandBatch(entry.protocol, data, binary) {
			select {
			case entry.msgs <- m:
			case <-entry.ctx.Done():
				// ctx cancel while the channel is full: record the cause so a
				// racing consumer doesn't format a misleading "receive: <nil>".
				entry.pumpErr = entry.ctx.Err()
				return
			}
		}
	}
}

// expandBatch decomposes a json batch frame into N synthetic item frames per
// the connection protocol's batch declarations. Returns the frames to push: the
// N items when the frame is a declared batch (json framing, array at
// items_path), otherwise a single-element slice holding the original frame
// (non-json, no protocol, non-batch type, or missing/empty/non-array items_path
// → degrade by passing the batch through unexpanded). The pump pushes whatever
// it returns, in order. Single-level: an item_type that is itself a batch is
// rejected at validation, so no recursion here.
func expandBatch(proto *project.Protocol, data []byte, binary bool) []wsMsg {
	original := []wsMsg{{data: data, binary: binary}}
	if binary || proto == nil || len(proto.Batches) == 0 {
		return original
	}
	if framing := proto.Framing; framing != "" && framing != "json" {
		return original
	}
	typePath := proto.TypePath
	if typePath == "" {
		typePath = "type"
	}
	rt, ok := extractTypePath(data, typePath)
	if !ok {
		return original
	}
	batch, ok := proto.Batches[rt]
	if !ok || batch == nil {
		return original
	}
	items, ok := extractArray(data, batch.ItemsPath)
	if !ok || len(items) == 0 {
		// Missing/non-array/empty items path: nothing to expand; pass the batch
		// frame through so a receive can still match it as a whole (alias path).
		return original
	}
	out := make([]wsMsg, 0, len(items))
	for _, it := range items {
		synth, err := json.Marshal(map[string]any{"type": batch.ItemType, "payload": it})
		if err != nil {
			return original // marshal failure: degrade rather than drop the frame
		}
		out = append(out, wsMsg{data: synth, binary: false})
	}
	return out
}

// readMatching consumes frames from entry's pump until one satisfies match, the
// timeout elapses, or the pump exits. It is the single consumer entry point:
// entry.readMu serializes consumption so at most one consumer drains msgs per
// connection at a time (preserving today's per-conn receive serialization).
//
// Returns the matched frame (valid only when status == "matched"), the
// human-readable non-matching frames seen, and a status:
//   - "matched": a frame satisfied match.
//   - "timeout": no match before the deadline; the connection is STILL ALIVE
//     (the pump keeps running). The caller returns OK:false without closing the
//     conn, so a later send/receive on the same connection_id can succeed.
//   - "closed": the pump exited (peer close / ctx cancel); entry.pumpErr holds
//     the cause.
func readMatching(entry *wsEntry, match func(wsMsg) bool, timeout time.Duration) (matched wsMsg, seen []string, status string) {
	entry.readMu.Lock()
	defer entry.readMu.Unlock()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		// Drain already-buffered frames (pushback slot first) without blocking.
		for {
			m, ok := entry.popBuffered()
			if !ok {
				break
			}
			if match(m) {
				return m, seen, "matched"
			}
			seen = append(seen, frameForResult(framingOf(entry), m.data))
		}
		// No buffered match; wait for a new frame, the timeout, or pump exit.
		select {
		case m := <-entry.msgs:
			if match(m) {
				return m, seen, "matched"
			}
			seen = append(seen, frameForResult(framingOf(entry), m.data))
		case <-timer.C:
			return matched, seen, "timeout"
		case <-entry.done:
			return matched, seen, "closed"
		}
	}
}

// readMatchingAll is the MatchAll variant: it collects EVERY consecutive
// matching frame in the arrival burst rather than stopping at the first. It
// shares readMu with readMatching (one consumer at a time). Semantics:
//
//   - Phase 1 (first match): drain buffered + block up to timeout for the first
//     matching frame. Non-matching frames seen before it are recorded in seen
//     (same loss semantics as readMatching). Timeout/closed before any match →
//     that status with no matched frames.
//   - Phase 2 (burst): drain consecutive matching frames non-blocking. A
//     non-matching frame ENDS the burst and is pushed back (pending) so the next
//     consumer receives it. When the buffer is empty, wait one matchAllGrace for
//     another frame: match ⇒ keep collecting; non-match ⇒ pushback + done;
//     grace/timeout/pump-exit ⇒ done with what was collected.
//
// Returns all matched frames (valid when status == "matched"), the non-matching
// frames seen before the first match, and a status ("matched"/"timeout"/"closed").
func readMatchingAll(entry *wsEntry, match func(wsMsg) bool, timeout time.Duration) (matched []wsMsg, seen []string, status string) {
	entry.readMu.Lock()
	defer entry.readMu.Unlock()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	render := func(m wsMsg) string { return frameForResult(framingOf(entry), m.data) }
	// Phase 1: reach the first matching frame.
	for {
		// Drain buffered (pushback first) non-blocking; record non-matches as seen.
		for {
			m, ok := entry.popBuffered()
			if !ok {
				break
			}
			if match(m) {
				matched = append(matched, m)
				goto burst
			}
			seen = append(seen, render(m))
		}
		// No buffered match; block for one frame, the timeout, or pump exit.
		select {
		case m := <-entry.msgs:
			if match(m) {
				matched = append(matched, m)
				goto burst
			}
			seen = append(seen, render(m))
		case <-timer.C:
			return nil, seen, "timeout"
		case <-entry.done:
			return nil, seen, "closed"
		}
	}
burst:
	// Phase 2: collect the rest of the burst.
	for {
		// Drain consecutive matching frames non-blocking; pushback ends the burst.
		for {
			m, ok := entry.popBuffered()
			if !ok {
				break
			}
			if match(m) {
				matched = append(matched, m)
				continue
			}
			entry.pending = m
			entry.hasPending = true
			return matched, seen, "matched"
		}
		// Buffer empty; grace-wait for one more frame.
		grace := time.NewTimer(matchAllGrace)
		select {
		case m := <-entry.msgs:
			grace.Stop()
			if match(m) {
				matched = append(matched, m)
				// Loop: drain any more that arrived behind it.
			} else {
				entry.pending = m
				entry.hasPending = true
				return matched, seen, "matched"
			}
		case <-grace.C:
			return matched, seen, "matched"
		case <-timer.C:
			return matched, seen, "matched"
		case <-entry.done:
			return matched, seen, "matched"
		}
	}
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
	// Role discriminator headers and subprotocols (strip-then-inject). Guarded
	// on role != nil because role is only resolved when a.Role != ""; without a
	// role there is nothing to inject. roleParams above is a nil map in that
	// case (range is a no-op), but role.Headers would deref a nil pointer.
	if role != nil {
		// Headers: remove any LLM-supplied value at this key, then set the
		// role's. opts.HTTPHeader already carries a.Headers, so this normalizes
		// to exactly the role's value. Headers never appear in WSResult.URL, so
		// preInjectionURL is unaffected.
		for k, v := range role.Headers {
			opts.HTTPHeader.Del(k)
			opts.HTTPHeader.Set(k, v)
		}
		// Subprotocols: remove any LLM-supplied entry at this name, then append
		// the role's (exactly one offer reaches the server).
		for _, s := range role.Subprotocols {
			opts.Subprotocols = append(removeString(opts.Subprotocols, s), s)
		}
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
	// F3: resolve {param} placeholders in the dial url from the resolved
	// actor's captured path params. Placed AFTER role-param injection (and its
	// preInjectionURL recompute) and BEFORE websocket.Dial so (a) role query
	// params are already on the URL when we template the path and (b) a leftover
	// {param} (no captured value) fails clearly here instead of producing a
	// silent wrong dial. The actor is the credentialRef already resolved above
	// (role.CredentialRef → a.CredentialRef → proto.Auth.CredentialRef).
	params := e.pathParamsFor(credentialRef)
	beforeTemplate := dialURL
	resolved, perr := resolveURLParams(dialURL, params)
	if perr != nil {
		return types.WSResult{OK: false, URL: preInjectionURL, Err: perr.Error(), Latency: time.Since(start)}
	}
	dialURL = resolved
	// recompute preInjectionURL only when templating actually changed the URL
	// (dialURL != beforeTemplate). Gating on the URL change — not on the presence
	// of params — avoids drifting the no-auth/no-role echo (e.g. http:// → ws://)
	// when an actor declares path_params but the URL has no placeholder. Only the
	// auth token is scrubbed — a path id like userId is the endpoint, not a secret.
	if dialURL != beforeTemplate {
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
	// ws_connect stays an intermediate step. readMatching drains the pump under
	// the entry's readMu; only the timeout-failure cleanup takes e.mu, never
	// both at once (readMatching has returned and released readMu before e.mu).
	var seen []string
	if role != nil && role.Handshake != nil {
		awaitType := role.Handshake.AwaitType
		// Suppress the connect-time auto-await for an OPTIONAL handshake when a
		// later ws_receive on this same connection will assert the same type. The
		// optional await is best-effort: left running, it either stalls the case
		// for the full handshake timeout (a peer-join signal arrives only on a
		// later peer-connect step, which this sequential runner blocks on), or —
		// when the server pushes the type at join — it matches and CONSUMES the
		// frame, leaving the decisive later receive nothing to match (false fail).
		// Skipping it keeps the buffered frame for the explicit receive. The
		// connection is already stored and alive. MANDATORY handshakes are NOT
		// suppressed: their connect consumes the AwaitType by design and the
		// redundant receive is dropped at assembly (sanitizeSelfHandshakeReawait).
		if role.Handshake.Optional && awaitType != "" && slices.Contains(a.SuppressAwaitTypes, awaitType) {
			return types.WSResult{OK: true, URL: preInjectionURL, ConnectionID: id, Latency: time.Since(start)}
		}
		entry, ok := e.lookup(key)
		if !ok {
			return types.WSResult{OK: false, URL: preInjectionURL, Err: "ws handshake: connection vanished", Latency: time.Since(start)}
		}
		hsFraming := framingOf(entry)
		path := "type"
		if proto.TypePath != "" {
			path = proto.TypePath
		}
		hsTimeout := time.Duration(role.Handshake.Timeout) * time.Second
		matched, hsSeen, hsStatus := readMatching(entry, func(m wsMsg) bool {
			return matchType(hsFraming, m.data, awaitType, path)
		}, hsTimeout)
		switch hsStatus {
		case "matched":
			// The matched handshake frame is also evidence: the old loop appended
			// every frame (including the match) to seen before checking the match.
			seen = append(hsSeen, frameForResult(hsFraming, matched.data))
		case "timeout":
			if role.Handshake.Optional {
				// Best-effort handshake: the awaited message did not arrive within
				// the timeout, but the connection is STILL ALIVE (the read pump
				// keeps running; readMatching released readMu on return). Succeed
				// without closing so a later ws_send/ws_receive on the same
				// connection_id works — peer-gated handshakes (e.g. open-agents'
				// devices:sync) only arrive when a bridge is online. Non-matching
				// frames seen while waiting are returned as evidence.
				return types.WSResult{OK: true, URL: preInjectionURL, ConnectionID: id, SeenMessages: hsSeen, Latency: time.Since(start)}
			}
			// Mandatory handshake timed out: close + remove the connection so a
			// subsequent ws_send on this id fails as unknown. Closing the conn
			// stops the pump (its Read errors out); the ctx-cancel cleanup
			// goroutine is a redundant safety net.
			e.mu.Lock()
			if ent, ok := e.conns[key]; ok {
				_ = ent.conn.Close(websocket.StatusNormalClosure, "handshake timeout")
				delete(e.conns, key)
			}
			e.mu.Unlock()
			return types.WSResult{OK: false, URL: preInjectionURL, Err: fmt.Sprintf("ws handshake: timed out awaiting %q", awaitType), SeenMessages: hsSeen, Latency: time.Since(start)}
		default: // "closed"
			// The pump exited (peer close or ctx cancel) before the handshake
			// completed. The connection is dead regardless of optional/mandatory:
			// close + remove it so a subsequent ws_send fails as unknown.
			e.mu.Lock()
			if ent, ok := e.conns[key]; ok {
				_ = ent.conn.Close(websocket.StatusNormalClosure, "handshake closed")
				delete(e.conns, key)
			}
			e.mu.Unlock()
			return types.WSResult{OK: false, URL: preInjectionURL, Err: fmt.Sprintf("ws handshake: connection closed awaiting %q", awaitType), SeenMessages: hsSeen, Latency: time.Since(start)}
		}
	}
	return types.WSResult{OK: true, URL: preInjectionURL, ConnectionID: id, SeenMessages: seen, Latency: time.Since(start)}
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

// pathParamsFor returns the resolved actor's captured url-param -> value map,
// or nil. A nil index or unknown actor yields nil (the no-placeholders path:
// resolveURLParams returns rawURL unchanged, preserving M0 behavior).
func (e *WebSocketExecutor) pathParamsFor(actor string) map[string]string {
	if e.idx == nil {
		return nil
	}
	return e.idx.ActorPathParams[actor]
}

// resolveURLParams substitutes {name} placeholders in rawURL from params.
// After substitution, any leftover placeholder means a placeholder with no
// captured value: that is a hard error (clear failure over a silent wrong dial)
// naming the first unresolved placeholder. Empty params / no placeholders ⇒
// rawURL unchanged (backwards-compat with pre-F3 configs).
//
// Both the raw "{name}" and the percent-encoded "%7Bname%7D" forms are
// recognized: setQueryParam during auth/role-param injection runs the URL
// through url.Parse+String, which percent-encodes "{" and "}" in the path. By
// the time templating runs (after that injection) the placeholder is normally
// in its encoded form, but a URL that skipped query injection stays raw.
//
// Captured values are substituted AS-IS (no URL encoding): they must be
// path-safe identifiers (e.g. a userId). A value containing '/', '?', '#', '&',
// '%', or spaces would malform the URL — the auth-flow capture is intended for
// ids, not arbitrary URL segments (no expression encoder, by design).
func resolveURLParams(rawURL string, params map[string]string) (string, error) {
	out := rawURL
	for name, val := range params {
		out = strings.ReplaceAll(out, "{"+name+"}", val)
		out = strings.ReplaceAll(out, "%7B"+name+"%7D", val)
	}
	if rest := unresolvedURLPlaceholder(out); rest != "" {
		return "", fmt.Errorf("ws connect: unresolved URL param %s", rest)
	}
	return out, nil
}

// unresolvedURLPlaceholder returns the first leftover placeholder in s — raw
// "{...}" or percent-encoded "%7B...%7D" — or "" when none. The raw form is
// checked first to keep the common-case error message readable.
func unresolvedURLPlaceholder(s string) string {
	for _, form := range []struct{ open, close string }{{"{", "}"}, {"%7B", "%7D"}} {
		i := strings.Index(s, form.open)
		if i < 0 {
			continue
		}
		tail := s[i+len(form.open):]
		j := strings.Index(tail, form.close)
		if j > 0 {
			return s[i : i+len(form.open)+j+len(form.close)]
		}
	}
	return ""
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
// order (deterministic error reporting). On the first LEGIT failure (a path
// whose root segment exists in the matched message but the full path is absent
// or its value mismatches) it returns the path, expected, actual ("<missing>"
// for an absent intermediate key) and ok=false. Entries whose ROOT segment is
// not a top-level key of the matched message are treated as malformed (a
// wrong-path or expression-shape assert the LLM emitted) and SKIPPED — collected
// in malformedPaths, non-fatal — so a type-matched receive is never false-failed
// by a malformed assert. A legit assert always references a path whose root is a
// real top-level field; the only masked case is a legit assert on a root field
// genuinely absent from the message (structurally identical to a wrong-path
// malformed assert — accepted, surfaced via malformedPaths for a visible warn).
// Empty asserts is a no-op (M1 behavior).
func checkAsserts(data []byte, asserts map[string]any) (failPath string, failExpected, failActual any, ok bool, malformedPaths []string) {
	if len(asserts) == 0 {
		return "", nil, nil, true, nil
	}
	paths := make([]string, 0, len(asserts))
	for k := range asserts {
		paths = append(paths, k)
	}
	sort.Strings(paths)
	for _, p := range paths {
		exp := asserts[p]
		// Malformed-assert defense (D4): if the path's root segment is not a
		// top-level key of the matched message, the entry is almost certainly a
		// malformed (wrong-path / expression-shape) assert rather than a real
		// content check. Skip it (non-fatal) and record it for a visible warning.
		root := p
		if i := strings.IndexByte(p, '.'); i >= 0 {
			root = p[:i]
		}
		if _, rootFound := extractPath(data, root); !rootFound {
			malformedPaths = append(malformedPaths, p)
			continue
		}
		got, found := extractPath(data, p)
		if !found {
			return p, exp, "<missing>", false, malformedPaths
		}
		if !valueEqual(got, exp) {
			return p, exp, got, false, malformedPaths
		}
	}
	return "", nil, nil, true, malformedPaths
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
	want := append([]string{a.Type}, a.Aliases...)
	if a.MatchAll {
		return e.doReceiveMatchAll(entry, framing, path, want, a, start)
	}
	matched, seen, status := readMatching(entry, func(m wsMsg) bool {
		return matchAnyType(framing, m.data, want, path)
	}, timeout)
	switch status {
	case "matched":
		data := matched.data
		// For json framing, evaluate field-level assertions (if any) in sorted
		// path order; the first failure fails the receive with a precise message.
		// The matched frame is evidence either way. No asserts (or non-json
		// framing) → arrival-only success.
		if framing == "" || framing == "json" {
			p, exp, act, ok, malformed := checkAsserts(data, a.Assert)
			if !ok {
				return types.WSResult{
					OK:             false,
					Err:            fmt.Sprintf("receive: assert %s: expected %v, got %v", p, exp, act),
					MatchedMessage: frameForResult(framing, data),
					SeenMessages:   seen,
					Latency:        time.Since(start),
				}
			}
			if len(malformed) > 0 {
				// Visible masking: these assert entries referenced roots absent
				// from the matched message (likely malformed). The type matched,
				// so the receive is NOT failed; logged so the skip is visible.
				e.logger.Warn("ws_receive: assert entries skipped (root not in matched message, likely malformed — not failing the matched receive)",
					zap.String("type", a.Type),
					zap.String("connection_id", a.ConnectionID),
					zap.Strings("skipped_paths", malformed))
			}
		}
		return types.WSResult{OK: true, MatchedMessage: frameForResult(framing, data), SeenMessages: seen, Latency: time.Since(start)}
	case "timeout":
		// No matching frame within the deadline. The connection is STILL ALIVE
		// (the pump keeps running): return OK:false without closing, so a later
		// send/receive on the same connection_id can succeed.
		errMsg := fmt.Sprintf("receive: timed out awaiting %q", a.Type)
		if len(a.Aliases) > 0 {
			errMsg = fmt.Sprintf("%s (aliases: %v)", errMsg, a.Aliases)
		}
		return types.WSResult{OK: false, Err: errMsg, SeenMessages: seen, Latency: time.Since(start)}
	default: // "closed"
		// The pump exited (peer close or ctx cancel); the connection is dead.
		return types.WSResult{OK: false, Err: fmt.Sprintf("receive: %v", entry.pumpErr), SeenMessages: seen, Latency: time.Since(start)}
	}
}

// doReceiveMatchAll handles a MatchAll receive: collect every consecutive
// matching frame in the burst (readMatchingAll) and evaluate Assert against EACH
// collected frame. The receive passes only when every frame satisfies every
// assert; the first failing item (1-based index named in Err) fails the receive.
// Without Assert (or non-json framing) it is arrival-only per item.
func (e *WebSocketExecutor) doReceiveMatchAll(entry *wsEntry, framing, path string, want []string, a types.WSReceiveAction, start time.Time) types.ExecutorResult {
	timeout := time.Duration(a.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	frames, seen, status := readMatchingAll(entry, func(m wsMsg) bool {
		return matchAnyType(framing, m.data, want, path)
	}, timeout)
	switch status {
	case "timeout":
		errMsg := fmt.Sprintf("receive: timed out awaiting %q", a.Type)
		if len(a.Aliases) > 0 {
			errMsg = fmt.Sprintf("%s (aliases: %v)", errMsg, a.Aliases)
		}
		return types.WSResult{OK: false, Err: errMsg, SeenMessages: seen, Latency: time.Since(start)}
	case "closed":
		return types.WSResult{OK: false, Err: fmt.Sprintf("receive: %v", entry.pumpErr), SeenMessages: seen, Latency: time.Since(start)}
	}
	// status == "matched" ⇒ at least one frame collected (readMatchingAll only
	// reaches the burst phase after the first match). Render every frame once.
	rendered := make([]string, len(frames))
	for i, f := range frames {
		rendered[i] = frameForResult(framing, f.data)
	}
	if framing == "" || framing == "json" {
		for i, f := range frames {
			p, exp, act, ok, _ := checkAsserts(f.data, a.Assert)
			if !ok {
				// Failing frame goes first (MatchedMessage); the rest are evidence.
				others := append([]string{}, rendered[:i]...)
				others = append(others, rendered[i+1:]...)
				return types.WSResult{
					OK:              false,
					Err:             fmt.Sprintf("receive: assert %s failed at item %d: expected %v, got %v", p, i+1, exp, act),
					MatchedMessage:  rendered[i],
					MatchedMessages: others,
					MatchedCount:    len(frames),
					SeenMessages:    seen,
					Latency:         time.Since(start),
				}
			}
		}
	}
	// All items satisfy the asserts (or no asserts / non-json: arrival-only).
	return types.WSResult{
		OK:              true,
		MatchedMessage:  rendered[0],
		MatchedMessages: rendered[1:],
		MatchedCount:    len(frames),
		SeenMessages:    seen,
		Latency:         time.Since(start),
	}
}
