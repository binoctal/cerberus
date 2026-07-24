# WS Static Token Auth Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development. Checkbox tracking.

**Goal:** An actor may declare a static WS auth token (`credentials.token`) used when it has no auth flow, so `cerberus run` works against static-token / dev-backdoor WS targets (the gap that blocked the open-agents dogfood). Flow-resolved `RawToken` still wins.

**Architecture:** `CredentialRef.Token` (YAML-loadable) + a fallback in `BuildWSProtocolIndex` (`RawToken` else `Token`). Executor/runSteps/stepToAction/TestStep/protocol-schema unchanged.

**Tech Stack:** Go 1.25; existing `CredentialRef`, `BuildWSProtocolIndex`.

## Global Constraints
- Go 1.25, pure-Go; `coder/websocket v1.8.14` ONLY; no new deps; no expression evaluator; no protocol-schema change.
- Production change confined to `internal/project/schema.go` + `internal/head/agent/ws_protocol.go` (+ tests/docs). Author `binoctal <binoctal@gmail.com>`; NO Co-Authored-By; English; docs only in `cerberus-docs/`; `make check` green.
- Secret hygiene: `Token` rides `CredentialRef` (credentials.yaml, gitignored), never in any prompt.
- Backwards-compat: no `token` + no flow ⇒ identical (no ActorTokens entry).

---

### Task 1: static-token schema + index fallback

**Files:**
- Modify: `internal/project/schema.go` (`CredentialRef` += `Token`)
- Modify: `internal/head/agent/ws_protocol.go` (`BuildWSProtocolIndex` fallback)
- Modify: `internal/project/credentials_test.go` (Token YAML round-trip)
- Modify: `internal/head/agent/ws_protocol_test.go` (Token fallback)

**Reviewer note (controller):** sonnet. Verify: RawToken wins over Token; Token used when RawToken empty; neither ⇒ no ActorTokens entry (backwards-compat); secret hygiene (Token never logged); YAML round-trip.

- [ ] **Step 1: Tests**

`internal/head/agent/ws_protocol_test.go` — extend or add:
```go
func TestBuildWSProtocolIndex_StaticToken(t *testing.T) {
	cfg := &project.Config{
		Services: []project.Service{{Name: "s", URL: "ws://h/ws", Protocol: &project.Protocol{Roles: map[string]*project.ProtocolRole{"web": {}}}}},
		Actors: []project.Actor{
			{Name: "static", Credentials: project.CredentialRef{Token: "demo_token"}},
			{Name: "flowed", Credentials: project.CredentialRef{Token: "FALLBACK", RawToken: "FLOW"}},
			{Name: "none", Credentials: project.CredentialRef{}},
		},
	}
	idx := agent.BuildWSProtocolIndex(cfg)
	require.Equal(t, "demo_token", idx.ActorTokens["static"], "static Token used when no RawToken")
	require.Equal(t, "FLOW", idx.ActorTokens["flowed"], "RawToken wins over static Token")
	_, ok := idx.ActorTokens["none"]
	require.False(t, ok, "no token + no flow ⇒ no ActorTokens entry")
}
```

`internal/project/credentials_test.go` — add a `token: demo_token` YAML round-trip row to an existing credentials-load test (or a focused `TestCredentialRef_TokenRoundTrip`).

- [ ] **Step 2: Run → FAIL (unknown field Token / failing assertion)**

- [ ] **Step 3: Implement**

`internal/project/schema.go` — add to `CredentialRef` (before RawToken):
```go
	// Token is a static WS auth token (API key / dev backdoor) used when the actor
	// has no auth flow. A flow-resolved RawToken takes precedence. Loaded from YAML
	// (credentials.yaml, gitignored) — same secret hygiene as password.
	Token string `yaml:"token,omitempty"`
```

`internal/head/agent/ws_protocol.go` — in `BuildWSProtocolIndex`, replace the token line:
```go
		token := a.Credentials.RawToken
		if token == "" {
			token = a.Credentials.Token // static fallback (no flow / flow failed)
		}
		if token != "" {
			idx.ActorTokens[a.Name] = token
		}
```
(was: `if a.Credentials.RawToken != "" { idx.ActorTokens[a.Name] = a.Credentials.RawToken }`)

- [ ] **Step 4: Run → PASS**

`go test -race -count=1 ./internal/project/ ./internal/head/agent/` then `make check`.

- [ ] **Step 5: Commit**

```bash
git add internal/project/schema.go internal/head/agent/ws_protocol.go internal/project/credentials_test.go internal/head/agent/ws_protocol_test.go
git commit -m "feat(ws): static WS auth token fallback (no-flow/dev-backdoor targets)"
```

---

### Task 2: docs

- Modify: `cerberus-docs/configuration/project.md` (or wherever actors/credentials are documented) — note `credentials.token` for static WS auth. And `cerberus-docs/executors/websocket.md` if the auth section mentions credential sources.
- [ ] `make check` + commit `docs(ws): document static WS auth token`.

---

## Post-implementation (controller)
- [ ] **Whole-branch review (opus):** `074d5a6..HEAD`. Verify: RawToken precedence; static fallback; backwards-compat; secret hygiene; constraints; `make check` green.
- [ ] **Finish:** ff-merge main + delete branch (NO push unless asked).
- [ ] **Memory + ledger:** static-token gap closed; WS-auth now serves static-token targets.
