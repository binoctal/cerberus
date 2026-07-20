# WebSocket Realtime Engine (M1) — Protocol Adaptation Layer — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a declarative `protocol:` block to a service (auth strategy + `type_path` + framing) that the WS executor consumes deterministically, plus complete M0's deferred per-case `connection_id` namespacing and `doReceive` read serialization — all with a nil-fallback to exact M0 behavior.

**Architecture:** A `Protocol` declaration on `Service` is turned into a `WSProtocolIndex{ByHost, ActorTokens}` at session setup and threaded into `WebSocketExecutor`. `ws_connect` resolves the protocol by dial host, **strip-then-injects** the declared auth (executor-authoritative; the static steer prompt cannot carry per-service facts), and stashes the protocol on the connection entry. `ws_receive` matches by the declared `type_path` (dotted-path resolver) and json framing. Connections are namespaced `<caseID>:<connectionID>` (caseID from `tc.ID` via ctx) and guarded by a per-conn read mutex. No protocol block → M0 behavior (top-level `type`, LLM-supplied auth).

**Tech Stack:** Go 1.25, `github.com/coder/websocket` v1.8.14, `gopkg.in/yaml.v3`, table-driven Go tests mirroring `internal/head/agent/http_test.go` and `internal/project/authflow_schema_test.go`.

## Global Constraints

- Go 1.25, module `github.com/binoctal/cerberus`, pure-Go (no CGo).
- WebSocket library is `github.com/coder/websocket` v1.8.14; do NOT add `nhooyr.io/websocket` or any expression/JSONPath/evaluator dependency (cerberus has no runtime evaluator — spec D5/M0 Constraint 3).
- Comments and commit messages in English. Commit author `binoctal <binoctal@gmail.com>`, no `Co-Authored-By`.
- Follow existing comment density and naming; table-driven tests.
- All documentation under `cerberus-docs/` only (never `docs/`).
- Each task leaves the tree compiling and `go test ./...` green; the final task runs `make check` (fmt+lint+test).
- Adding a field to an existing action type does NOT require registry/deref/plugin/multi wiring — but if any task adds a new `ActionType`, it MUST also be registered in `internal/types/actions_registry.go`, `wsPlugin.ActionTypes()` (`internal/head/agent/plugin_executors.go`), and the `internal/head/agent/multi_sandbox.go` switch, or `MultiExecutor` will not route it.

**Spec:** `cerberus-docs/superpowers/specs/2026-07-20-ws-realtime-engine-m1-design.md`

---

## File Structure

**Create:**
- `internal/project/protocol_schema.go` — `Protocol`, `ProtocolAuth` types.
- `internal/project/validate_protocol.go` — `ValidateProtocol`, `validateProtocol`.
- `internal/project/protocol_schema_test.go`, `internal/project/validate_protocol_test.go` — tests.
- `internal/head/agent/ws_protocol.go` — `WSProtocolIndex`, `BuildWSProtocolIndex`, the `extractTypePath` resolver, and `redactSecretQuery`.

**Modify:**
- `internal/project/schema.go` — `Service.Protocol`, `CredentialRef.RawToken`.
- `internal/project/validate.go` — Phase 6 call.
- `internal/head/agent/authflow.go` — `ResolveAuthHeader` returns the raw token too.
- `internal/session/auth_setup.go` — cache the raw token; signature update.
- `internal/head/agent/websocket.go` — `NewWebSocketExecutor` signature, `wsEntry` (`protocol`, `readMu`), caseID-from-ctx + namespaced keys, strip-then-inject auth, `type_path`/framing in `doReceive`, `readMu` guard, `resolveProtocol`/`actorToken`.
- `internal/head/agent/plugin_helpers.go`, `internal/head/agent/multi.go` — thread `*WSProtocolIndex`.
- `internal/session/run_phases_agent.go`, `internal/session/resume_phases_run.go` — build + pass the index.
- `internal/head/agent/execute_phases.go` — inject `tc.ID` on the per-case ctx.
- `internal/types/actions_http.go` — `WSConnectAction.CredentialRef`.
- `internal/types/result_ws.go` — URL redaction in `Summary`/`Evidence`.
- `internal/head/agent/prompts.go` — best-effort steer-prompt hint.
- `internal/head/agent/authflow_test.go` — update `ResolveAuthHeader` call sites.
- `cerberus-docs/executors/websocket.md` — document the `protocol:` block.

---

## Task 1: Protocol schema + Service.Protocol field

**Files:**
- Create: `internal/project/protocol_schema.go`
- Modify: `internal/project/schema.go:17-24` (Service struct)
- Test: `internal/project/protocol_schema_test.go` (create)

**Interfaces:**
- Consumes: nothing.
- Produces: `project.Protocol{Framing, TypePath string; Auth *ProtocolAuth}`, `project.ProtocolAuth{Strategy, Param, CredentialRef string}`, and `Service.Protocol *Protocol` — used by validation (Task 3), the index builder (Task 8), and the executor (Task 8/9).

- [ ] **Step 1: Write the failing test**

`internal/project/protocol_schema_test.go`:
```go
package project

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestProtocolYAMLRoundTrip(t *testing.T) {
	in := `
name: rt
url: http://localhost:8787
protocol:
  framing: json
  type_path: data.event
  auth:
    strategy: query
    param: token
    credential_ref: web-actor
`
	var svc Service
	if err := yaml.Unmarshal([]byte(in), &svc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if svc.Protocol == nil {
		t.Fatal("protocol is nil")
	}
	if svc.Protocol.Framing != "json" || svc.Protocol.TypePath != "data.event" {
		t.Fatalf("framing/type_path = %q/%q", svc.Protocol.Framing, svc.Protocol.TypePath)
	}
	if svc.Protocol.Auth == nil ||
		svc.Protocol.Auth.Strategy != "query" ||
		svc.Protocol.Auth.Param != "token" ||
		svc.Protocol.Auth.CredentialRef != "web-actor" {
		t.Fatalf("auth = %+v", svc.Protocol.Auth)
	}
}

func TestServiceWithoutProtocol(t *testing.T) {
	var svc Service
	if err := yaml.Unmarshal([]byte("name: x\nurl: http://x\n"), &svc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if svc.Protocol != nil {
		t.Fatalf("protocol should be nil when absent, got %+v", svc.Protocol)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/project/ -run TestProtocol -v`
Expected: FAIL — `unknown field protocol` / `svc.Protocol undefined`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/project/protocol_schema.go`:
```go
package project

// Protocol declares the stable, knowable-ahead-of-time WebSocket protocol
// facts for a service. A nil Protocol on a Service means "fall back to M0
// behavior" (top-level type matching, json framing, no auto auth).
type Protocol struct {
	// Framing is the wire framing. M1 supports "json" only (the default when
	// empty); "text"/"binary" are reserved for M2 and rejected by validation.
	Framing string `yaml:"framing,omitempty"`
	// TypePath is the dotted path to the message-routing key; default "type".
	TypePath string `yaml:"type_path,omitempty"`
	// Auth declares how credentials are attached and which actor supplies them.
	Auth *ProtocolAuth `yaml:"auth,omitempty"`
}

// ProtocolAuth declares where a credential goes and which actor supplies it.
type ProtocolAuth struct {
	// Strategy is where the credential is placed: query | header | subprotocol.
	Strategy string `yaml:"strategy"`
	// Param is the query-param name, header name, or subprotocol name.
	Param string `yaml:"param"`
	// CredentialRef names an entry in actors[] whose resolved raw token is used.
	CredentialRef string `yaml:"credential_ref"`
}
```

In `internal/project/schema.go`, add a field to the `Service` struct (after `BodyTemplate` at line 23):
```go
	// Protocol optionally declares this service's WebSocket protocol facts.
	// When nil, the WS executor falls back to M0 behavior.
	Protocol *Protocol `yaml:"protocol,omitempty"`
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/project/ -run TestProtocol -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/project/protocol_schema.go internal/project/schema.go internal/project/protocol_schema_test.go
git commit -m "feat(project): add Protocol declaration on Service"
```

---

## Task 2: extractTypePath resolver

Generalize `extractByDotPath` to take raw message bytes and return a `(string, bool)` match, with a non-string leaf counting as no-match (preserves M0 `messageType` semantics on the fallback path).

**Files:**
- Create: `internal/head/agent/ws_protocol.go`
- Test: `internal/head/agent/ws_protocol_test.go` (create)

**Interfaces:**
- Consumes: nothing.
- Produces: `extractTypePath(data []byte, path string) (string, bool)` — used by `doReceive` (Task 8).

- [ ] **Step 1: Write the failing test**

`internal/head/agent/ws_protocol_test.go`:
```go
package agent

import "testing"

func TestExtractTypePath(t *testing.T) {
	cases := []struct {
		name string
		data string
		path string
		want string
		ok   bool
	}{
		{name: "top-level type", data: `{"type":"permission:response"}`, path: "type", want: "permission:response", ok: true},
		{name: "default empty path = top-level type", data: `{"type":"x"}`, path: "", want: "x", ok: true},
		{name: "nested path", data: `{"data":{"event":"ping"}}`, path: "data.event", want: "ping", ok: true},
		{name: "missing path", data: `{"type":"x"}`, path: "data.event", want: "", ok: false},
		{name: "non-string leaf no match", data: `{"type":123}`, path: "type", want: "", ok: false},
		{name: "non-json no match", data: `not json`, path: "type", want: "", ok: false},
		{name: "json array no match", data: `[1,2,3]`, path: "type", want: "", ok: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := extractTypePath([]byte(tc.data), tc.path)
			if got != tc.want || ok != tc.ok {
				t.Fatalf("got (%q,%v) want (%q,%v)", got, ok, tc.want, tc.ok)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/head/agent/ -run TestExtractTypePath -v`
Expected: FAIL — `undefined: extractTypePath`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/head/agent/ws_protocol.go`:
```go
package agent

import (
	"encoding/json"
	"strings"
)

// extractTypePath returns the routing key at the dotted path within a JSON
// message. An empty path means top-level "type" (M0 behavior). Returns
// ("", false) if the message is not a JSON object, the path is absent, or the
// leaf is not a string — so the M0 fallback path reproduces messageType
// semantics exactly (a non-string type field does not match).
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

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/head/agent/ -run TestExtractTypePath -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/head/agent/ws_protocol.go internal/head/agent/ws_protocol_test.go
git commit -m "feat(ws): add dotted-path type extractor"
```

---

## Task 3: Protocol validation (Phase 6)

**Files:**
- Create: `internal/project/validate_protocol.go`, `internal/project/validate_protocol_test.go`
- Modify: `internal/project/validate.go:39` (add Phase 6)

**Interfaces:**
- Consumes: `project.Protocol` (Task 1), `cfg.Actors` (to check `credential_ref`).
- Produces: `ValidateProtocol(*Protocol) error` (exported, for reuse/tests) and a Phase 6 `validateProtocol(cfg, ve)` wired into `Config.Validate()`.

- [ ] **Step 1: Write the failing test**

`internal/project/validate_protocol_test.go`:
```go
package project

import "testing"

func TestValidateProtocol(t *testing.T) {
	actor := Actor{Name: "web"}
	cases := []struct {
		name    string
		p       *Protocol
		actors  []Actor
		wantErr string // non-empty substring expected when invalid
	}{
		{name: "nil ok", p: nil, actors: nil, wantErr: ""},
		{name: "empty defaults ok", p: &Protocol{}, actors: nil, wantErr: ""},
		{name: "json framing ok", p: &Protocol{Framing: "json"}, actors: nil, wantErr: ""},
		{name: "text framing rejected", p: &Protocol{Framing: "text"}, actors: nil, wantErr: "framing"},
		{name: "binary framing rejected", p: &Protocol{Framing: "binary"}, actors: nil, wantErr: "framing"},
		{name: "bad strategy", p: &Protocol{Auth: &ProtocolAuth{Strategy: "cookie", Param: "t"}}, actors: nil, wantErr: "strategy"},
		{name: "strategy without param", p: &Protocol{Auth: &ProtocolAuth{Strategy: "query"}}, actors: nil, wantErr: "param"},
		{name: "credential_ref missing actor", p: &Protocol{Auth: &ProtocolAuth{Strategy: "query", Param: "token", CredentialRef: "ghost"}}, actors: []Actor{actor}, wantErr: "credential_ref"},
		{name: "credential_ref ok", p: &Protocol{Auth: &ProtocolAuth{Strategy: "query", Param: "token", CredentialRef: "web"}}, actors: []Actor{actor}, wantErr: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateProtocol(tc.p, tc.actors)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected err: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("want err containing %q, got nil", tc.wantErr)
			}
		})
	}
}

func TestValidateIntegrationRejectsBadProtocol(t *testing.T) {
	cfg := &Config{
		Services: []Service{{Name: "rt", URL: "http://x", Protocol: &Protocol{Framing: "text"}}},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("want validation error for text framing")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/project/ -run 'TestValidateProtocol|TestValidateIntegrationRejectsBadProtocol' -v`
Expected: FAIL — `undefined: ValidateProtocol`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/project/validate_protocol.go`:
```go
package project

import "fmt"

// validProtocolFraming is the set of framing values M1 supports. M2 may add
// "text"/"binary".
var validProtocolFraming = map[string]bool{"": true, "json": true}

// validProtocolAuthStrategy is the set of auth placement strategies.
var validProtocolAuthStrategy = map[string]bool{"query": true, "header": true, "subprotocol": true}

// ValidateProtocol checks a Protocol declaration for config-time errors. A nil
// protocol is valid (means M0 fallback). actors is the config's actor list, used
// to confirm credential_ref names a real actor. Returns nil if valid.
func ValidateProtocol(p *Protocol, actors []Actor) error {
	if p == nil {
		return nil
	}
	if !validProtocolFraming[p.Framing] {
		return fmt.Errorf("protocol.framing %q is not supported in M1 (use \"json\")", p.Framing)
	}
	if p.Auth != nil {
		if !validProtocolAuthStrategy[p.Auth.Strategy] {
			return fmt.Errorf("protocol.auth.strategy %q must be query, header, or subprotocol", p.Auth.Strategy)
		}
		if p.Auth.Param == "" {
			return fmt.Errorf("protocol.auth.param is required when strategy is set")
		}
		if p.Auth.CredentialRef != "" {
			found := false
			for _, a := range actors {
				if a.Name == p.Auth.CredentialRef {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("protocol.auth.credential_ref %q does not match any actor", p.Auth.CredentialRef)
			}
		}
	}
	return nil
}

// validateProtocol is Phase 6 of Config.Validate: validates each service's
// optional Protocol block, collecting all errors.
func validateProtocol(cfg *Config, ve *ValidationError) {
	for i, svc := range cfg.Services {
		if svc.Protocol == nil {
			continue
		}
		if err := ValidateProtocol(svc.Protocol, cfg.Actors); err != nil {
			ve.add(fmt.Sprintf("services[%d].%s", i, err.Error()))
		}
	}
}
```

In `internal/project/validate.go`, add Phase 6 inside `Validate()` after the Phase 5 call (after line 39 `validateSettings(cfg, &ve)`):
```go
	// Phase 6: Validate WS protocol declarations
	validateProtocol(cfg, &ve)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/project/ -v`
Expected: PASS (new tests + existing validate tests still green).

- [ ] **Step 5: Commit**

```bash
git add internal/project/validate_protocol.go internal/project/validate_protocol_test.go internal/project/validate.go
git commit -m "feat(project): validate WS protocol declarations"
```

---

## Task 4: WSConnectAction.credential_ref field

**Files:**
- Modify: `internal/types/actions_http.go:172-192` (WSConnectAction)
- Test: `internal/types/ws_actions_test.go` (append) — if absent, create.

**Interfaces:**
- Consumes: nothing.
- Produces: `WSConnectAction.CredentialRef string` (json `credential_ref,omitempty`) — overrides the service default per-connection (Task 9).

- [ ] **Step 1: Write the failing test**

Append to `internal/types/ws_actions_test.go` (create the file if it does not exist with `package types` and the imports from the M0 plan):
```go
func TestWSConnectActionCredentialRefRoundTrip(t *testing.T) {
	envelope, err := MarshalAction(&WSConnectAction{
		URL:           "ws://x",
		ConnectionID:  "c1",
		CredentialRef: "bridge-actor",
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got, err := UnmarshalAction(envelope)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	c, ok := got.(WSConnectAction)
	if !ok {
		t.Fatalf("type %T, want WSConnectAction", got)
	}
	if c.CredentialRef != "bridge-actor" {
		t.Fatalf("credential_ref round-trip lost: %+v", c)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/types/ -run TestWSConnectActionCredentialRefRoundTrip -v`
Expected: FAIL — `unknown field credential_ref` / `c.CredentialRef undefined`.

- [ ] **Step 3: Write minimal implementation**

In `internal/types/actions_http.go`, add the field to `WSConnectAction` (after `ConnectionID`):
```go
	// CredentialRef optionally names the actor whose resolved raw token the
	// executor injects for this connection (overrides the service protocol's
	// auth.credential_ref). Only meaningful when the service declares a protocol.
	CredentialRef string `json:"credential_ref,omitempty"`
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/types/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/types/actions_http.go internal/types/ws_actions_test.go
git commit -m "feat(types): add credential_ref to WSConnectAction"
```

---

## Task 5: Raw-token resolution + caching

`ResolveAuthHeader` extracts the raw token at `authflow.go:138` then formats it via `InjectAs`; today only the formatted value is cached in `Credentials.Headers`. WS `query`/`header`/`subprotocol` auth needs the **raw** token. Extend `ResolveAuthHeader` to also return the raw token, cache it on `CredentialRef.RawToken` at session setup.

**Files:**
- Modify: `internal/head/agent/authflow.go:70` (signature + return raw token)
- Modify: `internal/project/schema.go:34-38` (CredentialRef: add RawToken)
- Modify: `internal/session/auth_setup.go:44-56` (capture + cache raw token)
- Modify: `internal/head/agent/authflow_test.go` (5 call sites → 4-return)
- Test: `internal/head/agent/authflow_test.go` (append a raw-token assertion)

**Interfaces:**
- Consumes: nothing new.
- Produces: `ResolveAuthHeader(...) (name, value, rawToken string, err error)`, `CredentialRef.RawToken string` (runtime-only, `yaml:"-"`), and a cached raw token read by `BuildWSProtocolIndex` (Task 8) and `doConnect` (Task 9).

- [ ] **Step 1: Write the failing test**

Append to `internal/head/agent/authflow_test.go`:
```go
func TestResolveAuthHeaderReturnsRawToken(t *testing.T) {
	srv := newLoginServer(t, 200, `{"token":"JWT-RAW"}`, nil)
	defer srv.Close()
	actor := project.Actor{Auth: &project.AuthFlow{
		Login: project.AuthLogin{Method: "POST", Path: "/login"},
		TokenFrom: "token", InjectAs: "Authorization: Bearer {token}",
	}}
	_, _, raw, err := ResolveAuthHeader(context.Background(), srv.URL, actor)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if raw != "JWT-RAW" {
		t.Fatalf("raw token = %q, want JWT-RAW", raw)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/head/agent/ -run TestResolveAuthHeaderReturnsRawToken -v`
Expected: FAIL — `assignment mismatch: 4 variables but ResolveAuthHeader returns 3 values`.

- [ ] **Step 3: Write minimal implementation**

In `internal/head/agent/authflow.go`, change the signature and the return of `ResolveAuthHeader` (line 70 and the final return at line 149). Signature:
```go
func ResolveAuthHeader(ctx context.Context, svcURL string, actor project.Actor) (name, value, rawToken string, err error) {
```
At the point the token is extracted (after line 141 `token, err := extractByDotPath(...)`, before the InjectAs interpolation), capture it. The existing `return "", "", fmt.Errorf(...)` error returns become `return "", "", "", fmt.Errorf(...)`. The final success return (line 149) becomes:
```go
	return hName, hValue, token, nil
```
(The local `token` from `extractByDotPath` is the raw token; return it as the third value.)

In `internal/project/schema.go`, add a runtime-only field to `CredentialRef` (after `Headers`):
```go
	// RawToken is the unformatted token cached at session setup (populated by
	// auth setup when the actor has an Auth flow). Runtime-only; not loaded
	// from YAML. Used by WS query/header/subprotocol auth injection.
	RawToken string `yaml:"-" json:"-"`
```

In `internal/session/auth_setup.go`, update the call and cache both values. Replace line 44 (`name, value, err := agent.ResolveAuthHeader(ctx, svcURL, *a)`) with:
```go
		name, value, rawToken, err := agent.ResolveAuthHeader(ctx, svcURL, *a)
```
And after `a.Credentials.Headers[name] = value` (line 56), add:
```go
		a.Credentials.RawToken = rawToken
```

In `internal/head/agent/authflow_test.go`, update the five existing 3-return call sites to 4-return:
- L117 `name, value, err :=` → `name, value, _, err :=`
- L135 `name, value, err :=` → `name, value, _, err :=`
- L162 `_, _, err :=` → `_, _, _, err :=`
- L183 `_, _, err :=` → `_, _, _, err :=`
- L197 `_, _, err :=` → `_, _, _, err :=`

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/head/agent/ -run TestResolveAuthHeader -v && go test ./internal/session/ -run TestResolveActorAuth -v 2>/dev/null || go build ./internal/session/`
Expected: authflow tests PASS; session builds. (If no `TestResolveActorAuth` exists, `go build ./internal/session/` confirms the call-site change compiles.)

- [ ] **Step 5: Commit**

```bash
git add internal/head/agent/authflow.go internal/head/agent/authflow_test.go internal/project/schema.go internal/session/auth_setup.go
git commit -m "feat(auth): expose and cache raw token alongside formatted header"
```

---

## Task 6: Per-case connection_id namespacing

Complete M0 D3: namespace connection-table keys by caseID so parallel cases passing the same LLM-supplied `connection_id` do not collide.

**Files:**
- Modify: `internal/head/agent/websocket.go` (`store`/`lookup`/`doDisconnect` namespaced keys + `caseIDKey`)
- Modify: `internal/head/agent/execute_phases.go:38-42` (inject `tc.ID`)
- Test: `internal/head/agent/websocket_test.go` (append)

**Interfaces:**
- Consumes: nothing.
- Produces: an unexported `caseIDKey` ctx key; `doConnect` reads `ctx.Value(caseIDKey)`; all table keys are `<caseID>:<connectionID>`.

- [ ] **Step 1: Write the failing test**

Append to `internal/head/agent/websocket_test.go`:
```go
func TestConnectionNamespacingByCaseID(t *testing.T) {
	connects := 0
	url := newWSTestServer(t, func(conn *websocket.Conn) {
		connects++
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, _, _ = conn.Read(ctx)
	})
	ex := newWSExecutor()

	// Two different case contexts, same LLM-supplied connection_id "c1".
	ctxA := context.WithValue(context.Background(), caseIDKey{}, "case-A")
	ctxB := context.WithValue(context.Background(), caseIDKey{}, "case-B")

	if !ex.Execute(ctxA, WSConnectAction{URL: url, ConnectionID: "c1"}).Success() {
		t.Fatal("connect A failed")
	}
	if !ex.Execute(ctxB, WSConnectAction{URL: url, ConnectionID: "c1"}).Success() {
		t.Fatal("connect B failed")
	}
	if connects != 2 {
		t.Fatalf("server saw %d connects, want 2 (namespacing failed)", connects)
	}
	// Disconnect in case A must not touch case B's connection.
	ex.Execute(ctxA, WSDisconnectAction{ConnectionID: "c1"})
	if !ex.Execute(ctxB, WSSendAction{ConnectionID: "c1", Message: `{"type":"ping"}`}).Success() {
		t.Fatal("case B connection lost after case A disconnect (namespacing failed)")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/head/agent/ -run TestConnectionNamespacingByCaseID -v`
Expected: FAIL — `connects == 1` (second connect reused case A's entry) and/or `undefined: caseIDKey`.

- [ ] **Step 3: Write minimal implementation**

In `internal/head/agent/websocket.go`, add the ctx key type near the top (after the imports):
```go
// caseIDKey is the per-case identifier carried on the per-case context, used to
// namespace connection-table keys so parallel cases cannot collide on a shared
// LLM-supplied connection_id.
type caseIDKey struct{}
```
Add a helper that reads + namespaces:
```go
// caseNamespace reads the caseID from ctx (defaulting to "_default" when absent,
// e.g. in unit tests) and returns the namespaced connection key.
func caseNamespace(ctx context.Context, connectionID string) string {
	v, _ := ctx.Value(caseIDKey{}).(string)
	if v == "" {
		v = "_default"
	}
	return v + ":" + connectionID
}
```
(`context` is already imported.)

Namespace at the call sites — `store` and `lookup` keep their existing signatures; each caller computes the namespaced key first. (Task 7 later changes `lookup`'s return type but not its key argument; this keeps the two tasks' changes independent.)
- `doConnect`: `key := caseNamespace(ctx, id)` (where `id` is the resolved connection id); `e.store(key, conn, ctx)`; return using the original `id` for any user-facing value.
- `doSend`: `conn, _, ok := e.lookup(caseNamespace(ctx, a.ConnectionID))`.
- `doReceive`: `conn, connCtx, ok := e.lookup(caseNamespace(ctx, a.ConnectionID))`.
- `doDisconnect`: `key := caseNamespace(ctx, a.ConnectionID)`; `e.mu.Lock()`; `entry, ok := e.conns[key]`; if ok close+`delete(e.conns, key)`; `e.mu.Unlock()`.
- The cleanup goroutine in `store` closes over its `id` argument — since `doConnect` passes the namespaced `key` as that `id`, the goroutine deletes the right entry. `store` internals are unchanged.

In `internal/head/agent/execute_phases.go`, inject `tc.ID` when deriving the per-case ctx. After line 40 (`se.ctx, cancel = context.WithTimeout(se.ctx, r.config.PerCaseTimeout)` inside the `if`), and ALSO for the no-timeout path so every case gets namespaced, replace the block at lines 37-42 with:
```go
	// Apply per-case timeout.
	if r.config.PerCaseTimeout > 0 {
		var cancel context.CancelFunc
		se.ctx, cancel = context.WithTimeout(se.ctx, r.config.PerCaseTimeout)
		defer cancel()
	}
	// Carry the case identifier for per-case connection namespacing (WS executor).
	se.ctx = context.WithValue(se.ctx, caseIDKey{}, tc.ID)
```
(`caseIDKey` is defined in package `agent` in `websocket.go`; `execute_phases.go` is in the same package.)

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/head/agent/ -run 'TestConnectionNamespacing|TestWSConnect|TestWSSend|TestWSReceive' -v`
Expected: PASS (new test green; existing executor tests unaffected — they use `context.Background()` → `_default` namespace).

- [ ] **Step 5: Commit**

```bash
git add internal/head/agent/websocket.go internal/head/agent/execute_phases.go internal/head/agent/websocket_test.go
git commit -m "feat(ws): namespace connections by caseID via ctx"
```

---

## Task 7: doReceive read serialization

`coder/websocket` forbids concurrent `Read` on one connection. Add a per-connection read mutex.

**Files:**
- Modify: `internal/head/agent/websocket.go` (`wsEntry.readMu`, `doReceive` guard, `lookup` returns `*wsEntry`)
- Test: `internal/head/agent/websocket_test.go` (append)

**Interfaces:**
- Consumes: Task 6's namespacing.
- Produces: a per-entry `readMu` taken around `conn.Read` in `doReceive`.

- [ ] **Step 1: Write the failing test**

Append to `internal/head/agent/websocket_test.go`:
```go
func TestReceiveSerializedPerConnection(t *testing.T) {
	url := newWSTestServer(t, func(conn *websocket.Conn) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		for i := 0; i < 20; i++ {
			_ = conn.Write(ctx, websocket.MessageText, []byte(`{"type":"ping"}`))
		}
		_, _, _ = conn.Read(ctx)
	})
	ex := newWSExecutor()
	ctx := context.Background()
	ex.Execute(ctx, WSConnectAction{URL: url, ConnectionID: "c1"})

	// Two concurrent receives on the SAME connection must serialize, not race.
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			r := ex.Execute(ctx, WSReceiveAction{ConnectionID: "c1", Type: "ping", Timeout: 2})
			if !r.Success() {
				errs <- fmt.Errorf("receive failed: %v", r)
				return
			}
			errs <- nil
		}()
	}
	for i := 0; i < 2; i++ {
		select {
		case e := <-errs:
			if e != nil {
				t.Fatal(e)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("receive timed out")
		}
	}
}
```
(Add `"fmt"` to the test file imports if not present.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/head/agent/ -run TestReceiveSerializedPerConnection -race -v`
Expected: FAIL or DATA RACE — without the guard, concurrent `conn.Read` races / errors.

- [ ] **Step 3: Write minimal implementation**

In `internal/head/agent/websocket.go`, add `readMu` to `wsEntry`:
```go
type wsEntry struct {
	conn   *websocket.Conn
	ctx    context.Context
	readMu sync.Mutex
	protocol *types.WSProtocol // set in Task 8; nil here is fine until then
}
```
Wait — `protocol` is added in Task 8. For Task 7, add only `readMu sync.Mutex` to the existing `wsEntry` (keep `conn`, `ctx`). So:
```go
type wsEntry struct {
	conn   *websocket.Conn
	ctx    context.Context
	readMu sync.Mutex
}
```
Guard the read loop in `doReceive`. Change `lookup` to return the `*wsEntry` (so callers can take its mutex), keeping its single namespaced-key argument:
```go
func (e *WebSocketExecutor) lookup(key string) (*wsEntry, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	entry, ok := e.conns[key]
	return entry, ok
}
```
In `doReceive`, after lookup, take the entry's `readMu` around the whole scan loop:
```go
	entry, ok := e.lookup(caseNamespace(ctx, a.ConnectionID))
	if !ok {
		return types.WSResult{OK: false, Err: fmt.Sprintf("unknown connection_id: %s", a.ConnectionID), Latency: time.Since(start)}
	}
	entry.readMu.Lock()
	defer entry.readMu.Unlock()
	conn, connCtx := entry.conn, entry.ctx
	// ... existing read loop uses conn/connCtx unchanged ...
```
Update `doSend` to the new return shape: `entry, ok := e.lookup(caseNamespace(ctx, a.ConnectionID)); conn := entry.conn`. `doDisconnect` keeps its direct map mutation (it already locks `e.mu` and computes the namespaced key in Task 6).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/head/agent/ -run 'TestReceiveSerialized|TestWSConnect|TestWSSend|TestWSReceive' -race -v`
Expected: PASS, `-race` clean.

- [ ] **Step 5: Commit**

```bash
git add internal/head/agent/websocket.go internal/head/agent/websocket_test.go
git commit -m "feat(ws): serialize concurrent receives per connection"
```

---

## Task 8: WSProtocolIndex threading + doConnect stash + doReceive type_path

Build the `WSProtocolIndex{ByHost, ActorTokens}` from config, thread it into the executor through the existing plugin chain, stash the resolved protocol on `wsEntry` at connect, and have `doReceive` match by the declared `type_path`.

**Files:**
- Modify: `internal/head/agent/ws_protocol.go` (add `WSProtocolIndex`, `BuildWSProtocolIndex`)
- Modify: `internal/head/agent/websocket.go` (`NewWebSocketExecutor` sig, `wsEntry.protocol`, `resolveProtocol`, `doConnect` stash, `doReceive` type_path)
- Modify: `internal/head/agent/plugin_helpers.go` (thread `*WSProtocolIndex`)
- Modify: `internal/head/agent/multi.go:93,115` (thread through `BuildMultiExecutor`/`BuiltinPluginsWithSandbox`)
- Modify: `internal/session/run_phases_agent.go:25`, `internal/session/resume_phases_run.go:25` (build + pass index)
- Test: `internal/head/agent/websocket_test.go` (append), `internal/head/agent/ws_protocol_test.go` (append)

**Interfaces:**
- Consumes: `project.Protocol` (Task 1), `extractTypePath` (Task 2).
- Produces: `NewWebSocketExecutor(logger, idx *WSProtocolIndex)`, `WSProtocolIndex{ByHost, ActorTokens}`, `BuildWSProtocolIndex(cfg)`, protocol stashed on `wsEntry` for `doReceive`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/head/agent/ws_protocol_test.go`:
```go
import "github.com/binoctal/cerberus/internal/project"

func TestBuildWSProtocolIndex(t *testing.T) {
	cfg := &project.Config{
		Services: []project.Service{{
			Name: "rt", URL: "http://localhost:8787",
			Protocol: &project.Protocol{TypePath: "data.event"},
		}},
		Actors: []project.Actor{{Name: "web", Credentials: project.CredentialRef{RawToken: "JWT"}}},
	}
	idx := BuildWSProtocolIndex(cfg)
	if idx == nil {
		t.Fatal("index is nil")
	}
	if p, ok := idx.ByHost["localhost:8787"]; !ok || p.TypePath != "data.event" {
		t.Fatalf("ByHost = %+v", idx.ByHost)
	}
	if idx.ActorTokens["web"] != "JWT" {
		t.Fatalf("ActorTokens = %+v", idx.ActorTokens)
	}
}

func TestBuildWSProtocolIndexNilWhenNoProtocols(t *testing.T) {
	cfg := &project.Config{Services: []project.Service{{Name: "x", URL: "http://x"}}}
	if idx := BuildWSProtocolIndex(cfg); idx != nil {
		t.Fatalf("want nil index when no protocols, got %+v", idx)
	}
}
```

Append to `internal/head/agent/websocket_test.go`:
```go
func TestReceiveMatchesByTypePath(t *testing.T) {
	url := newWSTestServer(t, func(conn *websocket.Conn) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = conn.Write(ctx, websocket.MessageText, []byte(`{"data":{"event":"go"}}`))
		_, _, _ = conn.Read(ctx)
	})
	ex := NewWebSocketExecutor(zap.NewNop(), protocolIndexForURL(t, url, &project.Protocol{TypePath: "data.event"}))
	ctx := context.Background()
	ex.Execute(ctx, WSConnectAction{URL: url, ConnectionID: "c1"})
	res := ex.Execute(ctx, WSReceiveAction{ConnectionID: "c1", Type: "go", Timeout: 2})
	ws, ok := res.(types.WSResult)
	if !ok || !ws.OK {
		t.Fatalf("receive failed: %+v", res)
	}
	if !strings.Contains(ws.MatchedMessage, "go") {
		t.Fatalf("did not match via type_path: %s", ws.MatchedMessage)
	}
}
```
And add a tiny helper at the top of the test file to build a single-host index (used above and in Task 9):
```go
// protocolIndexForURL builds a WSProtocolIndex mapping the host of url to p.
func protocolIndexForURL(t *testing.T, rawURL string, p *project.Protocol) *WSProtocolIndex {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return &WSProtocolIndex{ByHost: map[string]*project.Protocol{u.Host: p}}
}
```
(Add `"net/url"`, `"github.com/binoctal/cerberus/internal/project"`, `"github.com/binoctal/cerberus/internal/types"` to imports as needed.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/head/agent/ -run 'TestBuildWSProtocolIndex|TestReceiveMatchesByTypePath' -v`
Expected: FAIL — `undefined: WSProtocolIndex` / `BuildWSProtocolIndex` / `NewWebSocketExecutor` signature mismatch.

- [ ] **Step 3: Write minimal implementation**

In `internal/head/agent/ws_protocol.go`, add the index types and builder (append after `extractTypePath`):
```go
import (
	"encoding/json"
	"net/url"
	"strings"

	"github.com/binoctal/cerberus/internal/project"
)

// WSProtocolIndex gives the WS executor the per-host protocol declaration and
// the resolved raw credential tokens for actors referenced by those protocols.
// A nil index means "no service declares a protocol" → M0 behavior everywhere.
type WSProtocolIndex struct {
	ByHost      map[string]*project.Protocol // host (url.Host) -> protocol
	ActorTokens map[string]string            // actor name -> cached raw token
}

// BuildWSProtocolIndex builds the index from config. Returns nil when no service
// declares a protocol (so the executor can short-circuit to M0 behavior).
func BuildWSProtocolIndex(cfg *project.Config) *WSProtocolIndex {
	var idx *WSProtocolIndex
	for _, svc := range cfg.Services {
		if svc.Protocol == nil {
			continue
		}
		u, err := url.Parse(svc.URL)
		if err != nil {
			continue
		}
		if idx == nil {
			idx = &WSProtocolIndex{
				ByHost:      make(map[string]*project.Protocol),
				ActorTokens: make(map[string]string),
			}
		}
		idx.ByHost[u.Host] = svc.Protocol
	}
	if idx == nil {
		return nil
	}
	for _, a := range cfg.Actors {
		if a.Credentials.RawToken != "" {
			idx.ActorTokens[a.Name] = a.Credentials.RawToken
		}
	}
	return idx
}
```
(Remove the duplicate `import` block — merge `net/url` and `project` into the existing imports at the top of `ws_protocol.go`; keep `encoding/json` and `strings` already there.)

In `internal/head/agent/websocket.go`:
- Add field to `wsEntry`: `protocol *project.Protocol` (and `readMu` from Task 7).
- Change the constructor and add a field + resolver:
```go
type WebSocketExecutor struct {
	logger *zap.Logger
	mu     sync.RWMutex
	conns  map[string]*wsEntry
	seq    uint64
	idx    *WSProtocolIndex
}

func NewWebSocketExecutor(logger *zap.Logger, idx *WSProtocolIndex) *WebSocketExecutor {
	return &WebSocketExecutor{logger: logger, conns: make(map[string]*wsEntry), idx: idx}
}

// resolveProtocol returns the declared protocol for a dial url's host, or nil
// when none is declared (M0 behavior).
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
```
(Add `"net/url"` and `"github.com/binoctal/cerberus/internal/project"` to imports.)
- In `doConnect`, resolve the protocol once at the top — before building `opts` / dialing — so Task 9 can use it for auth injection; stash it on the entry after dialing:
```go
	proto := e.resolveProtocol(a.URL) // first line of doConnect
	// ... existing opts build + websocket.Dial(ctx, wsURL(a.URL), opts) ...
	e.store(caseNamespace(ctx, id), conn, ctx, proto)
	return types.WSResult{OK: true, URL: a.URL, Latency: time.Since(start)}
```
- Change `store` to accept and stash the protocol:
```go
func (e *WebSocketExecutor) store(key string, conn *websocket.Conn, ctx context.Context, proto *project.Protocol) {
	e.mu.Lock()
	e.conns[key] = &wsEntry{conn: conn, ctx: ctx, protocol: proto}
	e.mu.Unlock()
	go func() {
		<-ctx.Done()
		_ = conn.Close(websocket.StatusNormalClosure, "ctx done")
		e.mu.Lock()
		delete(e.conns, key)
		e.mu.Unlock()
	}()
}
```
- In `doReceive`, replace the `messageType(data)` match with `extractTypePath`:
```go
		path := "type"
		if entry.protocol != nil && entry.protocol.TypePath != "" {
			path = entry.protocol.TypePath
		}
		// ... inside the loop:
		if t, ok := extractTypePath(data, path); ok && t == a.Type {
			return types.WSResult{OK: true, MatchedMessage: string(data), SeenMessages: seen, Latency: time.Since(start)}
		}
```
(Remove the now-unused `messageType` helper if nothing else references it — `grep messageType`.)

Update the test helper `newWSExecutor()` (websocket_test.go:42) to the new signature:
```go
func newWSExecutor() *WebSocketExecutor {
	return NewWebSocketExecutor(zap.NewNop(), nil)
}
```

In `internal/head/agent/plugin_helpers.go`, thread `*WSProtocolIndex`:
```go
func BuiltinExecutorPlugins(projectDir string, serviceHeaders map[string]map[string]string, wsIdx *WSProtocolIndex, logger *zap.Logger) []ExecutorPlugin {
	return []ExecutorPlugin{
		&httpPlugin{executor: NewHTTPExecutorWithServiceHeaders(logger, serviceHeaders)},
		&waitPlugin{executor: NewWaitExecutor()},
	}
}

func BuiltinPluginsWithSandbox(projectDir string, serviceHeaders map[string]map[string]string, wsIdx *WSProtocolIndex, sb sandbox.Sandbox, gate escalation.Gate, logger *zap.Logger) []ExecutorPlugin {
	plugins := BuiltinExecutorPlugins(projectDir, serviceHeaders, wsIdx, logger)
	plugins = append(plugins,
		// ... unchanged ...
		&wsPlugin{executor: NewWebSocketExecutor(logger, wsIdx)},
	)
	// ... browser block unchanged ...
	return plugins
}
```

In `internal/head/agent/multi.go`, thread through `BuildMultiExecutor` (line 93) and its `BuiltinPluginsWithSandbox` call (line 115):
```go
func BuildMultiExecutor(projectDir string, serviceHeaders map[string]map[string]string, wsIdx *WSProtocolIndex, gate escalation.Gate, logger *zap.Logger) *MultiExecutor {
	// ... unchanged body ...
	for _, plugin := range BuiltinPluginsWithSandbox(projectDir, serviceHeaders, wsIdx, sb, gate, logger) {
	// ...
```

In `internal/session/run_phases_agent.go:25` and `internal/session/resume_phases_run.go:25`, build and pass the index. Replace:
```go
	multiExec := agent.BuildMultiExecutor(projectDir, agent.ServiceHeadersMap(rp.session.Config.Services), rp.session.Gate, rp.session.Logger)
```
with:
```go
	multiExec := agent.BuildMultiExecutor(projectDir, agent.ServiceHeadersMap(rp.session.Config.Services), agent.BuildWSProtocolIndex(rp.session.Config), rp.session.Gate, rp.session.Logger)
```
(Both files, identical change. `Config` is `*project.Config`; `BuildWSProtocolIndex` takes `*project.Config`.)

- [ ] **Step 4: Run test to verify it passes**

Run: `go build ./... && go test ./internal/head/agent/ -run 'TestBuildWSProtocolIndex|TestReceiveMatchesByTypePath|TestWSConnect|TestWSSend|TestWSReceive|TestConnectionNamespacing|TestReceiveSerialized' -v`
Expected: build clean; all listed tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/head/agent/ws_protocol.go internal/head/agent/ws_protocol_test.go internal/head/agent/websocket.go internal/head/agent/websocket_test.go internal/head/agent/plugin_helpers.go internal/head/agent/multi.go internal/session/run_phases_agent.go internal/session/resume_phases_run.go
git commit -m "feat(ws): thread protocol index to executor; match by declared type_path"
```

---

## Task 9: doConnect strip-then-inject auth (executor-authoritative)

When the resolved protocol declares `auth`, `doConnect` resolves the actor's raw token, **strips** any LLM-supplied value at `param`, then **injects** the resolved value (`url.QueryEscape` for query). Stores the pre-injection url in the result. Fails loudly if declared auth is unresolvable.

**Files:**
- Modify: `internal/head/agent/websocket.go` (`doConnect`)
- Test: `internal/head/agent/websocket_test.go` (append)

**Interfaces:**
- Consumes: `entry.protocol.Auth` (Task 8), `idx.ActorTokens` (Task 8), `WSConnectAction.CredentialRef` (Task 4).
- Produces: declared auth auto-injected at connect; M0 behavior when no protocol/auth.

- [ ] **Step 1: Write the failing tests**

Add a capturing test-server helper to `internal/head/agent/websocket_test.go` (records each upgrade request's raw query so the tests can observe exactly what the executor dialed):
```go
// newWSTestServerCapture starts a WS server that records each upgrade request's
// raw query string; returns the ws url and a getter for the most recent query.
func newWSTestServerCapture(t *testing.T) (string, func() string) {
	t.Helper()
	var mu sync.Mutex
	var queries []string
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		queries = append(queries, r.URL.RawQuery)
		mu.Unlock()
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")
		_, _, _ = conn.Read(r.Context())
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	return wsURL, func() string {
		mu.Lock()
		defer mu.Unlock()
		if len(queries) == 0 {
			return ""
		}
		return queries[len(queries)-1]
	}
}

func hostOf(t *testing.T, wsURL string) string {
	t.Helper()
	u, err := url.Parse(wsURL)
	if err != nil {
		t.Fatal(err)
	}
	return u.Host
}
```

Append the failing tests:
```go
func TestConnectInjectsQueryToken(t *testing.T) {
	wsURL, latestQuery := newWSTestServerCapture(t)
	p := &project.Protocol{Auth: &project.ProtocolAuth{Strategy: "query", Param: "token", CredentialRef: "web"}}
	idx := &WSProtocolIndex{
		ByHost:      map[string]*project.Protocol{hostOf(t, wsURL): p},
		ActorTokens: map[string]string{"web": "JWT-VALUE"},
	}
	ex := NewWebSocketExecutor(zap.NewNop(), idx)
	res := ex.Execute(context.Background(), WSConnectAction{URL: wsURL + "?type=web", ConnectionID: "c1", CredentialRef: "web"})
	ws, ok := res.(types.WSResult)
	if !ok || !ws.Success() {
		t.Fatalf("connect failed: %+v", res)
	}
	// The DIALED url carries the token (observable via the captured upgrade query).
	if !strings.Contains(latestQuery(), "token=JWT-VALUE") {
		t.Fatalf("query missing injected token: %s", latestQuery())
	}
	// The RESULT url is the pre-injection url (no secret).
	if strings.Contains(ws.URL, "JWT-VALUE") {
		t.Fatalf("result url leaks token: %s", ws.URL)
	}
}

func TestConnectStripsLLMSuppliedToken(t *testing.T) {
	wsURL, latestQuery := newWSTestServerCapture(t)
	p := &project.Protocol{Auth: &project.ProtocolAuth{Strategy: "query", Param: "token", CredentialRef: "web"}}
	idx := &WSProtocolIndex{
		ByHost:      map[string]*project.Protocol{hostOf(t, wsURL): p},
		ActorTokens: map[string]string{"web": "REAL"},
	}
	ex := NewWebSocketExecutor(zap.NewNop(), idx)
	ex.Execute(context.Background(), WSConnectAction{URL: wsURL + "?type=web&token=LLM-WRONG", ConnectionID: "c1", CredentialRef: "web"})
	q := latestQuery()
	if strings.Contains(q, "LLM-WRONG") {
		t.Fatalf("LLM-supplied token not stripped: %s", q)
	}
	if !strings.Contains(q, "token=REAL") {
		t.Fatalf("resolved token not injected: %s", q)
	}
}

func TestConnectFailsWhenAuthUnresolvable(t *testing.T) {
	wsURL, _ := newWSTestServerCapture(t)
	p := &project.Protocol{Auth: &project.ProtocolAuth{Strategy: "query", Param: "token", CredentialRef: "ghost"}}
	idx := &WSProtocolIndex{ByHost: map[string]*project.Protocol{hostOf(t, wsURL): p}, ActorTokens: map[string]string{"web": "X"}}
	ex := NewWebSocketExecutor(zap.NewNop(), idx)
	res := ex.Execute(context.Background(), WSConnectAction{URL: wsURL, ConnectionID: "c1"})
	if res.Success() {
		t.Fatalf("want failure for unresolvable auth, got %+v", res)
	}
}
```
(Ensure the test file imports include `"sync"`, `"net/url"`, `"github.com/binoctal/cerberus/internal/project"`, `"github.com/binoctal/cerberus/internal/types"`.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/head/agent/ -run 'TestConnectInjectsQueryToken|TestConnectStripsLLMSuppliedToken|TestConnectFailsWhenAuthUnresolvable' -v`
Expected: FAIL — no auth injection yet; the dial url has no `token=`; the unresolvable case does not fail.

- [ ] **Step 3: Write minimal implementation**

In `internal/head/agent/websocket.go`, add an auth-injection helper and call it from `doConnect` before dialing. After `resolveProtocol`, before `websocket.Dial`:
```go
	dialURL := wsURL(a.URL)
	var preInjectionURL string
	if proto != nil && proto.Auth != nil {
		var err error
		dialURL, preInjectionURL, err = e.injectAuth(ctx, dialURL, a, proto.Auth, opts)
		if err != nil {
			return types.WSResult{OK: false, URL: a.URL, Err: err.Error(), Latency: time.Since(start)}
		}
	} else {
		preInjectionURL = a.URL
	}
	conn, _, err := websocket.Dial(ctx, dialURL, opts)
	// ... unchanged ...
	return types.WSResult{OK: true, URL: preInjectionURL, Latency: time.Since(start)}
```
And the helper:
```go
// injectAuth resolves the declared credential, strips any existing value at
// param from the url, and injects the resolved value. It returns the dial url
// (with the secret) and the pre-injection url (without) for the result. Strategy
// "query" is implemented here; header/subprotocol populate opts.
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

func (e *WebSocketExecutor) tokenFor(actor string) (string, bool) {
	if e.idx == nil {
		return "", false
	}
	t, ok := e.idx.ActorTokens[actor]
	return t, ok && t != ""
}

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

func removeString(ss []string, s string) []string {
	out := ss[:0]
	for _, x := range ss {
		if x != s {
			out = append(out, x)
		}
	}
	return out
}
```
Note: `url.QueryEscape` is applied automatically by `u.Query().Set().Encode()` (query values are percent-encoded on `Encode`), so the injected token is correctly escaped. Update the `doConnect` dial to use `dialURL` and pass `opts` through `injectAuth` (so adjust the call signature to pass `opts`). Add `"net/url"`, `"net/http"` to imports if missing (`net/http` already imported).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/head/agent/ -run 'TestConnect' -v`
Expected: PASS — injected, stripped, unresolvable fails, result url secret-free.

- [ ] **Step 5: Commit**

```bash
git add internal/head/agent/websocket.go internal/head/agent/websocket_test.go
git commit -m "feat(ws): strip-then-inject declared auth at connect"
```

---

## Task 10: WSResult secret redaction (backstop)

Redact known-sensitive query params from `WSResult.URL` in `Summary`/`Evidence` as a defensive backstop (the executor already stores a pre-injection url; this catches any residual LLM-supplied secret).

**Files:**
- Modify: `internal/head/agent/ws_protocol.go` (add `redactSecretQuery`)
- Modify: `internal/types/result_ws.go` (use it in `Summary`/`Evidence`)
- Test: `internal/types/result_ws_test.go` (append)

**Interfaces:**
- Consumes: nothing.
- Produces: a static-denylist redaction applied to the URL whenever it is rendered.

- [ ] **Step 1: Write the failing test**

Append to `internal/types/result_ws_test.go` (the file exists from M0):
```go
func TestWSResultRedactsSecretQuery(t *testing.T) {
	r := WSResult{OK: true, URL: "ws://h/ws?type=web&token=SECRET&token2=x"}
	s := r.Summary()
	if strings.Contains(s, "SECRET") {
		t.Fatalf("summary leaks token: %s", s)
	}
	if !strings.Contains(s, "token=<redacted>") {
		t.Fatalf("summary did not redact token: %s", s)
	}
}
```
(Add `"strings"` to imports if missing.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/types/ -run TestWSResultRedactsSecretQuery -v`
Expected: FAIL — summary contains `SECRET`.

- [ ] **Step 3: Write minimal implementation**

In `internal/types/result_ws.go`, add (`types` cannot import `agent`, so the backstop redaction lives here):
```go
import "net/url"

var secretQueryParams = map[string]bool{
	"token": true, "password": true, "secret": true, "key": true,
	"apikey": true, "api_key": true, "authorization": true,
}

// redactSecretQuery redacts known-sensitive query params from a url string.
func redactSecretQuery(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	q := u.Query()
	changed := false
	for k := range q {
		if secretQueryParams[k] {
			q.Set(k, "<redacted>")
			changed = true
		}
	}
	if !changed {
		return raw
	}
	u.RawQuery = q.Encode()
	return u.String()
}
```
Use it in `Summary()` (replace `r.URL` in the format with `redactSecretQuery(r.URL)`):
```go
func (r WSResult) Summary() string {
	status := "ok"
	if !r.OK {
		status = "error"
	}
	matched := 0
	if r.MatchedMessage != "" {
		matched = 1
	}
	return fmt.Sprintf("ws %s %s (matched=%d seen=%d, %s)", status, redactSecretQuery(r.URL), matched, len(r.SeenMessages), r.Latency)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/types/ -run TestWSResult -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/types/result_ws.go internal/types/result_ws_test.go
git commit -m "feat(types): redact secret query params in WSResult"
```

---

## Task 11: Steer prompt best-effort hint

Update the static steer prompt to carry a best-effort hint that declared-protocol auth is executor-injected (so the LLM may omit credentials) and that routing follows the declared `type_path`. Correctness does not depend on this (strip-then-inject); it is token-saving only.

**Files:**
- Modify: `internal/head/agent/prompts.go:21-33` (the WS primitives block)
- Test: `internal/head/agent/prompts_test.go` (append)

**Interfaces:** none (prompt text + content assertion).

- [ ] **Step 1: Write the failing test**

Append to `internal/head/agent/prompts_test.go`:
```go
func TestSteerPromptMentionsProtocolDeclaration(t *testing.T) {
	for _, want := range []string{
		"declares a protocol",
		"omit credentials",
		"type_path",
	} {
		if !contains(promptSteerSystem, want) {
			t.Fatalf("steer prompt missing %q", want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/head/agent/ -run TestSteerPromptMentionsProtocolDeclaration -v`
Expected: FAIL — substrings absent.

- [ ] **Step 3: Write minimal implementation**

In `internal/head/agent/prompts.go`, edit the WS primitives block (a single raw-string literal — edit inline, no concatenation, no backticks). Replace the `ws_connect` bullet and append a note after the existing rules (within the `promptSteerSystem` backtick string, before its closing backtick). Concretely, change the `ws_connect` line to:
```
- ws_connect {url, headers?, subprotocols?, connection_id?, credential_ref?}: open a persistent connection. If the target service declares a protocol (see the project context), the executor auto-injects auth — omit credentials from the url in that case. Otherwise put credentials in url query, headers, or subprotocols as the protocol requires.
```
And append before the closing backtick:
```
Protocol declarations: when a service declares a protocol, its auth is injected by the executor (do not duplicate credentials) and ws_receive matches by the declared type_path. The routing key value you pass to ws_receive (the "type" argument) is the expected value at that path, not the path itself.
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/head/agent/ -run TestSteerPrompt -v`
Expected: PASS (new + existing prompt assertions).

- [ ] **Step 5: Commit**

```bash
git add internal/head/agent/prompts.go internal/head/agent/prompts_test.go
git commit -m "feat(agent): note protocol-declaration auth in steer prompt"
```

---

## Task 12: Executor doc + full suite green

**Files:**
- Modify: `cerberus-docs/executors/websocket.md`
- Verification: `make check`.

**Interfaces:** none (documentation + verification).

- [ ] **Step 1: Update the executor doc**

Append a "Protocol declaration" section to `cerberus-docs/executors/websocket.md` documenting the optional `protocol:` block (framing/type_path/auth), the strip-then-inject behavior, the M0 fallback when absent, and the per-case namespacing + serialization guarantees. Cross-link the M1 design spec.

- [ ] **Step 2: Run fmt + lint + test**

Run: `make check`
Expected: fmt clean, lint clean, all tests PASS including `-race`. Fix any lint nits (unused `messageType` if not removed, unused imports).

- [ ] **Step 3: Commit docs + any lint fixes**

```bash
git add cerberus-docs/executors/websocket.md
# plus any lint-fixed source files
git commit -m "docs(ws): document the protocol declaration block"
```

---

## Definition of Done

- A service with a `protocol:` block has auth executor-injected (strip-then-inject), routing by the declared `type_path`, and json framing — verified by unit tests.
- A service without `protocol:` behaves exactly as M0 — verified by the fallback test.
- Parallel cases sharing a `connection_id` do not collide (namespaced by `tc.ID`); concurrent receives on one connection serialize (`-race` clean).
- Credentials never appear in `WSResult` or logs (pre-injection url + denylist redaction).
- `make check` (fmt + lint + test) is green; spec and plan committed under `cerberus-docs/`.
- M1 deferred items (handshake, roles, text/binary framing, runtime dogfooding) remain listed in the spec's Non-Goals / Open Questions.
