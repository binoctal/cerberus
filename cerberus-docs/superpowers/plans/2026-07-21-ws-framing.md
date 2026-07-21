# WebSocket Realtime Engine (M2) — Text/Binary Framing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the WS executor send and receive `text` and `binary` frames driven by `protocol.framing`, while preserving M0/M1 JSON behavior byte-for-byte.

**Architecture:** `protocol.framing` (`json` default | `text` | `binary`) is a protocol-level declaration bundling three facts the executor derives: WS opcode (text vs binary), content codec (raw string vs base64), and the receive match predicate (type-path routing vs whole-frame exact equality). One shared helper (`matchType`) backs `doReceive` and the roles handshake loop; one codec helper (`frameForResult`) backs result encoding. No new action types, no new deps, no evaluator.

**Tech Stack:** Go 1.25 · `github.com/coder/websocket` v1.8.14 · `encoding/base64` (stdlib).

## Global Constraints

- Go 1.25, module `github.com/binoctal/cerberus`, **pure Go (no CGo)**.
- WS library **fixed** `github.com/coder/websocket` v1.8.14; **no** `nhooyr.io/websocket`; **no** expression/JSONPath/evaluator dependency.
- **No runtime expression evaluator** (M0 Constraint 3): the non-JSON match predicate is plain equality (`==` / `bytes.Equal`) only.
- Commit author **`binoctal <binoctal@gmail.com>`**, **no** `Co-Authored-By`. Code comments and commit messages in **English**.
- Documentation **only** in `cerberus-docs/`; **never** `docs/`.
- `make check` (fmt + lint + test `-race`) must be green. Tests are table-driven, mirroring `internal/head/agent/websocket_test.go` and `internal/project/validate_protocol_test.go`.
- `websocket_test.go` is `package agent` with **no alias** → action types need the `types.` prefix.
- `promptSteerSystem` is a **single raw-string literal** → any steer-prompt edit is inline (no concatenation, no backticks).
- Counters incremented by an httptest server goroutine must be `atomic` (`-race`).

**Spec:** `cerberus-docs/superpowers/specs/2026-07-21-ws-framing-design.md` (read it for rationale; this plan is the code).

---

## File Structure

- `internal/project/validate_protocol.go` — accept `text`/`binary` framing (Task 1).
- `internal/project/protocol_schema.go` — `Framing` field comment (Task 1).
- `internal/project/validate_protocol_test.go` — flip rejected→accepted, add invalid case (Task 1).
- `internal/head/agent/ws_protocol.go` — `framingOf`, `matchType`, `frameForResult` helpers (Task 2).
- `internal/head/agent/ws_protocol_test.go` — helper unit tests (Task 2).
- `internal/head/agent/websocket.go` — `doSend` (Task 3), `doReceive` (Task 4), handshake loop (Task 5).
- `internal/head/agent/websocket_test.go` — send/receive/handshake framing tests (Tasks 3–5).
- `cerberus-docs/executors/websocket.md` — document text/binary framing (Task 6).
- `internal/head/agent/prompts.go` — steer-prompt inline hint (Task 6).

---

## Task 1: Accept text/binary framing in validation

**Files:**
- Modify: `internal/project/validate_protocol.go:5-7,19-21`
- Modify: `internal/project/protocol_schema.go:6-9`
- Test: `internal/project/validate_protocol_test.go:18-20,42-50`

**Interfaces:** none (pure config validation).

- [ ] **Step 1: Write the failing tests**

In `internal/project/validate_protocol_test.go`, replace these two table rows (currently asserting rejection):

```go
		{name: "text framing rejected", p: &Protocol{Framing: "text"}, actors: nil, wantErr: "framing"},
		{name: "binary framing rejected", p: &Protocol{Framing: "binary"}, actors: nil, wantErr: "framing"},
```

with:

```go
		{name: "text framing ok", p: &Protocol{Framing: "text"}, actors: nil, wantErr: ""},
		{name: "binary framing ok", p: &Protocol{Framing: "binary"}, actors: nil, wantErr: ""},
		{name: "invalid framing rejected", p: &Protocol{Framing: "raw"}, actors: nil, wantErr: "framing"},
```

Then in `TestValidateIntegrationRejectsBadProtocol`, change the framing value from `text` (now valid) to `raw`:

```go
func TestValidateIntegrationRejectsBadProtocol(t *testing.T) {
	cfg := &Config{
		Services: []Service{{Name: "rt", URL: "http://x", Protocol: &Protocol{Framing: "raw"}}},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("want validation error for invalid framing")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -run 'TestValidateProtocol|TestValidateIntegrationRejectsBadProtocol' -v ./internal/project/`
Expected: FAIL — `text framing ok` / `binary framing ok` error (currently rejected), and `invalid framing rejected` may already pass by accident but the error wording check is loose; the integration test fails because `raw` is also rejected today but the row flip is the real driver.

- [ ] **Step 3: Write minimal implementation**

In `internal/project/validate_protocol.go`, replace the framing map and error:

```go
// validProtocolFraming is the set of framing values the WS executor supports.
var validProtocolFraming = map[string]bool{"": true, "json": true, "text": true, "binary": true}
```

```go
	if !validProtocolFraming[p.Framing] {
		return fmt.Errorf("protocol.framing %q must be json, text, or binary", p.Framing)
	}
```

In `internal/project/protocol_schema.go`, replace the `Framing` field comment:

```go
	// Framing is the wire framing: "json" (the default when empty), "text", or
	// "binary". json routes receive by type_path over text frames; text matches a
	// whole-frame string exactly; binary matches whole-frame bytes exactly with
	// the message/type carried as base64. See the WS framing design spec.
	Framing string `yaml:"framing,omitempty"`
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -run 'TestValidateProtocol|TestValidateIntegrationRejectsBadProtocol' -v ./internal/project/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/project/validate_protocol.go internal/project/protocol_schema.go internal/project/validate_protocol_test.go
git commit -m "feat(project): accept text/binary protocol framing"
```

---

## Task 2: framing helpers (framingOf, matchType, frameForResult)

**Files:**
- Modify: `internal/head/agent/ws_protocol.go:3-9` (imports) and add helpers after `extractTypePath` (after line 52)
- Test: `internal/head/agent/ws_protocol_test.go` (append tests; add import)

**Interfaces:**
- Produces:
  - `func framingOf(entry *wsEntry) string` — `entry.protocol.Framing`, or `""` (= json) when no protocol.
  - `func matchType(framing string, data []byte, want, typePath string) bool` — framing-aware receive match predicate.
  - `func frameForResult(framing string, data []byte) string` — renders bytes for a `WSResult` string field.

- [ ] **Step 1: Write the failing tests**

Add `"encoding/base64"` to the import block of `internal/head/agent/ws_protocol_test.go`:

```go
import (
	"encoding/base64"
	"reflect"
	"testing"

	"github.com/binoctal/cerberus/internal/project"
)
```

Append these tests:

```go
func TestMatchType(t *testing.T) {
	jsonMsg := []byte(`{"type":"go"}`)
	binWant := base64.StdEncoding.EncodeToString([]byte{0x00, 0xff})
	cases := []struct {
		name     string
		framing  string
		data     []byte
		want     string
		typePath string
		expect   bool
	}{
		{"json match", "json", jsonMsg, "go", "type", true},
		{"json no match", "json", jsonMsg, "other", "type", false},
		{"empty framing = json", "", jsonMsg, "go", "type", true},
		{"text exact match", "text", []byte("READY"), "READY", "", true},
		{"text no match", "text", []byte("READY"), "PENDING", "", false},
		{"binary match", "binary", []byte{0x00, 0xff}, binWant, "", true},
		{"binary no match", "binary", []byte{0x00, 0xff}, base64.StdEncoding.EncodeToString([]byte{0x01}), "", false},
		{"binary invalid want no match", "binary", []byte{0x00}, "@@@@", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := matchType(tc.framing, tc.data, tc.want, tc.typePath); got != tc.expect {
				t.Fatalf("matchType(%q,...) = %v, want %v", tc.framing, got, tc.expect)
			}
		})
	}
}

func TestFrameForResult(t *testing.T) {
	if got := frameForResult("binary", []byte{0x00, 0xff}); got != base64.StdEncoding.EncodeToString([]byte{0x00, 0xff}) {
		t.Fatalf("binary frameForResult = %q", got)
	}
	if got := frameForResult("text", []byte("hi")); got != "hi" {
		t.Fatalf("text frameForResult = %q", got)
	}
	if got := frameForResult("", []byte("hi")); got != "hi" {
		t.Fatalf("empty framing frameForResult = %q", got)
	}
}

func TestFramingOf(t *testing.T) {
	if got := framingOf(&wsEntry{}); got != "" {
		t.Fatalf("nil protocol framing = %q, want empty", got)
	}
	if got := framingOf(&wsEntry{protocol: &project.Protocol{Framing: "binary"}}); got != "binary" {
		t.Fatalf("framing = %q, want binary", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -run 'TestMatchType|TestFrameForResult|TestFramingOf' -v ./internal/head/agent/`
Expected: FAIL — `matchType`/`frameForResult`/`framingOf` undefined.

- [ ] **Step 3: Write minimal implementation**

In `internal/head/agent/ws_protocol.go`, update the import block:

```go
import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strings"

	"github.com/binoctal/cerberus/internal/project"
)
```

Append these helpers immediately after `extractTypePath` (after the function ending at line 52, before the `WSProtocolIndex` comment):

```go
// framingOf returns the effective wire framing for a connection. Empty (no
// protocol, or a protocol with no framing) means json — the M0/M1 default.
func framingOf(entry *wsEntry) string {
	if entry.protocol != nil {
		return entry.protocol.Framing
	}
	return ""
}

// matchType reports whether a received frame satisfies the awaited type under
// the connection's framing. json routes by type_path; text matches the whole
// frame text exactly; binary matches the whole frame bytes exactly (want is
// base64). A binary want that is not valid base64 never matches.
func matchType(framing string, data []byte, want, typePath string) bool {
	switch framing {
	case "text":
		return string(data) == want
	case "binary":
		got, err := base64.StdEncoding.DecodeString(want)
		if err != nil {
			return false
		}
		return bytes.Equal(got, data)
	default: // "" or "json"
		t, ok := extractTypePath(data, typePath)
		return ok && t == want
	}
}

// frameForResult renders received bytes for a WSResult string field under the
// connection's framing. binary frames are base64-encoded; text/json frames are
// the raw UTF-8 text.
func frameForResult(framing string, data []byte) string {
	if framing == "binary" {
		return base64.StdEncoding.EncodeToString(data)
	}
	return string(data)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -run 'TestMatchType|TestFrameForResult|TestFramingOf|TestExtractTypePath|TestExtractPath' -v ./internal/head/agent/`
Expected: PASS (new tests + existing extract tests unchanged).

- [ ] **Step 5: Commit**

```bash
git add internal/head/agent/ws_protocol.go internal/head/agent/ws_protocol_test.go
git commit -m "feat(ws): add framing-aware match and codec helpers"
```

---

## Task 3: doSend framing-aware (binary frame + base64 codec)

**Files:**
- Modify: `internal/head/agent/websocket.go:3-21` (imports: add `encoding/base64`) and `doSend` (`websocket.go:425-437`)
- Test: `internal/head/agent/websocket_test.go` (append tests; add import)

**Interfaces:**
- Consumes: `framingOf(entry *wsEntry) string` (Task 2).

- [ ] **Step 1: Write the failing tests**

Add `"encoding/base64"` to the import block of `internal/head/agent/websocket_test.go` (insert alphabetically before `"encoding/json"`):

```go
import (
	"context"
	"encoding/base64"
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
```

Append these tests:

```go
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -run 'TestSendBinaryFrameOpcodeAndPayload|TestSendTextFrameOpcode|TestSendBinaryInvalidBase64Fails' -v ./internal/head/agent/`
Expected: FAIL — binary test gets opcode `MessageText` (hardcoded), payload mismatch; base64 test gets success instead of error.

- [ ] **Step 3: Write minimal implementation**

In `internal/head/agent/websocket.go`, add `"encoding/base64"` to the import block (alphabetically, before `"fmt"`):

```go
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
```

Replace `doSend` (`websocket.go:425-437`) with:

```go
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -run 'TestSendBinaryFrameOpcodeAndPayload|TestSendTextFrameOpcode|TestSendBinaryInvalidBase64Fails|TestWSSendReusesConnection' -v ./internal/head/agent/`
Expected: PASS (new send tests + the existing M0 send regression test).

- [ ] **Step 5: Commit**

```bash
git add internal/head/agent/websocket.go internal/head/agent/websocket_test.go
git commit -m "feat(ws): send binary frames with base64 codec"
```

---

## Task 4: doReceive framing-aware + assert guard + result encoding

**Files:**
- Modify: `internal/head/agent/websocket.go` `doReceive` (`websocket.go:439-489`). (`encoding/base64` already imported by Task 3.)
- Test: `internal/head/agent/websocket_test.go` (append tests; `encoding/base64` already imported by Task 3)

**Interfaces:**
- Consumes: `framingOf`, `matchType`, `frameForResult` (Task 2). `encoding/base64` (Task 3 import).

- [ ] **Step 1: Write the failing tests**

Append these tests to `internal/head/agent/websocket_test.go`:

```go
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -run 'TestReceiveTextExactMatch|TestReceiveBinaryExactMatch|TestReceiveBinaryInvalidTypeFails|TestReceiveAssertRejectedUnderTextFraming' -v ./internal/head/agent/`
Expected: FAIL — text/binary receive never matches today (JSON-decode of non-JSON → no match → timeout); invalid-type test times out instead of fast-erroring; assert-under-text evaluates (and silently mis-fails) instead of the guard.

- [ ] **Step 3: Write minimal implementation**

Replace `doReceive` (`websocket.go:439-489`) with:

```go
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
```

- [ ] **Step 4: Run tests to verify they pass (incl. json regression)**

First, the new framing tests:

Run: `go test -run 'TestReceiveTextExactMatch|TestReceiveBinaryExactMatch|TestReceiveBinaryInvalidTypeFails|TestReceiveAssertRejectedUnderTextFraming' -v ./internal/head/agent/`
Expected: PASS.

Then the full package for the M1/M2 json + assert regression (json path is byte-identical, so `TestWSReceiveMatchesByType`, `TestWSReceiveTimeout`, `TestReceiveMatchesByTypePath`, and all `TestReceiveAssert*` must still pass):

Run: `go test -v ./internal/head/agent/`
Expected: PASS — no regressions.

- [ ] **Step 5: Commit**

```bash
git add internal/head/agent/websocket.go internal/head/agent/websocket_test.go
git commit -m "feat(ws): framing-aware receive with exact match and base64 results"
```

---

## Task 5: Handshake loop framing-aware

**Files:**
- Modify: `internal/head/agent/websocket.go` handshake loop (`websocket.go:223-240`). (Helpers + imports already in place.)
- Test: `internal/head/agent/websocket_test.go` (append tests)

**Interfaces:**
- Consumes: `framingOf`, `matchType`, `frameForResult` (Task 2).

- [ ] **Step 1: Write the failing tests**

Append these tests to `internal/head/agent/websocket_test.go`:

```go
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -run 'TestConnectRoleHandshakeTextFraming|TestConnectRoleHandshakeBinaryFraming' -v ./internal/head/agent/`
Expected: FAIL — the handshake loop still uses `extractTypePath`, so "READY" (not JSON) and the binary frame never match → handshake timeout → connect fails.

- [ ] **Step 3: Write minimal implementation**

In `internal/head/agent/websocket.go`, replace the handshake-loop block (`websocket.go:223-240`, the `matched := false` for-loop) with:

```go
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
```

- [ ] **Step 4: Run tests to verify they pass (incl. json handshake regression)**

Run: `go test -run 'TestConnectRoleHandshake' -v ./internal/head/agent/`
Expected: PASS — new text/binary handshake tests pass; existing `TestConnectRoleHandshakeSuccess` and `TestConnectRoleHandshakeTimeoutFailsAndCleansUp` (json framing) still pass.

- [ ] **Step 5: Commit**

```bash
git add internal/head/agent/websocket.go internal/head/agent/websocket_test.go
git commit -m "feat(ws): framing-aware role handshake loop"
```

---

## Task 6: Document text/binary framing (executor doc + steer prompt)

**Files:**
- Modify: `cerberus-docs/executors/websocket.md` (framing table row, ws_send/ws_receive fields, Notes, M0 fallback; add a Framing subsection)
- Modify: `internal/head/agent/prompts.go` (steer prompt: ws_send, ws_receive, Protocol declarations lines)

**Interfaces:** none (docs + prompt hint).

**Audit reminder (M2-field-assertions defect class):** every doc/prompt bullet that still says framing is "json only / text-binary reserved for M2 / rejected by validation" must be updated. grep before claiming done: `grep -rn "reserved for M2\|json only\|rejected by validation" cerberus-docs/ internal/head/agent/prompts.go`.

- [ ] **Step 1: Update the executor doc**

In `cerberus-docs/executors/websocket.md`:

(a) `ws_send` table (the `message` row) — change:

```markdown
| `message` | string | yes | Message to send |
```

to:

```markdown
| `message` | string | yes | Message to send. Under `binary` framing this is base64 (standard padding); decoded bytes are written as a binary frame. Under `json`/`text` (or no protocol) it is written as-is as a text frame. |
```

(b) `ws_receive` table (the `type` row) — change:

```markdown
| `type` | string | yes | Wait for a message whose top-level `type` matches |
```

to:

```markdown
| `type` | string | yes | The awaited message. Under `json`/no-protocol: the value at `type_path` (top-level `type` by default). Under `text`: the whole frame must equal this string. Under `binary`: base64 (standard padding) of the exact expected bytes. |
```

(c) Protocol Declaration `framing` table row — change:

```markdown
| `framing` | string | `json` | Wire framing. M1 supports `json` only; `text`/`binary` are reserved for M2 and rejected by validation. |
```

to:

```markdown
| `framing` | string | `json` | Wire framing: `json` (default), `text`, or `binary`. See [Framing](#framing). |
```

(d) Notes bullet that ends "`text`/`binary` framing remains deferred." (around line 121) — replace that sentence so the bullet reads:

```markdown
- Roles and the per-role mandatory handshake are declarable via
  [Protocol declaration > Roles](#roles) (M2). Field-level `assert` checks on
  `ws_receive` are documented under [Field assertions](#field-assertions) (M2).
  `text`/`binary` framing is documented under [Framing](#framing) (M2).
```

(e) M0 fallback section — change "framing is JSON" wording so it reads (keep the rest of the paragraph):

```markdown
A service without a `protocol:` block behaves exactly as M0: `ws_receive`
matches top-level `type`, framing is JSON (text frames), and auth is **not**
auto-injected (the LLM puts credentials into url/headers/subprotocols itself).
A declared `text`/`binary` framing is a strict enhancement, never a replacement.
```

(f) Add a new `### Framing` subsection immediately after the `### Roles` subsection (before `### M0 fallback`):

```markdown
### Framing

`protocol.framing` declares the wire framing for the connection. It bundles
three facts the executor derives and acts on deterministically; the LLM only
authors the `message`/`type` content in the declared form.

| framing | send | receive match | `assert` |
|---|---|---|---|
| `json` (default) | text frame, message as-is | value at `type_path` equals `type` | path→value on the matched JSON |
| `text` | text frame, message as-is | whole frame text equals `type` (exact) | rejected (json-only) |
| `binary` | binary frame, `message` is base64 → bytes | whole frame bytes equal base64-decoded `type` (exact) | rejected (json-only) |

**Binary codec.** A JSON string cannot carry arbitrary bytes, so binary content
travels as base64 (`encoding/base64.StdEncoding`, standard padding) in the
string-typed `message` (send), `type` (receive match target), and
`MatchedMessage`/`SeenMessages` (receive result). An invalid-base64 `message`
on send, or `type` on receive, fails fast with a clear non-secret error (the
receive error fires before any read, rather than timing out).

**Matching is exact equality only.** `text` matches the whole frame string;
`binary` matches the whole frame bytes. There is no substring/prefix/regex
predicate (that would be an expression engine — M0 Constraint 3). When the full
frame cannot be predicted ahead (a computed binary response, a variable text
payload), exact-match is not usable as `type`; fall back to a non-decisive
`ws_receive` and let the Examiner judge content — the same fallback used for
unpredictable JSON field values. Scan-and-filter is preserved in all framings:
non-matching frames still accumulate into `SeenMessages`.

`assert` is JSON-only; under `text`/`binary` a receive with a non-empty `assert`
fails immediately with `receive: assert requires json framing`.

Design rationale (why exact-match over receive-next, why base64 over hex) and
the dogfooding recourse are in the M2 framing design spec:
[`cerberus-docs/superpowers/specs/2026-07-21-ws-framing-design.md`](../superpowers/specs/2026-07-21-ws-framing-design.md).
```

- [ ] **Step 2: Update the steer prompt (single raw-string literal — inline edits only)**

In `internal/head/agent/prompts.go`, make these three inline edits inside `promptSteerSystem` (no backticks, no concatenation):

The `ws_send` bullet (currently):
```
- ws_send {connection_id, message}: send a text/JSON message on an open conn.
```
becomes:
```
- ws_send {connection_id, message}: send a message on an open conn. Under binary framing (protocol.framing: binary), message is base64 of the bytes (a binary frame); otherwise it is the text/JSON string sent as a text frame.
```

The `ws_receive` bullet's first sentence (currently):
```
- ws_receive {connection_id, type, timeout?, decisive?, assert?}: wait for a message whose top-level JSON type field equals the type argument. Other messages are kept as evidence.
```
becomes:
```
- ws_receive {connection_id, type, timeout?, decisive?, assert?}: wait for a message matching type. Under json (or no protocol) type is the value at the declared type_path (top-level type by default); under text framing type is the whole frame string (exact); under binary framing type is base64 of the exact expected bytes. Other messages are kept as evidence.
```

The Protocol declarations paragraph (currently ends):
```
Protocol declarations: when a service declares a protocol, its auth is injected by the executor (do not duplicate credentials) and ws_receive matches by the declared type_path. The routing key value you pass to ws_receive (the "type" argument) is the expected value at that path, not the path itself.
```
becomes:
```
Protocol declarations: when a service declares a protocol, its auth is injected by the executor (do not duplicate credentials), ws_receive matches by the declared type_path (json framing; for text/binary framing it matches the whole frame — see below), and framing selects the wire frame type. Under binary framing, encode ws_send message and ws_receive type as base64 (standard padding). The routing key value you pass to ws_receive (the "type" argument) is the expected value at that path, not the path itself.
```

- [ ] **Step 3: Verify build + lint + test green**

Run: `make check`
Expected: exit 0 (fmt + lint + test -race). Docs/prompt-only change; no behavior change since Task 5. Confirm the steer prompt still compiles (single raw-string literal intact — no backticks introduced).

Run the stale-bullet audit: `grep -rn "reserved for M2\|json only\|rejected by validation" cerberus-docs/ internal/head/agent/prompts.go`
Expected: no matches.

- [ ] **Step 4: Commit**

```bash
git add cerberus-docs/executors/websocket.md internal/head/agent/prompts.go
git commit -m "docs(ws): document text/binary framing in executor doc and steer prompt"
```

---

## Final Verification

- [ ] `make check` green across the whole branch.
- [ ] `go build ./...` clean (the doSend/doReceive/handshake signature changes are internal; no external caller signature changed, but verify).
- [ ] No stale "json only / reserved for M2 / rejected by validation" wording anywhere (`grep -rn` over `cerberus-docs/` and `internal/head/agent/prompts.go`).
- [ ] `-race` clean (server-goroutine counters in new send-capture tests use buffered channels, not shared counters — confirm no new data races).

## Self-Review Notes

- **Spec coverage:** D1 (protocol-level, no per-action field) → no new action types anywhere ✓; D2 (match predicate) → Task 2 `matchType` + Tasks 4/5 ✓; D3 (base64 codec) → Tasks 2/3/4 ✓; D4 (assert json-only guard) → Task 4 ✓; D5 (handshake framing-aware) → Task 5 ✓; D6 (json/M0 fallback byte-identical) → Task 4 json regression run ✓; validation/schema → Task 1 ✓; doc/prompt → Task 6 ✓.
- **Type consistency:** `framingOf`/`matchType`/`frameForResult` signatures are identical in Task 2 (definition) and Tasks 3/4/5 (use). `wsEntry` field is `protocol` (not `proto`) — `framingOf(&wsEntry{protocol: ...})` in Task 2 test matches the struct at `websocket.go:40-45`.
- **No placeholders:** every code step shows complete code; every test step shows complete test bodies.
