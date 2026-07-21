# WebSocket Realtime Engine (M2) — Role Discriminator Carriers Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a `protocol.roles.<name>` declare discriminator facts on all three carriers (query params, headers, subprotocols), strip-then-injected by the executor, with carrier-specific token-slot collision validation.

**Architecture:** `ProtocolRole` gains `Headers` (map) and `Subprotocols` (list) alongside the existing `params` (query map). `doConnect` strip-then-injects each carrier (delete-then-set headers, remove-then-append subprotocols) in the existing role-expansion block. Validation rejects a role occupying the auth token slot on the matching carrier. No new action types; secret hygiene untouched (headers/subprotocols never reach `WSResult.URL`).

**Tech Stack:** Go 1.25 · `github.com/coder/websocket` v1.8.14 · stdlib only.

## Global Constraints

- Go 1.25, module `github.com/binoctal/cerberus`, **pure Go (no CGo)**.
- WS library **fixed** `github.com/coder/websocket` v1.8.14; **no** `nhooyr.io/websocket`; **no** new deps.
- **No runtime expression evaluator** (M0 Constraint 3).
- Commit author **`binoctal <binoctal@gmail.com>`**, **no** `Co-Authored-By`. Comments and commit messages in **English**.
- Documentation **only** in `cerberus-docs/`; **never** `docs/`.
- `make check` (fmt + lint + test `-race`) must be green. Tests are table-driven, mirroring `internal/head/agent/websocket_test.go` and `internal/project/validate_protocol_test.go`.
- `websocket_test.go` / `validate_protocol_test.go` are `package X` with **no alias** → action/struct types need the package prefix where used cross-package.
- `promptSteerSystem` is a **single raw-string literal** → any steer-prompt edit is inline (no concatenation, no backticks).
- Counters/values read from an httptest server goroutine must be race-safe (buffered channel, not a shared counter).

**Spec:** `cerberus-docs/superpowers/specs/2026-07-21-ws-role-carriers-design.md` (read for rationale).

---

## File Structure

- `internal/project/protocol_schema.go` — `ProtocolRole.Headers` + `.Subprotocols` fields (Task 1).
- `internal/project/validate_protocol.go` — carrier-specific token-slot collision checks (Task 1).
- `internal/project/validate_protocol_test.go` — new validation rows (Task 1).
- `internal/head/agent/websocket.go` — `doConnect` role header/subprotocol strip-then-inject (Task 2).
- `internal/head/agent/websocket_test.go` — role header/subprotocol injection tests (Task 2).
- `cerberus-docs/executors/websocket.md` — Roles section: new table rows + expansion text (Task 3).
- `internal/head/agent/prompts.go` — steer prompt ws_connect role hint (Task 3).

---

## Task 1: ProtocolRole Headers/Subprotocols schema + carrier-specific validation

**Files:**
- Modify: `internal/project/protocol_schema.go` (`ProtocolRole`, ~line 32-40)
- Modify: `internal/project/validate_protocol.go` (role loop, ~line 42-68)
- Test: `internal/project/validate_protocol_test.go` (`TestValidateProtocolRoles` cases)

**Interfaces:** none (config schema + validation).

- [ ] **Step 1: Write the failing tests**

In `internal/project/validate_protocol_test.go`, add these rows to the `cases` slice in `TestValidateProtocolRoles` (alongside the existing rows):

```go
		{name: "role headers ok (no auth)", p: &Protocol{Roles: map[string]*ProtocolRole{"web": {Headers: map[string]string{"X-Role": "web"}}}}, wantErr: ""},
		{name: "role subprotocols ok (no auth)", p: &Protocol{Roles: map[string]*ProtocolRole{"web": {Subprotocols: []string{"web.v1"}}}}, wantErr: ""},
		{name: "role header collides with auth.param (header strategy)", p: &Protocol{Auth: &ProtocolAuth{Strategy: "header", Param: "X-Role"}, Roles: map[string]*ProtocolRole{"web": {Headers: map[string]string{"X-Role": "web"}}}}, wantErr: "auth.param"},
		{name: "role subprotocol collides with auth.param (subprotocol strategy)", p: &Protocol{Auth: &ProtocolAuth{Strategy: "subprotocol", Param: "token"}, Roles: map[string]*ProtocolRole{"web": {Subprotocols: []string{"token"}}}}, wantErr: "auth.param"},
		{name: "role header ok when auth strategy differs", p: &Protocol{Auth: &ProtocolAuth{Strategy: "query", Param: "token"}, Roles: map[string]*ProtocolRole{"web": {Headers: map[string]string{"token": "x"}}}}, wantErr: ""},
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -run TestValidateProtocolRoles -v ./internal/project/`
Expected: FAIL — `Headers`/`Subprotocols` fields do not exist (won't compile), so the file fails to build. (If it compiles via zero values, the "header collides" cases would pass-vacuously or the ok cases would fail; the compile error on the missing fields is the primary RED.)

- [ ] **Step 3: Write minimal implementation**

In `internal/project/protocol_schema.go`, replace the `ProtocolRole` struct with:

```go
// ProtocolRole declares a named connection type's credential, discriminator
// facts, and optional mandatory handshake. The executor expands a ws_connect
// that names this role.
type ProtocolRole struct {
	// CredentialRef names the actor whose resolved raw token is injected for
	// this role (overrides protocol.auth.credential_ref).
	CredentialRef string `yaml:"credential_ref"`
	// Params are discriminator query params strip-then-injected onto the dial
	// url. Must not include protocol.auth.param when auth.strategy is query
	// (the token slot).
	Params map[string]string `yaml:"params,omitempty"`
	// Headers are discriminator dial headers strip-then-injected (delete-then-
	// set). Must not include protocol.auth.param when auth.strategy is header.
	Headers map[string]string `yaml:"headers,omitempty"`
	// Subprotocols are discriminator subprotocol names offered (strip-then-
	// injected: remove-then-append). Must not include protocol.auth.param when
	// auth.strategy is subprotocol.
	Subprotocols []string `yaml:"subprotocols,omitempty"`
	// Handshake is the optional mandatory post-connect exchange.
	Handshake *RoleHandshake `yaml:"handshake,omitempty"`
}
```

(Scope is minimal: only the two new fields `Headers` and `Subprotocols` are added. `CredentialRef` and its existing `yaml:"credential_ref"` tag are unchanged — do not add `omitempty` or alter any other existing field.)

In `internal/project/validate_protocol.go`, replace the role loop's collision check (the block currently:

```go
		for k := range role.Params {
			if p.Auth != nil && k == p.Auth.Param {
				return fmt.Errorf("roles[%q].params[%q] collides with auth.param (token slot)", name, k)
			}
		}
```

) with carrier-specific checks:

```go
		if p.Auth != nil {
			// A role must not occupy the auth token slot on the carrier auth
			// uses; on other carriers the same name is harmless (different slot).
			switch p.Auth.Strategy {
			case "query":
				for k := range role.Params {
					if k == p.Auth.Param {
						return fmt.Errorf("roles[%q].params[%q] collides with auth.param (token slot)", name, k)
					}
				}
			case "header":
				for k := range role.Headers {
					if k == p.Auth.Param {
						return fmt.Errorf("roles[%q].headers[%q] collides with auth.param (token slot)", name, k)
					}
				}
			case "subprotocol":
				for _, s := range role.Subprotocols {
					if s == p.Auth.Param {
						return fmt.Errorf("roles[%q].subprotocols[%q] collides with auth.param (token slot)", name, s)
					}
				}
			}
		}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -run 'TestValidateProtocolRoles|TestValidateProtocol$' -v ./internal/project/`
Expected: PASS — new rows pass; the existing "role param collides with auth.param" row (strategy=query) still rejects under the carrier-specific check.

- [ ] **Step 5: Commit**

```bash
git add internal/project/protocol_schema.go internal/project/validate_protocol.go internal/project/validate_protocol_test.go
git commit -m "feat(project): add role header/subprotocol carriers with carrier-specific validation"
```

---

## Task 2: doConnect strip-then-inject role headers and subprotocols

**Files:**
- Modify: `internal/head/agent/websocket.go` (`doConnect` role-expansion block, ~line 181-184)
- Test: `internal/head/agent/websocket_test.go` (append 2 tests)

**Interfaces:**
- Consumes: `ProtocolRole.Headers map[string]string` and `ProtocolRole.Subprotocols []string` (Task 1); existing `removeString` (`websocket.go:345`), `opts *websocket.DialOptions` (built earlier in `doConnect`).

- [ ] **Step 1: Write the failing tests**

Append these tests to `internal/head/agent/websocket_test.go`:

```go
// TestConnectRoleHeadersInjected proves a role's declared header is
// strip-then-injected onto the dial: any LLM-supplied value at the same key is
// removed and exactly the role's value reaches the server.
func TestConnectRoleHeadersInjected(t *testing.T) {
	seen := make(chan string, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		select {
		case seen <- r.Header.Get("X-Role"):
		default:
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("accept: %v", err)
			return
		}
		defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()
		_, _, _ = conn.Read(r.Context())
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"

	p := &project.Protocol{Roles: map[string]*project.ProtocolRole{
		"web": {Headers: map[string]string{"X-Role": "web"}},
	}}
	ex := NewWebSocketExecutor(zap.NewNop(), protocolIndexForURL(t, wsURL, p))
	// LLM also supplies X-Role (wrong) — must be stripped to the role's value.
	res := ex.Execute(context.Background(), types.WSConnectAction{
		URL: wsURL, ConnectionID: "c1", Role: "web",
		Headers: map[string]string{"X-Role": "LLM-WRONG"},
	})
	if !res.Success() {
		t.Fatalf("connect failed: %+v", res)
	}
	select {
	case h := <-seen:
		if h != "web" {
			t.Fatalf("server saw X-Role=%q, want %q (LLM value not stripped)", h, "web")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server never observed the role header")
	}
}

// TestConnectRoleSubprotocolsInjected proves a role's declared subprotocol is
// offered, and an LLM-supplied duplicate is stripped (offered exactly once).
func TestConnectRoleSubprotocolsInjected(t *testing.T) {
	seen := make(chan string, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		select {
		case seen <- r.Header.Get("Sec-WebSocket-Protocol"):
		default:
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("accept: %v", err)
			return
		}
		defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()
		_, _, _ = conn.Read(r.Context())
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"

	p := &project.Protocol{Roles: map[string]*project.ProtocolRole{
		"web": {Subprotocols: []string{"web.v1"}},
	}}
	ex := NewWebSocketExecutor(zap.NewNop(), protocolIndexForURL(t, wsURL, p))
	// LLM also offers web.v1 (duplicate) — must be stripped to exactly one offer.
	res := ex.Execute(context.Background(), types.WSConnectAction{
		URL: wsURL, ConnectionID: "c1", Role: "web",
		Subprotocols: []string{"web.v1"},
	})
	if !res.Success() {
		t.Fatalf("connect failed: %+v", res)
	}
	select {
	case offered := <-seen:
		if !strings.Contains(offered, "web.v1") {
			t.Fatalf("role subprotocol not offered: %q", offered)
		}
		if c := strings.Count(offered, "web.v1"); c != 1 {
			t.Fatalf("role subprotocol offered %d times, want 1 (LLM duplicate not stripped): %q", c, offered)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server never observed offered subprotocols")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -run 'TestConnectRoleHeadersInjected|TestConnectRoleSubprotocolsInjected' -v ./internal/head/agent/`
Expected: FAIL — the role header never reaches the server (not injected); the role subprotocol is either not offered or offered twice (LLM duplicate not stripped).

- [ ] **Step 3: Write minimal implementation**

In `internal/head/agent/websocket.go`, in `doConnect`, locate the role query-param injection block (the comment `// Role discriminator params (strip-then-inject onto the dial url).` and its loop). Insert these two blocks immediately AFTER that loop and BEFORE the `// After role-param injection, recompute preInjectionURL` comment:

```go
	// Role discriminator headers (strip-then-inject): remove any LLM-supplied
	// value at this key, then set the role's. opts.HTTPHeader already carries
	// a.Headers, so this normalizes to exactly the role's value. Headers never
	// appear in WSResult.URL, so preInjectionURL is unaffected.
	for k, v := range role.Headers {
		opts.HTTPHeader.Del(k)
		opts.HTTPHeader.Set(k, v)
	}
	// Role discriminator subprotocols (strip-then-inject): remove any
	// LLM-supplied entry at this name, then append the role's.
	for _, s := range role.Subprotocols {
		opts.Subprotocols = append(removeString(opts.Subprotocols, s), s)
	}
```

- [ ] **Step 4: Run tests to verify they pass (incl. regression)**

Run: `go test -run 'TestConnectRoleHeadersInjected|TestConnectRoleSubprotocolsInjected|TestConnectRoleUsesRoleCredentialAndParams|TestConnectUnknownRoleFails|TestConnectInjectsQueryToken' -v ./internal/head/agent/`
Expected: PASS — new tests pass; existing role + auth tests still pass (the role query-param path, credential resolution, and auth injection are unchanged).

- [ ] **Step 5: Commit**

```bash
git add internal/head/agent/websocket.go internal/head/agent/websocket_test.go
git commit -m "feat(ws): strip-then-inject role headers and subprotocols at connect"
```

---

## Task 3: Document role carriers (executor doc + steer prompt)

**Files:**
- Modify: `cerberus-docs/executors/websocket.md` (Roles section table + expansion text)
- Modify: `internal/head/agent/prompts.go` (ws_connect role bullet — inline, single raw-string literal)

**Interfaces:** none (docs + prompt hint).

- [ ] **Step 1: Update the executor doc**

In `cerberus-docs/executors/websocket.md`, in the `### Roles` subsection:

(a) The roles table (the `| Field | Type | ... |` table listing `roles.<name>.credential_ref`, `.params`, `.handshake.*`). Add two rows after the `.params` row:

```markdown
| `roles.<name>.headers` | map[string]string | — | Discriminator dial headers strip-then-injected (delete-then-set). Must not include `auth.param` when `auth.strategy` is `header`. |
| `roles.<name>.subprotocols` | []string | — | Discriminator subprotocol names offered (strip-then-injected: remove-then-append). Must not include `auth.param` when `auth.strategy` is `subprotocol`. |
```

(b) The `.params` row — append the carrier qualifier so it reads consistently. Change:

```markdown
| `roles.<name>.params` | map[string]string | — | Discriminator query params applied (strip-then-inject) to the dial url. Must not include `protocol.auth.param` (token-slot collision is rejected by validation). |
```

to:

```markdown
| `roles.<name>.params` | map[string]string | — | Discriminator query params applied (strip-then-inject) to the dial url. Must not include `protocol.auth.param` when `auth.strategy` is `query` (token-slot collision is rejected by validation). |
```

(c) The Executor-expansion numbered list (the step that says "(4) strip-then-injects each of `role.params` into the url query"). Change step (4):

```markdown
and (5) after dial, if the role declares a `handshake`, runs an internal
```

— renumber to (6), and insert a new step (5) before it:

```markdown
and (5) strip-then-injects each of `role.headers` (delete-then-set) and each
entry of `role.subprotocols` (remove-then-append) onto the dial — normalizing
any LLM-supplied value at those slots to exactly the role's; and (6) after
dial, if the role declares a `handshake`, runs an internal
```

(The existing step (4) about `role.params` query injection stays as-is; the new headers/subprotocols step is (5), handshake becomes (6).)

- [ ] **Step 2: Update the steer prompt (single raw-string literal — inline edit, no backticks)**

In `internal/head/agent/prompts.go`, the ws_connect bullet currently contains:

```
the executor injects its credential and discriminator params and runs any mandatory handshake automatically — omit token and discriminator params, provide the base url with dynamic values (userId, deviceId).
```

Change it inline to:

```
the executor injects its credential and discriminator params/headers/subprotocols and runs any mandatory handshake automatically — omit token and discriminator params/headers/subprotocols, provide the base url with dynamic values (userId, deviceId).
```

(Inline replacement of the two occurrences of "discriminator params" with "discriminator params/headers/subprotocols" within that single sentence. No backticks, no concatenation.)

- [ ] **Step 3: Verify build + lint + test green + stale-bullet audit**

Run: `make check`
Expected: exit 0. Docs/prompt-only change; no behavior change. Confirm the steer prompt still compiles (single raw-string literal intact — no backticks introduced).

Run: `grep -rn "discriminator params\b" cerberus-docs/executors/websocket.md internal/head/agent/prompts.go`
Expected: the websocket.md `.params` row may still say "Discriminator query params" (correct — it IS the query carrier); the prompts.go bullet should now say "params/headers/subprotocols". No stale bullet claiming headers/subprotocols are unsupported.

- [ ] **Step 4: Commit**

```bash
git add cerberus-docs/executors/websocket.md internal/head/agent/prompts.go
git commit -m "docs(ws): document role header/subprotocol carriers"
```

---

## Final Verification

- [ ] `make check` green across the whole branch.
- [ ] `go build ./...` clean (no signature changes to public APIs; `ProtocolRole` gains fields only).
- [ ] No stale wording claiming role discriminators are query-only (`grep -rn "query only\|query params only" cerberus-docs/executors/websocket.md` — clean).
- [ ] `-race` clean (the two new server-goroutine tests use buffered channels, not shared counters).

## Self-Review Notes

- **Spec coverage:** D1 (parallel collections params/headers/subprotocols) → Task 1 schema ✓; D2 (strip-then-inject per carrier) → Task 2 ✓; D3 (carrier-specific collision) → Task 1 validation ✓ (also refines the existing params check from over-broad to strategy-gated, a correctness improvement); D4 (secret hygiene — headers/subprotocols never in WSResult.URL) → Task 2 leaves preInjectionURL untouched ✓; D5 (fallback byte-identical) → Task 2 guards by map/slice iteration over nil collections (no-op when absent) ✓.
- **Type consistency:** `ProtocolRole.Headers`/`.Subprotocols` field names match between Task 1 (schema) and Task 2 (doConnect reads `role.Headers`/`role.Subprotocols`). `removeString` signature unchanged.
- **Regression:** the existing `params` collision test (strategy=query) still rejects under the carrier-specific switch; the existing role credential/param injection tests are untouched by Task 2's insertion point (after the params loop).
- **No placeholders:** every code step shows complete code; every test step shows complete test bodies.
