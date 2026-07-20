# WebSocket Realtime Engine (M2) — Roles & Handshake — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a service declare named roles (each with a credential, discriminator query params, and an optional mandatory handshake) and have the WS executor expand a `ws_connect { role }` into credential injection + param injection + auto-handshake, while preserving M1 behavior for non-role connects.

**Architecture:** A `Roles` map on the M1 `Protocol` carries per-connection-type facts. `doConnect` resolves the role, reuses M1 `injectAuth` (refactored to take an explicit resolved actor) for the credential, strip-then-injects the role's discriminator params, and — when the role declares a handshake — runs a `readMu`-guarded post-dial receive loop (matching via M1 `extractTypePath`) that succeeds on the awaited type or fails+cleans-up on timeout. No new threading (roles ride on `WSProtocolIndex.ByHost`); no evaluator (dynamic url stays LLM-supplied).

**Tech Stack:** Go 1.25, `github.com/coder/websocket` v1.8.14, `gopkg.in/yaml.v3`, table-driven Go tests mirroring `internal/head/agent/http_test.go` and `internal/project/authflow_schema_test.go`.

## Global Constraints

- Go 1.25, module `github.com/binoctal/cerberus`, pure-Go (no CGo).
- WebSocket library is `github.com/coder/websocket` v1.8.14; do NOT add `nhooyr.io/websocket` or any expression/JSONPath/evaluator dependency (cerberus has no runtime evaluator — roles are static templates; the dynamic url stays LLM-supplied).
- Comments and commit messages in English. Commit author `binoctal <binoctal@gmail.com>`, no `Co-Authored-By`.
- Follow existing comment density and naming; table-driven tests.
- All documentation under `cerberus-docs/` only (never `docs/`).
- Each task leaves the tree compiling and the focused tests green; the final task runs `make check` (fmt+lint+test, `-race`).
- Adding a field to an existing action type does NOT require registry/deref/plugin wiring.

**Spec:** `cerberus-docs/superpowers/specs/2026-07-21-ws-realtime-engine-m2-roles-design.md`

---

## File Structure

**Modify (no new files):**
- `internal/project/protocol_schema.go` — `Protocol.Roles`, `ProtocolRole`, `RoleHandshake`.
- `internal/project/validate_protocol.go` — role validation in `ValidateProtocol`.
- `internal/types/actions_http.go` — `WSConnectAction.Role`.
- `internal/head/agent/websocket.go` — `injectAuth` actor-param refactor; `doConnect` role resolution + param strip-then-inject + auto-handshake + failure cleanup; a shared `setQueryParam` helper.
- `internal/head/agent/prompts.go` — best-effort role hint (inline raw-string edit).
- `cerberus-docs/executors/websocket.md` — roles documentation.

**Tests:** append to `internal/project/protocol_schema_test.go`, `internal/project/validate_protocol_test.go`, `internal/types/ws_actions_test.go`, `internal/head/agent/websocket_test.go`, `internal/head/agent/prompts_test.go`.

---

## Task 1: Role schema (ProtocolRole, RoleHandshake, Protocol.Roles)

**Files:**
- Modify: `internal/project/protocol_schema.go`
- Test: `internal/project/protocol_schema_test.go` (append)

**Interfaces:**
- Produces: `ProtocolRole{CredentialRef, Params, Handshake}`, `RoleHandshake{AwaitType, Timeout}`, `Protocol.Roles map[string]*ProtocolRole` — used by validation (Task 2) and the executor (Task 4/5).

- [ ] **Step 1: Write the failing test**

Append to `internal/project/protocol_schema_test.go`:
```go
func TestProtocolRolesYAMLRoundTrip(t *testing.T) {
	in := `
name: rt
url: http://localhost:8787
protocol:
  type_path: type
  auth: { strategy: query, param: token, credential_ref: web-actor }
  roles:
    web:
      credential_ref: web-actor
      params: { type: web }
      handshake: { await_type: devices:sync, timeout: 5 }
    bridge:
      credential_ref: bridge-actor
      params: { type: bridge }
`
	var svc Service
	if err := yaml.Unmarshal([]byte(in), &svc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if svc.Protocol == nil || len(svc.Protocol.Roles) != 2 {
		t.Fatalf("roles = %+v", svc.Protocol)
	}
	web := svc.Protocol.Roles["web"]
	if web == nil || web.CredentialRef != "web-actor" || web.Params["type"] != "web" {
		t.Fatalf("web role = %+v", web)
	}
	if web.Handshake == nil || web.Handshake.AwaitType != "devices:sync" || web.Handshake.Timeout != 5 {
		t.Fatalf("web handshake = %+v", web.Handshake)
	}
	bridge := svc.Protocol.Roles["bridge"]
	if bridge == nil || bridge.Handshake != nil {
		t.Fatalf("bridge role = %+v", bridge)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/project/ -run TestProtocolRolesYAMLRoundTrip -v`
Expected: FAIL — `unknown field roles` / `svc.Protocol.Roles undefined`.

- [ ] **Step 3: Write minimal implementation**

In `internal/project/protocol_schema.go`, add the two types (after `ProtocolAuth`) and the field on `Protocol`:
```go
// ProtocolRole declares a named connection type's credential, discriminator
// query params, and optional mandatory handshake. The executor expands a
// ws_connect that names this role.
type ProtocolRole struct {
	// CredentialRef names the actor whose resolved raw token is injected for
	// this role (overrides protocol.auth.credential_ref).
	CredentialRef string `yaml:"credential_ref"`
	// Params are discriminator query params strip-then-injected onto the dial
	// url. Must not include protocol.auth.param (the token slot).
	Params map[string]string `yaml:"params,omitempty"`
	// Handshake is the optional mandatory post-connect exchange.
	Handshake *RoleHandshake `yaml:"handshake,omitempty"`
}

// RoleHandshake declares the message the executor auto-awaits after connect.
type RoleHandshake struct {
	// AwaitType is the routing-key value (at protocol.type_path) to wait for.
	AwaitType string `yaml:"await_type"`
	// Timeout is seconds to wait; must be > 0 (validation) so a mandatory
	// handshake cannot hang a case indefinitely.
	Timeout int `yaml:"timeout,omitempty"`
}
```
Add to `Protocol` (after `Auth`):
```go
	// Roles maps named connection types (e.g. "web", "bridge") to their
	// per-role declaration. Empty means no roles (M1 behavior).
	Roles map[string]*ProtocolRole `yaml:"roles,omitempty"`
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/project/ -run TestProtocol -v`
Expected: PASS (new + existing protocol tests green).

- [ ] **Step 5: Commit**

```bash
git add internal/project/protocol_schema.go internal/project/protocol_schema_test.go
git commit -m "feat(project): add Protocol roles and handshake schema"
```

---

## Task 2: Role validation

**Files:**
- Modify: `internal/project/validate_protocol.go`
- Test: `internal/project/validate_protocol_test.go` (append)

**Interfaces:**
- Consumes: `ProtocolRole`/`RoleHandshake` (Task 1), `cfg.Actors`.
- Produces: role checks inside `ValidateProtocol`.

- [ ] **Step 1: Write the failing test**

Append to `internal/project/validate_protocol_test.go`:
```go
func TestValidateProtocolRoles(t *testing.T) {
	actor := Actor{Name: "web"}
	cases := []struct {
		name    string
		p       *Protocol
		wantErr string
	}{
		{name: "role ok", p: &Protocol{Auth: &ProtocolAuth{Strategy: "query", Param: "token"},
			Roles: map[string]*ProtocolRole{"web": {CredentialRef: "web", Params: map[string]string{"type": "web"}}}},
			wantErr: ""},
		{name: "role credential_ref missing actor", p: &Protocol{Roles: map[string]*ProtocolRole{"web": {CredentialRef: "ghost"}}},
			wantErr: "credential_ref"},
		{name: "role param collides with auth.param", p: &Protocol{Auth: &ProtocolAuth{Strategy: "query", Param: "token"},
			Roles: map[string]*ProtocolRole{"web": {Params: map[string]string{"token": "x"}}}},
			wantErr: "auth.param"},
		{name: "handshake missing await_type", p: &Protocol{Roles: map[string]*ProtocolRole{"web": {Handshake: &RoleHandshake{Timeout: 5}}}},
			wantErr: "await_type"},
		{name: "handshake timeout zero", p: &Protocol{Roles: map[string]*ProtocolRole{"web": {Handshake: &RoleHandshake{AwaitType: "x", Timeout: 0}}}},
			wantErr: "timeout"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateProtocol(tc.p, []Actor{actor})
			if tc.wantErr == "" && err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)) {
				t.Fatalf("want err containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}
```
(Add `"strings"` to imports if missing.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/project/ -run TestValidateProtocolRoles -v`
Expected: FAIL — no role validation performed (bad roles accepted).

- [ ] **Step 3: Write minimal implementation**

In `internal/project/validate_protocol.go`, inside `ValidateProtocol` (after the existing `Auth` block, before `return nil`), add:
```go
	for name, role := range p.Roles {
		if role.CredentialRef != "" {
			found := false
			for _, a := range actors {
				if a.Name == role.CredentialRef {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("roles[%q].credential_ref %q does not match any actor", name, role.CredentialRef)
			}
		}
		for k := range role.Params {
			if p.Auth != nil && k == p.Auth.Param {
				return fmt.Errorf("roles[%q].params[%q] collides with auth.param (token slot)", name, k)
			}
		}
		if role.Handshake != nil {
			if role.Handshake.AwaitType == "" {
				return fmt.Errorf("roles[%q].handshake.await_type is required", name)
			}
			if role.Handshake.Timeout <= 0 {
				return fmt.Errorf("roles[%q].handshake.timeout must be > 0", name)
			}
		}
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/project/ -v`
Expected: PASS (new role tests + existing validate tests green).

- [ ] **Step 5: Commit**

```bash
git add internal/project/validate_protocol.go internal/project/validate_protocol_test.go
git commit -m "feat(project): validate protocol roles"
```

---

## Task 3: WSConnectAction.Role field

**Files:**
- Modify: `internal/types/actions_http.go` (WSConnectAction)
- Test: `internal/types/ws_actions_test.go` (append)

**Interfaces:**
- Produces: `WSConnectAction.Role string` (json `role,omitempty`) — used by `doConnect` (Task 4).

- [ ] **Step 1: Write the failing test**

Append to `internal/types/ws_actions_test.go`:
```go
func TestWSConnectActionRoleRoundTrip(t *testing.T) {
	envelope, err := MarshalAction(&WSConnectAction{
		URL: "ws://x", ConnectionID: "c1", Role: "web",
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got, err := UnmarshalAction(envelope)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	c, ok := got.(WSConnectAction)
	if !ok || c.Role != "web" {
		t.Fatalf("role round-trip lost: %+v", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/types/ -run TestWSConnectActionRoleRoundTrip -v`
Expected: FAIL — `unknown field role` / `c.Role undefined`.

- [ ] **Step 3: Write minimal implementation**

In `internal/types/actions_http.go`, add to `WSConnectAction` (after `CredentialRef`):
```go
	// Role optionally names a declared protocol role whose credential,
	// discriminator params, and handshake the executor expands. When set,
	// CredentialRef is ignored and the role's declaration drives auth + params.
	Role string `json:"role,omitempty"`
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/types/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/types/actions_http.go internal/types/ws_actions_test.go
git commit -m "feat(types): add Role to WSConnectAction"
```

---

## Task 4: Role resolution + injectAuth actor-param refactor + param strip-then-inject

Refactor `injectAuth` to take the resolved actor explicitly (so a role's `credential_ref` is authoritative), resolve the role + effective credential_ref + discriminator params in `doConnect`, and strip-then-inject the role's params. Non-role connects behave exactly as M1.

**Files:**
- Modify: `internal/head/agent/websocket.go` (`injectAuth`, `doConnect`; add `setQueryParam`)
- Test: `internal/head/agent/websocket_test.go` (append)

**Interfaces:**
- Consumes: `Protocol.Roles` (Task 1), `WSConnectAction.Role` (Task 3), M1 `injectAuth`/`tokenFor`/`stripQuery`.
- Produces: `doConnect` role handling; `injectAuth(ctx, dialURL, actor, auth, opts)`; `setQueryParam(rawURL, key, val) string`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/head/agent/websocket_test.go`:
```go
func TestConnectRoleUsesRoleCredentialAndParams(t *testing.T) {
	wsURL, latestQuery := newWSTestServerCapture(t)
	p := &project.Protocol{
		Auth: &project.ProtocolAuth{Strategy: "query", Param: "token", CredentialRef: "default-actor"},
		Roles: map[string]*project.ProtocolRole{
			"web": {CredentialRef: "web-actor", Params: map[string]string{"type": "web"}},
		},
	}
	idx := &WSProtocolIndex{
		ByHost:      map[string]*project.Protocol{hostOf(t, wsURL): p},
		ActorTokens: map[string]string{"web-actor": "WEB-JWT", "default-actor": "DEF"},
	}
	ex := NewWebSocketExecutor(zap.NewNop(), idx)
	res := ex.Execute(context.Background(), WSConnectAction{URL: wsURL + "?type=WRONG", ConnectionID: "c1", Role: "web"})
	if !res.Success() {
		t.Fatalf("connect failed: %+v", res)
	}
	q := latestQuery()
	if !strings.Contains(q, "token=WEB-JWT") || strings.Contains(q, "DEF") {
		t.Fatalf("role credential not used: %s", q)
	}
	if !strings.Contains(q, "type=web") || strings.Contains(q, "type=WRONG") {
		t.Fatalf("role param not strip-then-injected: %s", q)
	}
}

func TestConnectUnknownRoleFails(t *testing.T) {
	wsURL, latestQuery := newWSTestServerCapture(t)
	p := &project.Protocol{Roles: map[string]*project.ProtocolRole{"web": {}}}
	idx := &WSProtocolIndex{ByHost: map[string]*project.Protocol{hostOf(t, wsURL): p}}
	ex := NewWebSocketExecutor(zap.NewNop(), idx)
	res := ex.Execute(context.Background(), WSConnectAction{URL: wsURL, ConnectionID: "c1", Role: "ghost"})
	if res.Success() {
		t.Fatalf("unknown role should fail: %+v", res)
	}
	if latestQuery() != "" {
		t.Fatalf("unknown role should not dial: %s", latestQuery())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/head/agent/ -run 'TestConnectRoleUsesRoleCredentialAndParams|TestConnectUnknownRoleFails' -v`
Expected: FAIL — role ignored (default-actor token used; no failure for unknown role).

- [ ] **Step 3: Write minimal implementation**

In `internal/head/agent/websocket.go`:

(a) Add a shared query-param strip-then-inject helper (used by `injectAuth` and role params):
```go
// setQueryParam removes any existing key then sets it to val on the url's query
// string, returning the rewritten url. Falls back to rawURL on parse error.
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
```

(b) Refactor `injectAuth` to take the resolved actor (replace the `actor := auth.CredentialRef; if a.CredentialRef != "" ...` block and the `a types.WSConnectAction` param):
```go
func (e *WebSocketExecutor) injectAuth(ctx context.Context, dialURL string, actor string, auth *project.ProtocolAuth, opts *websocket.DialOptions) (string, string, error) {
	token, ok := e.tokenFor(actor)
	if !ok {
		return "", "", fmt.Errorf("ws auth: no token for actor %q", actor)
	}
	switch auth.Strategy {
	case "query":
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
```
(`ctx` stays in the signature for parity; it is unused now but retained — `make check` confirms no lint failure.)

(c) In `doConnect`, replace the M1 credential/auth block. After `proto := e.resolveProtocol(a.URL)` and the existing `opts` build, insert role + credential resolution before the `if proto != nil && proto.Auth != nil` block:
```go
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
```
and change the `injectAuth` call to pass `credentialRef`:
```go
		dialURL, preInjectionURL, authErr = e.injectAuth(ctx, dialURL, credentialRef, proto.Auth, opts)
```
and after the auth block, strip-then-inject role params:
```go
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
```
(For the common query-auth + query-param case, `preInjectionURL` was already set to `stripQuery(...)` by `injectAuth`; role-param injection further mutates `dialURL` only — the result url keeps the auth-stripped preInjectionURL. Add a `maybeAuthParam(proto)` helper returning `proto.Auth.Param` (or `""`) so the unknown-role failure strips the token slot from the echoed url.)

Add:
```go
func maybeAuthParam(p *project.Protocol) string {
	if p != nil && p.Auth != nil {
		return p.Auth.Param
	}
	return ""
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/head/agent/ -run 'TestConnect|TestWSConnect|TestWSSend|TestWSReceive' -v`
Expected: PASS — new role tests green; existing M1 connect tests (non-role) still green (behavior preserved).

- [ ] **Step 5: Commit**

```bash
git add internal/head/agent/websocket.go internal/head/agent/websocket_test.go
git commit -m "feat(ws): resolve role credential and params at connect"
```

---

## Task 5: Auto-handshake after connect

When a role declares a `handshake`, run a `readMu`-guarded post-dial receive loop matching `await_type` via `extractTypePath`; succeed with evidence on match, fail + clean up on timeout.

**Files:**
- Modify: `internal/head/agent/websocket.go` (`doConnect`)
- Test: `internal/head/agent/websocket_test.go` (append)

**Interfaces:**
- Consumes: `role.Handshake` (Task 1), the stored `wsEntry` (its `readMu`), M1 `extractTypePath`/`caseNamespace`.
- Produces: auto-handshake in `doConnect`; connect `SeenMessages` populated with handshake evidence.

- [ ] **Step 1: Write the failing tests**

Append to `internal/head/agent/websocket_test.go`:
```go
func TestConnectRoleHandshakeSuccess(t *testing.T) {
	wsURL := newWSTestServer(t, func(conn *websocket.Conn) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = conn.Write(ctx, websocket.MessageText, []byte(`{"type":"devices:sync"}`))
		_, _, _ = conn.Read(ctx)
	})
	p := &project.Protocol{TypePath: "type",
		Roles: map[string]*project.ProtocolRole{"web": {Handshake: &project.RoleHandshake{AwaitType: "devices:sync", Timeout: 2}}}}
	ex := NewWebSocketExecutor(zap.NewNop(), protocolIndexForURL(t, wsURL, p))
	res := ex.Execute(context.Background(), WSConnectAction{URL: wsURL, ConnectionID: "c1", Role: "web"})
	ws, ok := res.(types.WSResult)
	if !ok || !ws.Success() {
		t.Fatalf("connect failed: %+v", res)
	}
	if !strings.Contains(strings.Join(ws.SeenMessages, ""), "devices:sync") {
		t.Fatalf("handshake message not in evidence: %v", ws.SeenMessages)
	}
}

func TestConnectRoleHandshakeTimeoutFailsAndCleansUp(t *testing.T) {
	wsURL := newWSTestServer(t, func(conn *websocket.Conn) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_, _, _ = conn.Read(ctx) // never sends devices:sync
	})
	p := &project.Protocol{TypePath: "type",
		Roles: map[string]*project.ProtocolRole{"web": {Handshake: &project.RoleHandshake{AwaitType: "devices:sync", Timeout: 1}}}}
	ex := NewWebSocketExecutor(zap.NewNop(), protocolIndexForURL(t, wsURL, p))
	ctx := context.Background()
	res := ex.Execute(ctx, WSConnectAction{URL: wsURL, ConnectionID: "c1", Role: "web"})
	if res.Success() {
		t.Fatalf("handshake timeout should fail: %+v", res)
	}
	// Connection must be cleaned up: a subsequent send fails as unknown id.
	if ex.Execute(ctx, WSSendAction{ConnectionID: "c1", Message: `{"type":"x"}`}).Success() {
		t.Fatal("connection should be removed after handshake timeout")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/head/agent/ -run 'TestConnectRoleHandshake' -v`
Expected: FAIL — no handshake run (connect succeeds immediately; timeout case doesn't fail/cleanup).

- [ ] **Step 3: Write minimal implementation**

In `internal/head/agent/websocket.go`, after the dial succeeds and `e.store(...)` (and after role params), before building the success `WSResult`, add the handshake. `role` is already in scope from Task 4's resolution (declared at the doConnect body scope):
```go
	// Auto-handshake (role with handshake declared).
	var seen []string
	if role != nil && role.Handshake != nil {
		hsTimeout := time.Duration(role.Handshake.Timeout) * time.Second
		hsCtx, hsCancel := context.WithTimeout(ctx, hsTimeout)
		entry, ok := e.lookup(caseNamespace(ctx, id))
		if !ok {
			hsCancel()
			return types.WSResult{OK: false, URL: preInjectionURL, Err: "ws handshake: connection vanished", Latency: time.Since(start)}
		}
		entry.readMu.Lock()
		matched := false
		for {
			_, data, rerr := entry.conn.Read(hsCtx)
			if rerr != nil {
				break // timeout or peer close
			}
			seen = append(seen, string(data))
			path := "type"
			if proto.TypePath != "" {
				path = proto.TypePath
			}
			if t, ok := extractTypePath(data, path); ok && t == role.Handshake.AwaitType {
				matched = true
				break
			}
		}
		entry.readMu.Unlock()
		hsCancel()
		if !matched {
			// Mandatory handshake did not complete: close + remove the connection.
			key := caseNamespace(ctx, id)
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
```
(Adjust Task 4's role-resolution so `role *project.ProtocolRole` stays in scope — the `var role *project.ProtocolRole` + assignment inside the `if a.Role != ""` block, instead of a fresh `role :=`.)

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/head/agent/ -run 'TestConnect|TestWSConnect|TestWSSend|TestWSReceive|TestConnectionNamespacing|TestReceiveSerialized' -race -v`
Expected: PASS — handshake success + evidence; timeout fail + cleanup; existing tests + `-race` green.

- [ ] **Step 5: Commit**

```bash
git add internal/head/agent/websocket.go internal/head/agent/websocket_test.go
git commit -m "feat(ws): auto-run role handshake after connect"
```

---

## Task 6: Steer prompt hint + executor doc + make check green

**Files:**
- Modify: `internal/head/agent/prompts.go`, `cerberus-docs/executors/websocket.md`
- Test: `internal/head/agent/prompts_test.go` (append)
- Verify: `make check`.

**Interfaces:** none (documentation + prompt text + verification).

- [ ] **Step 1: Write the failing test**

Append to `internal/head/agent/prompts_test.go`:
```go
func TestSteerPromptMentionsRoles(t *testing.T) {
	for _, want := range []string{"role", "handshake"} {
		if !contains(promptSteerSystem, want) {
			t.Fatalf("steer prompt missing %q", want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/head/agent/ -run TestSteerPromptMentionsRoles -v`
Expected: FAIL — substrings absent.

- [ ] **Step 3: Write minimal implementation**

In `internal/head/agent/prompts.go`, inline-edit `promptSteerSystem` (single raw-string literal — no concatenation, no backticks). Extend the `ws_connect` bullet to list `role?` and append a short note before the closing backtick:
```
- ws_connect {url, headers?, subprotocols?, connection_id?, credential_ref?, role?}: open a persistent connection. When the service declares roles, set role to the connection type (e.g. "web"); the executor injects its credential and discriminator params and runs any mandatory handshake automatically — omit token and discriminator params, provide the base url with dynamic values (userId, deviceId). Otherwise behave as M1 (omit credentials if auth is declared; provide the rest).
```
and append:
```
Roles: a service may declare named roles (web, bridge, ...). A role bundles its credential, discriminator query params, and an optional mandatory handshake (auto-awaited after connect). Use ws_connect with role when the target declares roles.
```

In `cerberus-docs/executors/websocket.md`, add a "Roles" subsection under "Protocol Declaration": document `roles:` (`credential_ref`, `params`, `handshake`), the `role` field on `ws_connect`, executor-expansion (credential + param strip-then-inject + auto-handshake), the non-decisive handshake, failure-on-timeout, and the no-role → M1 fallback. Cross-link the M2 design spec.

- [ ] **Step 4: Run fmt + lint + test**

Run: `make check`
Expected: fmt clean, lint clean, all tests PASS including `-race`. Fix any lint nits.

- [ ] **Step 5: Commit**

```bash
git add internal/head/agent/prompts.go internal/head/agent/prompts_test.go cerberus-docs/executors/websocket.md
git commit -m "feat(agent): document roles in steer prompt and executor doc"
```

---

## Definition of Done

- A `ws_connect { role: "web" }` gets the role's credential (strip-then-inject), discriminator params (strip-then-inject), and auto-handshake — verified by unit tests.
- Handshake arrival → connect success + evidence in `SeenMessages` (non-decisive); timeout → connect fails + connection cleaned up.
- Unknown role / role-without-protocol → connect fails without dialing.
- A `ws_connect` without `role` behaves exactly as M1 (regression-green).
- Credentials never leak (M1 secret hygiene preserved).
- `make check` (fmt + lint + test, `-race`) is green; spec and plan committed under `cerberus-docs/`.
- Role discovery (how the LLM learns role names) remains an Open Question — M2 ships the mechanism + graceful fallback; value-realization via dogfooding/M3.
