# WS Static Token Auth — Design

Status: Design (autonomous; chosen 2026-07-24 after the live dogfood found a gap).
Trigger: `cerberus run` against open-agents is blocked at the WS auth step because
cerberus's WS query/header/subprotocol auth is flow-based — `BuildWSProtocolIndex`
reads `Credentials.RawToken`, which `auth_setup` populates ONLY for actors with an
`Auth` flow. A static dev backdoor like open-agents' web `demo_token` (no flow)
yields no token, so the connect fails "no token for actor." Many real test targets
auth with a static/pre-provisioned token (API key, dev token) that has no login flow.

## Goal

An actor may declare a **static token** directly (YAML-loadable), used for WS auth
when the actor has no auth flow (or the flow did not resolve a token). A flow's
resolved `RawToken` still wins when present. This unblocks `cerberus run` against
static-token / dev-backdoor WS targets without forcing a contrived auth flow.

## Design

### `CredentialRef` (`internal/project/schema.go`)

Add a YAML-loadable `Token`:

```go
type CredentialRef struct {
	Email    string            `yaml:"email"`
	Password string            `yaml:"password"`
	Headers  map[string]string `yaml:"headers,omitempty"`
	// Token is a static WS auth token (API key / dev backdoor) used when the actor
	// has no auth flow. A flow-resolved RawToken (below) takes precedence. Loaded
	// from YAML (credentials.yaml, gitignored) — same secret hygiene as password.
	Token string `yaml:"token,omitempty"`
	// RawToken is the flow-resolved token (runtime; never YAML-loaded).
	RawToken   string            `yaml:"-" json:"-"`
	PathParams map[string]string `yaml:"-" json:"-"` // F3
}
```

### `BuildWSProtocolIndex` (`internal/head/agent/ws_protocol.go`)

Use the static `Token` as a fallback when `RawToken` is empty:

```go
for _, a := range cfg.Actors {
	token := a.Credentials.RawToken      // flow-resolved wins
	if token == "" {
		token = a.Credentials.Token      // static fallback (no flow / flow failed)
	}
	if token != "" {
		idx.ActorTokens[a.Name] = token
	}
	// F3 path-params unchanged
}
```

Called during the agent run phase (after `auth_setup` populated `RawToken`), so the
flow token is already resolved when present; the static `Token` is always available
(YAML-loaded). No ordering change.

### Validation (`internal/project/validate_actors.go` or `validate_auth.go`)

`Token` is optional. No conflict rule (an actor MAY have both a flow and a static
`Token` — the flow wins; the static is a fallback). No new validation required.

## Behavior changes

- An actor with `credentials.token` (and no/failed flow) now supplies a WS auth
  token → WS query/header/subprotocol auth works for static-token targets.
- A flow-resolved `RawToken` still takes precedence (unchanged for flow actors).
- No `token` + no flow ⇒ unchanged (no token, auth fails as today).

## Constraints

- Go 1.25, pure-Go; `coder/websocket v1.8.14` ONLY; no new deps; no expression
  evaluator; no protocol-schema change.
- Production change confined to `internal/project/schema.go` (`CredentialRef.Token`)
  + `internal/head/agent/ws_protocol.go` (the fallback) + tests/docs. Executor/
  runSteps/stepToAction/TestStep/protocol-schema unchanged.
- Secret hygiene: `Token` rides `CredentialRef` (credentials.yaml, gitignored),
  never placed in any prompt (the authdiscover guarantee carries over — it is a
  credential value like password). `BuildWSProtocolIndex` stashes it in
  `ActorTokens` (runtime, never logged).
- Author `binoctal <binoctal@gmail.com>`; no Co-Authored-By; English; docs only in
  `cerberus-docs/`; `make check` green.

## Testing

- `BuildWSProtocolIndex`: uses static `Token` when no `RawToken`; `RawToken` wins
  when both; no token ⇒ not in `ActorTokens` (unchanged).
- `CredentialRef.Token` YAML round-trip.
- Existing WS auth tests green (flow path unchanged).

## Non-goals

- Static tokens for HTTP auth (the `Headers` map already covers static HTTP auth).
- A static-token concept at the protocol level (per-actor is correct — different
  actors have different tokens).
- Auto-provisioning (the user supplies the static token in credentials.yaml).
