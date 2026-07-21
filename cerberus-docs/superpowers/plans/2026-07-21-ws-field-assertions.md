# WebSocket Realtime Engine (M2) — Field-Level Assertions — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a `ws_receive` optionally assert that specific fields in the matched message equal specific values, checked deterministically by the executor, while preserving M1 arrival-only behavior when no assertions are declared.

**Architecture:** A `WSReceiveAction.Assert map[string]any` (path → expected value) is evaluated after the M1 type match. A generalized `extractPath` (factored out of M1's `extractTypePath`) walks the dotted path; a `valueEqual` helper compares with numeric normalization. Any failed assertion fails the receive (`OK=false`) with a precise message, the matched message still returned as evidence. Constrained equality only — no expression engine (M0 Constraint 3 preserved).

**Tech Stack:** Go 1.25, `github.com/coder/websocket` v1.8.14, `encoding/json`, `reflect`, `sort`, table-driven Go tests mirroring `internal/head/agent/websocket_test.go` and `internal/types/ws_actions_test.go`.

## Global Constraints

- Go 1.25, module `github.com/binoctal/cerberus`, pure-Go (no CGo).
- WebSocket library is `github.com/coder/websocket` v1.8.14; do NOT add `nhooyr.io/websocket` or any expression/JSONPath/evaluator dependency — assertions are **constrained path→value equality only**, not an expression language (M0 Constraint 3).
- Comments and commit messages in English. Commit author `binoctal <binoctal@gmail.com>`, no `Co-Authored-By`.
- Follow existing comment density and naming; table-driven tests.
- All documentation under `cerberus-docs/` only (never `docs/`).
- Each task leaves the tree compiling and the focused tests green; the final task runs `make check` (fmt+lint+test, `-race`).
- Adding a field to an existing action type does NOT require registry/deref/plugin wiring.

**Spec:** `cerberus-docs/superpowers/specs/2026-07-21-ws-field-assertions-design.md`

---

## File Structure

**Modify (no new files):**
- `internal/head/agent/ws_protocol.go` — `extractPath` (new); `extractTypePath` refactored to call it.
- `internal/types/actions_http.go` — `WSReceiveAction.Assert` + `Validate` empty-path check.
- `internal/head/agent/websocket.go` — `doReceive` assertion evaluation; `checkAsserts` + `valueEqual` + `numericFloat` helpers.
- `internal/head/agent/prompts.go` — `ws_receive` + content bullets (inline raw-string edit).
- `cerberus-docs/executors/websocket.md` — `assert` documentation.

**Tests:** append to `internal/head/agent/ws_protocol_test.go` (create if absent), `internal/types/ws_actions_test.go`, `internal/head/agent/websocket_test.go`, `internal/head/agent/prompts_test.go`.

---

## Task 1: Generalize the JSON path walker (`extractPath`)

**Files:**
- Modify: `internal/head/agent/ws_protocol.go` (`extractTypePath` → refactor; add `extractPath`)
- Test: `internal/head/agent/ws_protocol_test.go` (append; create the file if it does not exist)

**Interfaces:**
- Produces: `extractPath(data []byte, path string) (any, bool)` — used by `extractTypePath` (this task) and `checkAsserts` (Task 3). `extractTypePath` keeps its exact current signature/contract.

- [ ] **Step 1: Write the failing test**

Append to `internal/head/agent/ws_protocol_test.go` (create the file with `package agent` and the needed imports if it does not exist):
```go
package agent

import (
	"reflect"
	"testing"
)

func TestExtractPath(t *testing.T) {
	cases := []struct {
		name string
		data string
		path string
		want any
		ok   bool
	}{
		{"empty path reads type", `{"type":"devices:sync"}`, "", "devices:sync", true},
		{"nested object leaf", `{"payload":{"approved":true}}`, "payload.approved", true, true},
		{"string leaf", `{"type":"x","role":"admin"}`, "role", "admin", true},
		{"number leaf decodes float64", `{"n":5}`, "n", float64(5), true},
		{"absent key", `{"a":1}`, "b", nil, false},
		{"non-object mid-path", `{"a":"str"}`, "a.b", nil, false},
		{"present null", `{"v":null}`, "v", nil, true},
		{"not json", `not-json`, "a", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := extractPath([]byte(tc.data), tc.path)
			if ok != tc.ok {
				t.Fatalf("extractPath(%q) ok = %v, want %v (got %v)", tc.path, ok, tc.ok, got)
			}
			if ok && !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("extractPath(%q) = %#v, want %#v", tc.path, got, tc.want)
			}
		})
	}
}
```
(If `ws_protocol_test.go` already exists with a different import block, merge — do not duplicate `package agent` or imports.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/head/agent/ -run TestExtractPath -v`
Expected: FAIL — `extractPath undefined`.

- [ ] **Step 3: Write minimal implementation**

In `internal/head/agent/ws_protocol.go`, replace the body of `extractTypePath` and add `extractPath` immediately above it. The current `extractTypePath` is:
```go
func extractTypePath(data []byte, path string) (string, bool) {
	if path == "" {
		path = "type"
	}
	var obj map[string]any
	if err := json.Unmarshal(data, &obj); err != nil {
		return "", false
	}
	cur := any(obj)
	for _, key := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return "", false
		}
		next, exists := m[key]
		if !exists {
			return "", false
		}
		cur = next
	}
	s, ok := cur.(string)
	return s, ok
}
```
Replace it with:
```go
// extractPath walks a dotted path through a JSON message and returns the leaf
// value. An empty path returns the top-level "type" field's value (M0 routing
// semantics, shared with extractTypePath). Returns (value, true) when the path
// resolves to a present leaf — including a JSON null, which is a present nil —
// and (nil, false) if the message is not a JSON object, the path traverses a
// non-object, or the leaf key is absent.
func extractPath(data []byte, path string) (any, bool) {
	if path == "" {
		path = "type"
	}
	var obj map[string]any
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil, false
	}
	cur := any(obj)
	for _, key := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		next, exists := m[key]
		if !exists {
			return nil, false
		}
		cur = next
	}
	return cur, true
}

// extractTypePath returns the routing key at the dotted path within a JSON
// message as a string. Empty path means top-level "type" (M0 behavior). Returns
// ("", false) if the message is not a JSON object, the path is absent, or the
// leaf is not a string — so the M0 fallback reproduces messageType semantics
// exactly (a non-string type field does not match).
func extractTypePath(data []byte, path string) (string, bool) {
	v, ok := extractPath(data, path)
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}
```
(`json` and `strings` are already imported in this file.)

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/head/agent/ -run 'TestExtractPath|TestExtractTypePath|TestMessageType' -v`
Expected: PASS — new `TestExtractPath` green AND any existing `extractTypePath`/message-type tests still green (the refactor preserves the string-only contract).

- [ ] **Step 5: Commit**

```bash
git add internal/head/agent/ws_protocol.go internal/head/agent/ws_protocol_test.go
git commit -m "feat(ws): generalize JSON path walker into extractPath"
```

---

## Task 2: `WSReceiveAction.Assert` field + validation

**Files:**
- Modify: `internal/types/actions_http.go` (`WSReceiveAction`; its `Validate`)
- Test: `internal/types/ws_actions_test.go` (append)

**Interfaces:**
- Produces: `WSReceiveAction.Assert map[string]any` (json `assert,omitempty`) — consumed by `doReceive` (Task 3). `Validate` rejects empty path keys.

- [ ] **Step 1: Write the failing tests**

Append to `internal/types/ws_actions_test.go`:
```go
func TestWSReceiveActionAssertRoundTrip(t *testing.T) {
	envelope, err := MarshalAction(&WSReceiveAction{
		ConnectionID: "c1", Type: "approval",
		Assert: map[string]any{"payload.approved": true},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got, err := UnmarshalAction(envelope)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	r, ok := got.(WSReceiveAction)
	if !ok {
		t.Fatalf("type: %+v", got)
	}
	if r.Assert["payload.approved"] != true {
		t.Fatalf("assert round-trip lost: %+v", r.Assert)
	}
}

func TestWSReceiveActionValidateRejectsEmptyAssertKey(t *testing.T) {
	a := WSReceiveAction{ConnectionID: "c1", Type: "x", Assert: map[string]any{"": true}}
	if err := a.Validate(); err == nil {
		t.Fatal("empty assert path key should be rejected")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/types/ -run 'TestWSReceiveActionAssertRoundTrip|TestWSReceiveActionValidateRejectsEmptyAssertKey' -v`
Expected: FAIL — `r.Assert undefined` / `unknown field assert`.

- [ ] **Step 3: Write minimal implementation**

In `internal/types/actions_http.go`, read the current `WSReceiveAction` struct and its `Validate` method first. Add the `Assert` field to `WSReceiveAction` (after `Decisive`):
```go
	// Assert optionally declares field-level equality checks evaluated against the
	// matched message after type matching. Each key is a dotted JSON object path
	// (e.g. "payload.approved"); each value is the expected value. All must hold
	// for the receive to succeed; a failed assertion fails the receive. Empty
	// means no assertions (M1 arrival-only behavior). Constrained equality only
	// — no expression engine.
	Assert map[string]any `json:"assert,omitempty"`
```
In `Validate`, after the existing checks (and before `return nil`), add:
```go
	for k := range a.Assert {
		if strings.TrimSpace(k) == "" {
			return fmt.Errorf("assert path key must be non-empty")
		}
	}
```
(Add `"strings"` to the file's imports if it is not already present.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/types/ -v`
Expected: PASS (new assert tests + existing ws_actions tests green).

- [ ] **Step 5: Commit**

```bash
git add internal/types/actions_http.go internal/types/ws_actions_test.go
git commit -m "feat(types): add Assert to WSReceiveAction"
```

---

## Task 3: `doReceive` assertion evaluation + `valueEqual`

**Files:**
- Modify: `internal/head/agent/websocket.go` (`doReceive`; add `checkAsserts`, `valueEqual`, `numericFloat`)
- Test: `internal/head/agent/websocket_test.go` (append)

**Interfaces:**
- Consumes: `extractPath` (Task 1), `WSReceiveAction.Assert` (Task 2).
- Produces: assertion evaluation inside `doReceive` — a matched message with a failing assertion returns `WSResult{OK:false, Err:"receive: assert <path>: expected <v>, got <actual>", MatchedMessage:..., SeenMessages:...}`.

**Plan-vs-reality correction:** the tests below reference `WSConnectAction`/`WSReceiveAction` without a package prefix in this description for brevity, but `websocket_test.go` is `package agent` with NO type alias — qualify every action type as `types.WSConnectAction` / `types.WSReceiveAction` (the `types` import is already present). `newWSTestServer` and `newWSExecutor` exist in the package's `_test.go` files.

- [ ] **Step 1: Write the failing tests**

Append to `internal/head/agent/websocket_test.go`:
```go
func TestReceiveAssertPass(t *testing.T) {
	wsURL := newWSTestServer(t, func(conn *websocket.Conn) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = conn.Write(ctx, websocket.MessageText, []byte(`{"type":"approval","payload":{"approved":true}}`))
		_, _, _ = conn.Read(ctx)
	})
	ex := newWSExecutor()
	ctx := context.Background()
	ex.Execute(ctx, types.WSConnectAction{URL: wsURL, ConnectionID: "c1"})
	res := ex.Execute(ctx, types.WSReceiveAction{
		ConnectionID: "c1", Type: "approval",
		Assert: map[string]any{"payload.approved": true},
	})
	ws, ok := res.(types.WSResult)
	if !ok || !ws.Success() {
		t.Fatalf("receive failed: %+v", res)
	}
	if !strings.Contains(ws.MatchedMessage, "approval") {
		t.Fatalf("matched message not returned: %s", ws.MatchedMessage)
	}
}

func TestReceiveAssertValueMismatchFails(t *testing.T) {
	wsURL := newWSTestServer(t, func(conn *websocket.Conn) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = conn.Write(ctx, websocket.MessageText, []byte(`{"type":"approval","payload":{"approved":false}}`))
		_, _, _ = conn.Read(ctx)
	})
	ex := newWSExecutor()
	ctx := context.Background()
	ex.Execute(ctx, types.WSConnectAction{URL: wsURL, ConnectionID: "c1"})
	res := ex.Execute(ctx, types.WSReceiveAction{
		ConnectionID: "c1", Type: "approval",
		Assert: map[string]any{"payload.approved": true},
	})
	ws, ok := res.(types.WSResult)
	if ok && ws.Success() {
		t.Fatalf("receive should fail on assertion mismatch: %+v", res)
	}
	if !strings.Contains(ws.Err, "payload.approved") || !strings.Contains(ws.Err, "true") || !strings.Contains(ws.Err, "false") {
		t.Fatalf("err should name path/expected/actual: %q", ws.Err)
	}
	if !strings.Contains(ws.MatchedMessage, "approval") {
		t.Fatalf("matched message should still be evidence on assert failure: %s", ws.MatchedMessage)
	}
}

func TestReceiveAssertMissingPathFails(t *testing.T) {
	wsURL := newWSTestServer(t, func(conn *websocket.Conn) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = conn.Write(ctx, websocket.MessageText, []byte(`{"type":"x"}`))
		_, _, _ = conn.Read(ctx)
	})
	ex := newWSExecutor()
	ctx := context.Background()
	ex.Execute(ctx, types.WSConnectAction{URL: wsURL, ConnectionID: "c1"})
	res := ex.Execute(ctx, types.WSReceiveAction{
		ConnectionID: "c1", Type: "x",
		Assert: map[string]any{"payload.approved": true},
	})
	ws, _ := res.(types.WSResult)
	if ws.Success() {
		t.Fatalf("absent assert path should fail: %+v", res)
	}
	if !strings.Contains(ws.Err, "payload.approved") || !strings.Contains(ws.Err, "<missing>") {
		t.Fatalf("err should report missing path: %q", ws.Err)
	}
}

func TestReceiveAssertNumericNormalization(t *testing.T) {
	wsURL := newWSTestServer(t, func(conn *websocket.Conn) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = conn.Write(ctx, websocket.MessageText, []byte(`{"type":"x","n":5}`))
		_, _, _ = conn.Read(ctx)
	})
	ex := newWSExecutor()
	ctx := context.Background()
	ex.Execute(ctx, types.WSConnectAction{URL: wsURL, ConnectionID: "c1"})
	// Expected int 5 must match the JSON-decoded float64 5.
	res := ex.Execute(ctx, types.WSReceiveAction{
		ConnectionID: "c1", Type: "x",
		Assert: map[string]any{"n": 5},
	})
	if !res.Success() {
		t.Fatalf("numeric assert should pass with normalization: %+v", res)
	}
}

func TestReceiveAssertMultipleReportsFirstSorted(t *testing.T) {
	wsURL := newWSTestServer(t, func(conn *websocket.Conn) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = conn.Write(ctx, websocket.MessageText, []byte(`{"type":"x","a":1,"z":1}`))
		_, _, _ = conn.Read(ctx)
	})
	ex := newWSExecutor()
	ctx := context.Background()
	ex.Execute(ctx, types.WSConnectAction{URL: wsURL, ConnectionID: "c1"})
	// Both fail; sorted order reports "a" before "z".
	res := ex.Execute(ctx, types.WSReceiveAction{
		ConnectionID: "c1", Type: "x",
		Assert: map[string]any{"z": 99, "a": 99},
	})
	ws, _ := res.(types.WSResult)
	if ws.Success() {
		t.Fatalf("assertions should fail: %+v", res)
	}
	// Both fail; sorted order reports "a" before "z".
	if !strings.Contains(ws.Err, "assert a") {
		t.Fatalf("should report the sorted-first failing path 'a': %q", ws.Err)
	}
	if strings.Contains(ws.Err, "assert z") {
		t.Fatalf("should not report 'z' (sorted-first is 'a'): %q", ws.Err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/head/agent/ -run 'TestReceiveAssert' -v`
Expected: FAIL — no assertion evaluation; the assert is ignored and the receive succeeds (so the mismatch/missing/multiple cases fail their `Success()` assertion; the pass/numeric cases happen to pass because they don't exercise the failure path — that is fine, the implementation step makes all deterministic).

- [ ] **Step 3: Write minimal implementation**

In `internal/head/agent/websocket.go`:

(a) Add `"sort"` to the import block (alongside the existing `"reflect"` if present, or add `"reflect"` too — it is used by `valueEqual`). Check the current imports; add what is missing.

(b) Add the helpers (place near `stripQuery`/`removeString`):
```go
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
```

(c) In `doReceive`, the current type-match branch is:
```go
		if t, ok := extractTypePath(data, path); ok && t == a.Type {
			return types.WSResult{OK: true, MatchedMessage: string(data), SeenMessages: seen, Latency: time.Since(start)}
		}
```
Replace it with:
```go
		if t, ok := extractTypePath(data, path); ok && t == a.Type {
			// Type matched. Evaluate field-level assertions (if any) against this
			// message in sorted path order; the first failure fails the receive
			// with a precise message. The matched message is returned as evidence
			// either way. No asserts → M1 arrival-only behavior.
			if p, exp, act, ok := checkAsserts(data, a.Assert); !ok {
				return types.WSResult{
					OK:             false,
					Err:            fmt.Sprintf("receive: assert %s: expected %v, got %v", p, exp, act),
					MatchedMessage: string(data),
					SeenMessages:   seen,
					Latency:        time.Since(start),
				}
			}
			return types.WSResult{OK: true, MatchedMessage: string(data), SeenMessages: seen, Latency: time.Since(start)}
		}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/head/agent/ -run 'TestReceive|TestWSReceive|TestWSConnect|TestConnectionNamespacing|TestReceiveSerialized' -race -v`
Expected: PASS — all assert tests green; existing M1 receive tests still green (no `assert` → unchanged path); `-race` clean.

- [ ] **Step 5: Commit**

```bash
git add internal/head/agent/websocket.go internal/head/agent/websocket_test.go
git commit -m "feat(ws): evaluate field-level asserts in doReceive"
```

---

## Task 4: Steer prompt + executor doc + `make check` green

**Files:**
- Modify: `internal/head/agent/prompts.go` (inline raw-string edit), `cerberus-docs/executors/websocket.md`
- Test: `internal/head/agent/prompts_test.go` (append)
- Verify: `make check`

**Interfaces:** none (documentation + prompt text + verification).

- [ ] **Step 1: Write the failing test**

Append to `internal/head/agent/prompts_test.go`:
```go
func TestSteerPromptMentionsAssert(t *testing.T) {
	for _, want := range []string{"assert", "deterministic"} {
		if !contains(promptSteerSystem, want) {
			t.Fatalf("steer prompt missing %q", want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/head/agent/ -run TestSteerPromptMentionsAssert -v`
Expected: FAIL — substrings absent.

- [ ] **Step 3: Write minimal implementation**

In `internal/head/agent/prompts.go`, inline-edit `promptSteerSystem` (single raw-string literal — no concatenation, no backticks). The current `ws_receive` bullet (around line 26) is:
```
- ws_receive {connection_id, type, timeout?, decisive?}: wait for a message whose top-level JSON type field equals the type argument. Other messages are kept as evidence.
```
Replace it with:
```
- ws_receive {connection_id, type, timeout?, decisive?, assert?}: wait for a message whose top-level JSON type field equals the type argument. Other messages are kept as evidence. Optional assert is a path-to-value map (e.g. {payload.approved: true}) checked deterministically against the matched message — every entry must hold or the receive fails (and fails the case if decisive). Use assert for precise content checks.
```
And the current content bullet (around line 32):
```
- Content assertions (e.g. payload.approved) are judged from the received message by the Examiner against the test case expectation — ws_receive only confirms the message arrived.
```
Replace it with:
```
- Content checks: by default ws_receive only confirms the awaited message arrived, and content (e.g. payload.approved) is judged by the Examiner against the expectation. For a deterministic check, add assert — a path-to-value map the executor verifies on the matched message, failing the receive on any mismatch. assert is path-to-value equality only (no expressions).
```

In `cerberus-docs/executors/websocket.md`, under the `### ws_receive` section (around line 30), document `assert`: the field (path→value map), evaluation (after the type match, all entries must hold, checked in sorted path order), failure semantics (the receive fails with a message naming path/expected/actual; the matched message is still returned as evidence; a decisive receive with a failing assert fails the case), the constrained-equality/no-evaluator constraint, numeric normalization (JSON float64 vs int), and the M1 fallback (no `assert` → arrival-only). Cross-link the design spec `../superpowers/specs/2026-07-21-ws-field-assertions-design.md`. Match the file's existing prose density.

- [ ] **Step 4: Run fmt + lint + test**

Run: `make check`
Expected: fmt clean, lint clean, all tests PASS including `-race`. Fix any lint nits (e.g. an unused import if `reflect`/`sort` ended up unused — they should not be).

- [ ] **Step 5: Commit**

```bash
git add internal/head/agent/prompts.go internal/head/agent/prompts_test.go cerberus-docs/executors/websocket.md
git commit -m "feat(agent): document field-level assert in steer prompt and executor doc"
```

---

## Definition of Done

- A `ws_receive` with `assert: {payload.approved: true}` matching a message whose `payload.approved` is `true` succeeds; matching `false` fails with a message naming path/expected/actual, the matched message still evidence.
- A `ws_receive` without `assert` behaves exactly as M1 (regression-green).
- Assertions are constrained path→value equality (no expression engine); numbers normalize (int 5 == float64 5).
- A decisive receive with a failing assertion fails (consistent with a decisive receive that times out).
- `make check` (fmt + lint + test, `-race`) is green; spec and plan committed under `cerberus-docs/`.
