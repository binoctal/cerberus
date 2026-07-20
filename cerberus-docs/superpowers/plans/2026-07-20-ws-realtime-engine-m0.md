# WebSocket Realtime Engine (M0) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Upgrade the WebSocket executor from dial-and-close `ws_connect`/`ws_send` into a protocol-agnostic primitive engine (`connect`/`send`/`receive`/`disconnect`) with per-case connection lifetime, a `decisive`-step judgment model, and Examiner visibility into WS message bodies.

**Architecture:** Fine-grained WS primitives referenced by `connection_id`, orchestrated by the LLM inside the existing ReAct loop. Connections live on a per-case context and auto-close on ctx cancellation. A new `isIntermediateStep` predicate (generalizing `isNoopWait`) plus a Phase-7 recovery guard let connect/send/discount and intermediate receives succeed without prematurely passing the case or burning recovery LLM calls. Assertions stay LLM-side: receive matches by top-level `type`; content is judged by the Examiner once `buildEvidenceContext` learns to surface `WSResult` messages.

**Tech Stack:** Go 1.25, `github.com/coder/websocket` v1.8.14, `net/http/httptest` for WS test servers, table-driven Go tests.

## Global Constraints

- Go 1.25, module `github.com/binoctal/cerberus`, pure-Go (no CGo).
- WebSocket library is `github.com/coder/websocket` v1.8.14 (already in `go.mod`); do NOT add `nhooyr.io/websocket` or any expression/JSONPath dependency — M0 has no evaluator (see spec D5).
- Comments and commit messages in English. Commit author `binoctal <binoctal@gmail.com>`, no `Co-Authored-By`.
- Follow existing comment density and naming; table-driven tests mirroring `internal/head/agent/http_test.go`.
- Scope is M0 only. No `project.yaml` WS schema, no Scout WS cases, no auth-strategy abstraction (those are M1).
- Each task must leave the tree compiling and tests green (`make test`).

**Spec:** `cerberus-docs/superpowers/specs/2026-07-20-ws-realtime-engine-m0-design.md`

---

## File Structure

**Create:** none new — all changes land in existing files (the engine extends the current WebSocket executor and types).

**Modify:**
- `internal/types/actions.go` — add `ActionWSReceive`, `ActionWSDisconnect` constants.
- `internal/types/actions_http.go` — add `WSReceiveAction`, `WSDisconnectAction`; grow `WSConnectAction` (ConnectionID, Subprotocols) and `WSConnectAction`→`WSSendAction` (ConnectionID instead of URL).
- `internal/types/actions_registry.go` — register the two new factories.
- `internal/types/actions_deref_groups.go` — dereference the two new pointer types.
- `internal/types/result_ws.go` — extend `WSResult` (MatchedMessage, SeenMessages).
- `internal/head/agent/websocket.go` — connection table, rewrite `doConnect`/`doSend`, add `doReceive`/`doDisconnect`.
- `internal/head/agent/react_loop_helpers.go` — `isNoopWait` → `isIntermediateStep`.
- `internal/head/agent/execute_phases_react_loop.go` — pass-gate + Phase-7 recovery guard.
- `internal/head/examiner/judge.go` — `buildEvidenceContext` WSResult branch.
- `internal/head/agent/prompts.go` — WS primitive guidance in the steer prompt.
- `cerberus-docs/executors/websocket.md` — rewrite executor doc.

**Test files (create):**
- `internal/types/ws_actions_test.go` — action marshal/validate/deref.
- `internal/types/result_ws_test.go` — WSResult summary/evidence (if absent).
- `internal/head/agent/websocket_test.go` — executor primitives + lifecycle + judgment.

---

## Task 1: New WS action types + constants + registry + deref

**Files:**
- Modify: `internal/types/actions.go` (constants), `internal/types/actions_http.go` (structs), `internal/types/actions_registry.go`, `internal/types/actions_deref_groups.go`
- Test: `internal/types/ws_actions_test.go` (create)

**Interfaces:**
- Produces: `WSReceiveAction{ConnectionID, Type, Timeout, Decisive}` (ActionType `ws_receive`), `WSDisconnectAction{ConnectionID}` (ActionType `ws_disconnect`). Both implement `TypedAction` (`GetActionType`, `Target`, `Validate`). Does NOT touch `WSConnectAction`/`WSSendAction` (Task 3/4 own those).

- [ ] **Step 1: Write the failing test**

`internal/types/ws_actions_test.go`:
```go
package types

import (
	"encoding/json"
	"testing"
)

func TestWSReceiveActionRoundTrip(t *testing.T) {
	envelope, err := MarshalAction(&WSReceiveAction{
		ConnectionID: "conn-1",
		Type:         "permission:response",
		Timeout:      5,
		Decisive:     true,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if envelope.Type != ActionWSReceive {
		t.Fatalf("type = %s, want %s", envelope.Type, ActionWSReceive)
	}
	got, err := UnmarshalAction(envelope)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	r, ok := got.(WSReceiveAction)
	if !ok {
		t.Fatalf("deref type %T, want WSReceiveAction value", got)
	}
	if r.ConnectionID != "conn-1" || r.Type != "permission:response" || !r.Decisive {
		t.Fatalf("round-trip lost fields: %+v", r)
	}
}

func TestWSReceiveActionValidate(t *testing.T) {
	if err := (WSReceiveAction{ConnectionID: "c", Type: "t"}).Validate(); err != nil {
		t.Fatalf("valid action rejected: %v", err)
	}
	if err := (WSReceiveAction{ConnectionID: "", Type: "t"}).Validate(); err == nil {
		t.Fatal("empty connection_id should fail validation")
	}
	if err := (WSReceiveAction{ConnectionID: "c", Type: ""}).Validate(); err == nil {
		t.Fatal("empty type should fail validation")
	}
}

func TestWSDisconnectActionRoundTrip(t *testing.T) {
	envelope, err := MarshalAction(&WSDisconnectAction{ConnectionID: "conn-2"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if envelope.Type != ActionWSDisconnect {
		t.Fatalf("type = %s, want %s", envelope.Type, ActionWSDisconnect)
	}
	got, err := UnmarshalAction(envelope)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if d, ok := got.(WSDisconnectAction); !ok || d.ConnectionID != "conn-2" {
		t.Fatalf("round-trip failed: %+v", got)
	}
}

// Guard against accidental JSON-tag drift by decoding raw bytes.
func TestWSReceiveActionJSONTags(t *testing.T) {
	raw := []byte(`{"connection_id":"c","type":"t","timeout":3,"decisive":true}`)
	var r WSReceiveAction
	if err := json.Unmarshal(raw, &r); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if r.Timeout != 3 || !r.Decisive {
		t.Fatalf("json tags mismatch: %+v", r)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/types/ -run 'TestWSReceiveAction|TestWSDisconnectAction' -v`
Expected: FAIL — `undefined: ActionWSReceive` / `WSReceiveAction`.

- [ ] **Step 3: Write minimal implementation**

In `internal/types/actions.go`, add next to the existing WS constants (after `ActionWSSend` at line 47):
```go
	ActionWSReceive    ActionType = "ws_receive"
	ActionWSDisconnect ActionType = "ws_disconnect"
```

In `internal/types/actions_http.go`, append after the `WSSendAction` definition (after line 216):
```go
// WSReceiveAction waits for an inbound message whose top-level "type" field
// equals Type. Non-matching messages are accumulated as evidence. When Decisive
// is true (the default), a matching message passes the case.
type WSReceiveAction struct {
	ConnectionID string `json:"connection_id"`
	Type         string `json:"type"`
	// Timeout in seconds for the matching message to arrive.
	Timeout  int  `json:"timeout,omitempty"`
	Decisive bool `json:"decisive,omitempty"`
}

func (a WSReceiveAction) GetActionType() ActionType { return ActionWSReceive }
func (a WSReceiveAction) Target() string            { return a.ConnectionID }
func (a WSReceiveAction) Validate() error {
	if a.ConnectionID == "" {
		return fmt.Errorf("connection_id is required")
	}
	if a.Type == "" {
		return fmt.Errorf("type is required")
	}
	return nil
}

// WSDisconnectAction closes and removes an established WebSocket connection.
type WSDisconnectAction struct {
	ConnectionID string `json:"connection_id"`
}

func (a WSDisconnectAction) GetActionType() ActionType { return ActionWSDisconnect }
func (a WSDisconnectAction) Target() string            { return a.ConnectionID }
func (a WSDisconnectAction) Validate() error {
	if a.ConnectionID == "" {
		return fmt.Errorf("connection_id is required")
	}
	return nil
}
```

In `internal/types/actions_registry.go`, add to the `unmarshalRegistry` map (after the `ActionWSSend` line):
```go
	ActionWSReceive:    func() TypedAction { return &WSReceiveAction{} },
	ActionWSDisconnect: func() TypedAction { return &WSDisconnectAction{} },
```

In `internal/types/actions_deref_groups.go`, add two cases to `derefHTTPActions` (after `*WSSendAction`):
```go
	case *WSReceiveAction:
		return *v, true
	case *WSDisconnectAction:
		return *v, true
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/types/ -run 'TestWSReceiveAction|TestWSDisconnectAction' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/types/actions.go internal/types/actions_http.go internal/types/actions_registry.go internal/types/actions_deref_groups.go internal/types/ws_actions_test.go
git commit -m "feat(types): add ws_receive and ws_disconnect actions"
```

---

## Task 2: Extend WSResult with matched + seen messages

**Files:**
- Modify: `internal/types/result_ws.go`
- Test: `internal/types/result_ws_test.go` (create if absent)

**Interfaces:**
- Produces: `WSResult` gains `MatchedMessage string` and `SeenMessages []string`. `Summary()` stays compact (status/url/count); `Evidence()` includes the matched message plus seen messages so the Examiner (Task 7) and trace store can read bodies.

- [ ] **Step 1: Write the failing test**

`internal/types/result_ws_test.go`:
```go
package types

import "testing"

func TestWSResultEvidenceIncludesMatchedAndSeen(t *testing.T) {
	r := WSResult{
		OK:            true,
		URL:           "ws://x/ws",
		MatchedMessage: `{"type":"permission:response","payload":{"approved":true}}`,
		SeenMessages:  []string{`{"type":"heartbeat"}`},
	}
	ev := r.Evidence()
	if ev.Type != "ws_messages" {
		t.Fatalf("evidence type = %s, want ws_messages", ev.Type)
	}
	if !contains(ev.Content, "permission:response") {
		t.Fatalf("evidence missing matched message: %s", ev.Content)
	}
	if !contains(ev.Content, "heartbeat") {
		t.Fatalf("evidence missing seen message: %s", ev.Content)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	}())
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/types/ -run TestWSResultEvidenceIncludesMatchedAndSeen -v`
Expected: FAIL — `WSResult has no field MatchedMessage` / evidence lacks content.

- [ ] **Step 3: Write minimal implementation**

Replace the `WSResult` struct and its `Evidence` method in `internal/types/result_ws.go`:
```go
// WSResult represents a WebSocket operation result.
type WSResult struct {
	OK       bool          `json:"success"`
	URL      string        `json:"url"`
	// MatchedMessage is the message that satisfied a WSReceive match (empty for
	// connect/send/disconnect).
	MatchedMessage string `json:"matched_message,omitempty"`
	// SeenMessages are non-matching messages observed while WSReceive scanned.
	SeenMessages []string `json:"seen_messages,omitempty"`
	// Messages is the legacy combined list (kept for back-compat readers).
	Messages []string      `json:"messages,omitempty"`
	Latency  time.Duration `json:"duration"`
	Err      string        `json:"error,omitempty"`
}

func (r WSResult) Success() bool           { return r.OK }
func (r WSResult) Duration() time.Duration { return r.Latency }
func (r WSResult) Summary() string {
	status := "ok"
	if !r.OK {
		status = "error"
	}
	return fmt.Sprintf("ws %s %s (%d msgs, %s)", status, r.URL, len(r.Messages), r.Latency)
}
func (r WSResult) Evidence() EvidenceData {
	var all []string
	if r.MatchedMessage != "" {
		all = append(all, "matched: "+r.MatchedMessage)
	}
	all = append(all, r.SeenMessages...)
	all = append(all, r.Messages...)
	return EvidenceData{Type: "ws_messages", Content: truncate(joinStrings(all, "\n"), 10000)}
}
```
Keep the existing `truncate` and `joinStrings` helpers in the package (they already exist).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/types/ -run TestWSResult -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/types/result_ws.go internal/types/result_ws_test.go
git commit -m "feat(types): expose matched and seen messages in WSResult evidence"
```

---

## Task 3: Connection table + WSConnect (persistent) + WSDisconnect

This task changes `WSConnectAction` (adds `ConnectionID`, `Subprotocols`) and rewrites `doConnect` so connections persist, plus adds `doDisconnect`. The executor gains a connection table.

**Files:**
- Modify: `internal/types/actions_http.go` (`WSConnectAction` fields), `internal/head/agent/websocket.go` (table, `doConnect`, `doDisconnect`, `Execute` switch)
- Test: `internal/head/agent/websocket_test.go` (create, includes the shared WS test-server helper)

**Interfaces:**
- Consumes: `WSResult` from Task 2.
- Produces: `WebSocketExecutor` holds `conns map[string]*wsEntry` guarded by `sync.RWMutex`; each entry stores `*websocket.Conn` and its creating `context.Context`. `WSConnectAction` gains `ConnectionID string` and `Subprotocols []string`. `Execute` dispatches `WSConnectAction`/`WSSendAction`(Task 4)/`WSReceiveAction`(Task 5)/`WSDisconnectAction`.

- [ ] **Step 1: Write the failing test**

`internal/head/agent/websocket_test.go`:
```go
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

	res := ex.Execute(ctx, WSConnectAction{URL: url, ConnectionID: "c1"})
	if !res.Success() {
		t.Fatalf("connect failed: %+v", res)
	}

	// Same connection_id reused: must NOT dial again.
	res2 := ex.Execute(ctx, WSDisconnectAction{ConnectionID: "c1"})
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
	res := ex.Execute(ctx, WSConnectAction{URL: url, ConnectionID: "c1"})
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/head/agent/ -run 'TestWSConnect' -v`
Expected: FAIL — `WSConnectAction has no field ConnectionID` / `doConnect still closes`.

- [ ] **Step 3: Write minimal implementation**

In `internal/types/actions_http.go`, extend `WSConnectAction` (replace the existing struct at line 172):
```go
// WSConnectAction establishes and persists a WebSocket connection.
type WSConnectAction struct {
	URL string `json:"url"`
	// Headers are optional WebSocket handshake headers.
	Headers map[string]string `json:"headers,omitempty"`
	// Subprotocols are optional WS subprotocols to negotiate.
	Subprotocols []string `json:"subprotocols,omitempty"`
	// HandshakeTimeout is the timeout for connection handshake.
	HandshakeTimeout int `json:"handshake_timeout,omitempty"`
	// ConnectionID names this connection for later send/receive/disconnect.
	// If empty the executor assigns one.
	ConnectionID string `json:"connection_id,omitempty"`
}

func (a WSConnectAction) GetActionType() ActionType { return ActionWSConnect }
func (a WSConnectAction) Target() string            { return a.URL }
func (a WSConnectAction) Validate() error {
	if a.URL == "" {
		return fmt.Errorf("url is required")
	}
	return nil
}
```
(Remove the old `Messages []WSMessage` field and the now-unused `WSMessage` struct if nothing else references it — check with `grep`; if referenced elsewhere, leave it.)

Replace `internal/head/agent/websocket.go` entirely with:
```go
package agent

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/types"

	"github.com/coder/websocket"
)

// wsEntry is a persisted WebSocket connection owned by a case context.
type wsEntry struct {
	conn *websocket.Conn
	ctx  context.Context // per-case ctx; cancellation closes the conn
}

// WebSocketExecutor handles persistent WebSocket connections and primitives.
type WebSocketExecutor struct {
	logger *zap.Logger
	mu     sync.RWMutex
	conns  map[string]*wsEntry
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
	if len(a.Subprotocols) > 0 {
		opts.Subprotocols = a.Subprotocols
	}
	conn, _, err := websocket.Dial(ctx, wsURL(a.URL), opts)
	if err != nil {
		return types.WSResult{OK: false, URL: a.URL, Err: err.Error(), Latency: time.Since(start)}
	}
	id := a.ConnectionID
	if id == "" {
		id = fmt.Sprintf("ws-%d", time.Now().UnixNano())
	}
	e.store(id, conn, ctx)
	return types.WSResult{OK: true, URL: a.URL, Latency: time.Since(start)}
}

func (e *WebSocketExecutor) doDisconnect(ctx context.Context, a types.WSDisconnectAction, start time.Time) types.ExecutorResult {
	e.mu.Lock()
	entry, ok := e.conns[a.ConnectionID]
	if ok {
		_ = entry.conn.Close(websocket.StatusNormalClosure, "disconnect")
		delete(e.conns, a.ConnectionID)
	}
	e.mu.Unlock()
	if !ok {
		return types.WSResult{OK: false, Err: fmt.Sprintf("unknown connection_id: %s", a.ConnectionID), Latency: time.Since(start)}
	}
	return types.WSResult{OK: true, Latency: time.Since(start)}
}
```
`doSend`/`doReceive` are added in Tasks 4/5; for now leave them as stubs that return an error result so the file compiles:
```go
func (e *WebSocketExecutor) doSend(ctx context.Context, a types.WSSendAction, start time.Time) types.ExecutorResult {
	return types.ErrorResult{Err: "ws_send not yet implemented"} // Task 4
}

func (e *WebSocketExecutor) doReceive(ctx context.Context, a types.WSReceiveAction, start time.Time) types.ExecutorResult {
	return types.ErrorResult{Err: "ws_receive not yet implemented"} // Task 5
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/head/agent/ -run 'TestWSConnect' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/types/actions_http.go internal/head/agent/websocket.go internal/head/agent/websocket_test.go
git commit -m "feat(ws): persistent connections with ctx-bound lifetime and disconnect"
```

---

## Task 4: WSSend over an existing connection

**Files:**
- Modify: `internal/types/actions_http.go` (`WSSendAction`: URL → ConnectionID), `internal/head/agent/websocket.go` (`doSend`)
- Test: `internal/head/agent/websocket_test.go` (append)

**Interfaces:**
- Consumes: connection table from Task 3.
- Produces: `WSSendAction{ConnectionID, Message}` (URL field removed — connections are referenced by id, never re-dialed).

- [ ] **Step 1: Write the failing test**

Append to `internal/head/agent/websocket_test.go`:
```go
func TestWSSendReusesConnection(t *testing.T) {
	dials := 0
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
	if !ex.Execute(ctx, WSConnectAction{URL: url, ConnectionID: "c1"}).Success() {
		t.Fatal("connect failed")
	}
	dials = 0 // reset; subsequent sends must not dial
	_ = dials

	res := ex.Execute(ctx, WSSendAction{ConnectionID: "c1", Message: `{"type":"ping"}`})
	if !res.Success() {
		t.Fatalf("send failed: %+v", res)
	}
	// Unknown id fails, does not dial.
	res2 := ex.Execute(ctx, WSSendAction{ConnectionID: "nope", Message: "x"})
	if res2.Success() {
		t.Fatal("send on unknown connection_id should fail")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/head/agent/ -run TestWSSendReusesConnection -v`
Expected: FAIL — `WSSendAction has no field ConnectionID` / stub returns error.

- [ ] **Step 3: Write minimal implementation**

In `internal/types/actions_http.go`, replace the `WSSendAction` definition (line ~199):
```go
// WSSendAction sends a message over an existing WebSocket connection.
type WSSendAction struct {
	// ConnectionID identifies the connection established by WSConnect.
	ConnectionID string `json:"connection_id"`
	Message      string `json:"message"`
}

func (a WSSendAction) GetActionType() ActionType { return ActionWSSend }
func (a WSSendAction) Target() string            { return a.ConnectionID }
func (a WSSendAction) Validate() error {
	if a.ConnectionID == "" {
		return fmt.Errorf("connection_id is required")
	}
	if a.Message == "" {
		return fmt.Errorf("message is required")
	}
	return nil
}
```

In `internal/head/agent/websocket.go`, replace the `doSend` stub:
```go
func (e *WebSocketExecutor) doSend(ctx context.Context, a types.WSSendAction, start time.Time) types.ExecutorResult {
	conn, _, ok := e.lookup(a.ConnectionID)
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/head/agent/ -run 'TestWSSend|TestWSConnect' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/types/actions_http.go internal/head/agent/websocket.go internal/head/agent/websocket_test.go
git commit -m "feat(ws): ws_send over an existing connection by id"
```

---

## Task 5: WSReceive — wait for a type match

**Files:**
- Modify: `internal/head/agent/websocket.go` (`doReceive`)
- Test: `internal/head/agent/websocket_test.go` (append)

**Interfaces:**
- Consumes: connection table; `WSResult.MatchedMessage`/`SeenMessages` (Task 2).
- Produces: `doReceive` reads the inbound stream until a message whose top-level `type` field equals `a.Type` arrives, or `timeout` elapses, or the peer closes. Non-matching messages accumulate in `SeenMessages`; the match goes to `MatchedMessage`.

- [ ] **Step 1: Write the failing test**

Append to `internal/head/agent/websocket_test.go`:
```go
import "encoding/json"

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
	ex.Execute(ctx, WSConnectAction{URL: url, ConnectionID: "c1"})

	res := ex.Execute(ctx, WSReceiveAction{ConnectionID: "c1", Type: "permission:response", Timeout: 2})
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
	ex.Execute(ctx, WSConnectAction{URL: url, ConnectionID: "c1"})

	res := ex.Execute(ctx, WSReceiveAction{ConnectionID: "c1", Type: "never", Timeout: 1})
	if res.Success() {
		t.Fatalf("expected timeout failure, got success: %+v", res)
	}
}
```
(Add `"github.com/binoctal/cerberus/internal/types"` to the test file imports if not present.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/head/agent/ -run 'TestWSReceive' -v`
Expected: FAIL — stub returns "not yet implemented".

- [ ] **Step 3: Write minimal implementation**

In `internal/head/agent/websocket.go`, replace the `doReceive` stub and add a helper:
```go
import "encoding/json"

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
	conn, connCtx, ok := e.lookup(a.ConnectionID)
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/head/agent/ -run 'TestWSReceive' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/head/agent/websocket.go internal/head/agent/websocket_test.go
git commit -m "feat(ws): ws_receive waits for a top-level type match"
```

---

## Task 6: Intermediate-step judgment + recovery guard

**Files:**
- Modify: `internal/head/agent/react_loop_helpers.go`, `internal/head/agent/execute_phases_react_loop.go`
- Test: `internal/head/agent/websocket_test.go` (append — judgment via a mini loop driver), or a focused unit test on `isIntermediateStep`.

**Interfaces:**
- Produces: `isIntermediateStep(action)` generalizes `isNoopWait` (true for WaitAction without selector AND for WSConnect/WSSend/WSDisconnect and decisive=false WSReceive). `execute_phases_react_loop.go` pass-gate uses `isIntermediateStep`; Phase 7 calls `tryRecovery` only when `!isIntermediateStep(action) || !newResult.Success()`.

- [ ] **Step 1: Write the failing test**

Append to `internal/head/agent/websocket_test.go` (unit-level, no full loop):
```go
func TestIsIntermediateStep(t *testing.T) {
	cases := []struct {
		name string
		a    types.TypedAction
		want bool
	}{
		{"ws_connect", WSConnectAction{URL: "ws://x"}, true},
		{"ws_send", WSSendAction{ConnectionID: "c", Message: "m"}, true},
		{"ws_disconnect", WSDisconnectAction{ConnectionID: "c"}, true},
		{"ws_receive decisive=false", WSReceiveAction{ConnectionID: "c", Type: "t"}, true},
		{"ws_receive decisive=true", WSReceiveAction{ConnectionID: "c", Type: "t", Decisive: true}, false},
		{"pure wait", types.WaitAction{Duration: "1s"}, true},
		{"wait with selector", types.WaitAction{Duration: "1s", Selector: "#x"}, false},
	}
	for _, c := range cases {
		if got := isIntermediateStep(c.a); got != c.want {
			t.Fatalf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}
```
(Add `internal/types` import if not present; `types.WaitAction` has fields `Duration`, `Selector`, `WaitForState` — confirm field names via `grep "type WaitAction struct" internal/types/`.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/head/agent/ -run TestIsIntermediateStep -v`
Expected: FAIL — `undefined: isIntermediateStep`.

- [ ] **Step 3: Write minimal implementation**

In `internal/head/agent/react_loop_helpers.go`, replace `isNoopWait` (line 181) with:
```go
// isIntermediateStep reports whether an action's success should NOT end the
// case or consume recovery. It generalizes isNoopWait: pure waits stay
// intermediate, and so do the WS plumbing actions and non-decisive receives.
// A decisive WSReceive (the verification endpoint) is the only WS step that
// passes the case, so it is NOT intermediate.
func isIntermediateStep(action types.TypedAction) bool {
	if w, ok := action.(types.WaitAction); ok {
		return w.Selector == "" && w.WaitForState == ""
	}
	switch action.(type) {
	case types.WSConnectAction, types.WSSendAction, types.WSDisconnectAction:
		return true
	case types.WSReceiveAction:
		return !action.(types.WSReceiveAction).Decisive
	}
	return false
}
```
Keep the old `isNoopWait` name as a thin alias if any other package references it (check `grep -rn isNoopWait`); otherwise drop it — the only caller is updated next.

In `internal/head/agent/execute_phases_react_loop.go`:
- Line 51: change `!isNoopWait(action)` → `!isIntermediateStep(action)`.
- Phase 7 (line 59): change the guard from `if se.tryRecovery(attempt) { continue }` to:
```go
		// Phase 7: Attempt recovery only on actual failures. An intermediate
		// step that succeeded (connect/send/non-decisive receive) must proceed
		// to the next steer without burning a recovery LLM call or setting
		// recoverySkipped (which would mislabel the case as skipped).
		if !isIntermediateStep(action) || !newResult.Success() {
			if se.tryRecovery(attempt) {
				continue
			}
		}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/head/agent/ -run TestIsIntermediateStep -v && go build ./...`
Expected: PASS + clean build.

- [ ] **Step 5: Commit**

```bash
git add internal/head/agent/react_loop_helpers.go internal/head/agent/execute_phases_react_loop.go internal/head/agent/websocket_test.go
git commit -m "feat(agent): intermediate-step judgment with recovery guard"
```

---

## Task 7: Examiner reads WS message bodies

**Files:**
- Modify: `internal/head/examiner/judge.go` (`buildEvidenceContext`)
- Test: `internal/head/examiner/judge_test.go` (create or append)

**Interfaces:**
- Consumes: `WSResult.MatchedMessage`/`SeenMessages` from Task 2.
- Produces: `buildEvidenceContext` adds a `WSResult` branch (mirroring the HTTPResult body branch) so the judge prompt contains WS message bodies when judging a WS case's expectation.

- [ ] **Step 1: Write the failing test**

`internal/head/examiner/judge_test.go`:
```go
package examiner

import (
	"strings"
	"testing"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/types"
)

func TestBuildEvidenceContextIncludesWSMessages(t *testing.T) {
	j := &Judge{}
	res := agent.StepResult{
		TestCase: agent.TestCase{Name: "perm round-trip", Target: "ws://x/ws"},
		Status:   agent.StepPassed,
		Result: types.WSResult{
			OK:             true,
			MatchedMessage: `{"type":"permission:response","payload":{"approved":true}}`,
		},
	}
	got := j.buildEvidenceContext(res)
	if !strings.Contains(got, "permission:response") {
		t.Fatalf("evidence missing matched message body:\n%s", got)
	}
	if !strings.Contains(got, "approved") {
		t.Fatalf("evidence missing payload content:\n%s", got)
	}
}
```
(Confirm `agent.StepResult`, `agent.StepPassed`, and `agent.TestCase` field names with a quick `grep` before writing — they match `internal/head/agent/types.go`.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/head/examiner/ -run TestBuildEvidenceContextIncludesWSMessages -v`
Expected: FAIL — evidence falls back to `Summary()` which has no message body.

- [ ] **Step 3: Write minimal implementation**

In `internal/head/examiner/judge.go` `buildEvidenceContext`, replace the result-handling block (lines ~129-149) with one that also handles WS:
```go
	if r.Result != nil {
		switch res := r.Result.(type) {
		case types.HTTPResult:
			if res.StatusCode != 0 {
				b = append(b, fmt.Sprintf("HTTP Status: %d\n", res.StatusCode)...)
			}
			if res.Body != "" {
				body := res.Body
				if len(body) > 2000 {
					body = body[:2000] + "\n... (truncated)"
				}
				b = append(b, fmt.Sprintf("Response Body: %s\n", body)...)
			}
			if res.Err != "" {
				b = append(b, fmt.Sprintf("Error: %s\n", res.Err)...)
			}
		case types.WSResult:
			if res.MatchedMessage != "" {
				msg := res.MatchedMessage
				if len(msg) > 2000 {
					msg = msg[:2000] + "\n... (truncated)"
				}
				b = append(b, fmt.Sprintf("WS Matched Message: %s\n", msg)...)
			}
			for i, seen := range res.SeenMessages {
				if i >= 5 { // cap noise from heartbeats
					b = append(b, fmt.Sprintf("... and %d more seen messages\n", len(res.SeenMessages)-5)...)
					break
				}
				b = append(b, fmt.Sprintf("WS Seen: %s\n", seen)...)
			}
			if res.Err != "" {
				b = append(b, fmt.Sprintf("WS Error: %s\n", res.Err)...)
			}
		default:
			b = append(b, fmt.Sprintf("Result Summary: %s\n", r.Result.Summary())...)
		}
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/head/examiner/ -run TestBuildEvidenceContext -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/head/examiner/judge.go internal/head/examiner/judge_test.go
git commit -m "feat(examiner): surface WS message bodies in judge evidence"
```

---

## Task 8: WS primitives in the steer prompt

**Files:**
- Modify: `internal/head/agent/prompts.go` (the steer system prompt — locate `promptSteerSystem`)
- Test: `internal/head/agent/prompts_test.go` (append) or a content assertion

**Interfaces:**
- Produces: the steer prompt documents `ws_connect`/`ws_send`/`ws_receive`/`ws_disconnect`, the `connection_id` contract, `decisive` (at most one per case), and `type`-matching — so the LLM emits correct actions.

- [ ] **Step 1: Write the failing test**

Append to `internal/head/agent/prompts_test.go` (create if absent):
```go
package agent

import "testing"

func TestSteerPromptDocumentsWSPrimitives(t *testing.T) {
	for _, want := range []string{
		"ws_connect", "ws_send", "ws_receive", "ws_disconnect",
		"connection_id", "decisive",
	} {
		if !contains(promptSteerSystem, want) {
			t.Fatalf("steer prompt missing %q", want)
		}
	}
	if !contains(promptSteerSystem, "at most one decisive") {
		t.Fatal("steer prompt must state at-most-one-decisive")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/head/agent/ -run TestSteerPromptDocumentsWSPrimitives -v`
Expected: FAIL — WS primitives absent from prompt.

- [ ] **Step 3: Write minimal implementation**

In `internal/head/agent/prompts.go`, append a WS section to `promptSteerSystem` (insert before the closing backtick). Text:
```go
`

// Then in the const promptSteerSystem string, append this block (keep one
// trailing newline before the closing backtick):

const wsSteerGuidance = `
## WebSocket primitives (realtime protocols)
Use these for any WebSocket / realtime target. They share a connection table
keyed by connection_id; connect once, then send/receive/disconnect by id.

- ws_connect {url, headers?, subprotocols?, connection_id?}: open a persistent
  connection. Put credentials in url query, headers, or subprotocols as the
  protocol requires. If connection_id is omitted, one is assigned.
- ws_send {connection_id, message}: send a text/JSON message on an open conn.
- ws_receive {connection_id, type, timeout?, decisive?}: wait for a message
  whose top-level JSON "type" equals ` + "`type`" + `. Other messages are kept as
  evidence.
- ws_disconnect {connection_id}: close the connection.

Rules:
- Reuse the SAME connection_id across connect/send/receive/disconnect for one
  logical connection.
- A case passes when a ws_receive with decisive=true (default) sees its type
  arrive. Use AT MOST ONE decisive receive per case — it is the verification
  endpoint. Use decisive=false for intermediate "did it arrive here" checks.
- Content assertions (e.g. payload.approved) are judged from the received
  message by the Examiner against the test case expectation — receive only
  confirms the message arrived.
`
```
NOTE: `promptSteerSystem` is a single const raw-string literal (`prompts.go:4`) — do NOT create a `wsSteerGuidance` const or use string concatenation. Inline the block above directly into the existing `promptSteerSystem` raw string, before its closing backtick (right after the `- code_analyze/code_lint/code_symbols: Static code analysis` line). Two required corrections when inlining: (1) raw strings cannot contain backticks, so the `` ` + "`type`" + ` `` fragment becomes plain `whose top-level JSON "type" matches`; (2) write the decisive rule in lowercase — `Use at most one decisive receive per case` — because the test asserts the literal substring `at most one decisive` case-sensitively.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/head/agent/ -run TestSteerPromptDocumentsWSPrimitives -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/head/agent/prompts.go internal/head/agent/prompts_test.go
git commit -m "feat(agent): document WS primitives and decisive contract in steer prompt"
```

---

## Task 9: Rewrite the WebSocket executor doc

**Files:**
- Modify: `cerberus-docs/executors/websocket.md`

**Interfaces:** none (documentation).

- [ ] **Step 1: Read the current doc to confirm what changes**

Run: the file is already known (`cerberus-docs/executors/websocket.md`). Replace its content.

- [ ] **Step 2: Write the new doc**

Overwrite `cerberus-docs/executors/websocket.md` with:
```markdown
# WebSocket Executor

Tests realtime communication over persistent WebSocket connections. The LLM
orchestrates four protocol-agnostic primitives; connections are referenced by
`connection_id` and live for the duration of a test case.

Uses `github.com/coder/websocket`.

## Actions

### `ws_connect`
| Field | Type | Required | Description |
|---|---|---|---|
| `url` | string | yes | WebSocket URL (http(s) auto-converts to ws(s)) |
| `headers` | map[string]string | no | Handshake headers |
| `subprotocols` | []string | no | WS subprotocols |
| `connection_id` | string | no | Name for this connection (assigned if omitted) |

Opens a persistent connection. Put credentials in the url query string,
headers, or subprotocols as the target requires.

### `ws_send`
| Field | Type | Required | Description |
|---|---|---|---|
| `connection_id` | string | yes | Connection from `ws_connect` |
| `message` | string | yes | Message to send |

### `ws_receive`
| Field | Type | Required | Description |
|---|---|---|---|
| `connection_id` | string | yes | Connection to read from |
| `type` | string | yes | Wait for a message whose top-level `type` matches |
| `timeout` | int | no | Seconds (default 10) |
| `decisive` | bool | no | If true (default), a match passes the case |

Non-matching messages are kept as evidence. At most one `decisive=true`
receive per case.

### `ws_disconnect`
| Field | Type | Required | Description |
|---|---|---|---|
| `connection_id` | string | yes | Connection to close |

## Lifecycle

Connections are bound to the per-case context and close automatically when the
case ends (normal exit, timeout, or cancellation). Parallel cases are isolated.

## Result

- **Success** — connect/send succeeded, or the awaited `type` arrived.
- **Evidence** — matched message plus non-matching messages seen.
- **Duration** — time for the operation.

## Notes

- Matching is by top-level JSON `type` only (M0). Field-level assertions are
  judged by the Examiner from the received message. Configurable type-field
  paths and a declarative protocol layer arrive in later milestones (M1/M2).
```

- [ ] **Step 3: Verify build/docs not broken**

Run: `make build`
Expected: clean build (docs change only).

- [ ] **Step 4: Commit**

```bash
git add cerberus-docs/executors/websocket.md
git commit -m "docs(ws): rewrite executor doc for persistent primitives"
```

---

## Task 10: Full-suite green + integration smoke

**Files:** none (verification).

- [ ] **Step 1: Run the full test suite with race detector**

Run: `make test`
Expected: all packages PASS, including `internal/types`, `internal/head/agent`, `internal/head/examiner`.

- [ ] **Step 2: Run lint**

Run: `make lint`
Expected: clean. Fix any nits (unused `WSMessage`, dead `isNoopWait` alias) introduced by the refactor.

- [ ] **Step 3: Local integration smoke (optional, requires open-agents stack)**

If the open-agents API+realtime stack is running locally (`ws://localhost:8989`):
```bash
./build/cerberus run --url http://localhost:8989 \
  --goal "WebSocket permission round-trip: bridge sends permission:request, web receives it, web responds approved, bridge receives the response" \
  --max-duration 5m
```
Expected: one case orchestrates connect×2 → send → receive(decisive=false) → send → receive(decisive=true); case passes; Examiner sees the `approved:true` body. If the stack is unavailable, skip — the unit + executor tests in Tasks 1-7 already cover the behavior.

- [ ] **Step 4: Commit any lint fixes**

```bash
git add -A
git commit -m "chore(ws): lint cleanup after M0 executor refactor"
```

---

## Definition of Done

- All four WS primitives work over persisted, ctx-bound connections.
- A multi-step realtime conversation runs inside one TestCase without spurious recovery calls or `StepSkipped` mis-labels (Task 6 guard).
- The Examiner can read WS message bodies to judge content expectations (Task 7).
- `make test` and `make lint` are green.
- Docs reflect the new model; spec and plan committed in `cerberus-docs/`.
