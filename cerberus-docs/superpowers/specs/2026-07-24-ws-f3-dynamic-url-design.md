# WS Dynamic URL Path Params (F3) — Design

Status: Design (autonomous; chosen 2026-07-24 as deferred polish, now requested).
Trigger: open-agents embeds a runtime `userId` in the WS path (`/ws/{userId}`) that
is provisioned per-run by `/api/dev/setup` (which returns `config.userId` alongside
the device token). cerberus's auth flow already fetches that JSON response and
extracts the token via `token_from`; it discards the `userId`. Today the userId must
be baked literally into `svc.URL`. F3 captures additional fields from the auth
response and templates them into the URL at connect time.

## Goal

A WS service URL may contain `{param}` placeholders (e.g. `ws://h/ws/{userId}`)
that are resolved at connect time from fields captured by the connecting role's
auth flow. No more literal baking of runtime-provisioned path ids.

## Approach (resolved fork)

**Capture from the existing auth flow response + URL templating.** cerberus already
executes each actor's `AuthFlow` once per session (`resolveActorAuth` →
`ResolveAuthHeader`) and extracts the token via `token_from` (a dot-path into the
login response JSON). F3 extends this: the `AuthFlow` may declare `path_params`
(`{url_param: response_dot_path}`); those fields are captured from the SAME
response, stored on the actor, carried through the WS protocol index, and
substituted into `{param}` placeholders in the dial URL at connect. This reuses the
flow that already exists — it is not a new auth mechanism.

The path-param value comes from the connecting **role's** `credential_ref` actor
(web and bridge typically share a userId, but each role resolves its own actor).

## Design

### `AuthFlow` (`internal/project/authflow_schema.go`)

Add `PathParams`:

```go
type AuthFlow struct {
	Login     AuthLogin `yaml:"login"`
	TokenFrom string    `yaml:"token_from"`
	InjectAs  string    `yaml:"inject_as"`
	// PathParams captures additional fields from the login response for URL
	// templating: url-param name -> response JSON dot-path (same syntax as
	// token_from). At WS connect, {name} placeholders in the service URL are
	// substituted from these captured values. Empty when no path params are
	// needed (backwards-compatible).
	PathParams map[string]string `yaml:"path_params,omitempty"`
}
```

Validation (`validate_auth.go`): a `path_params` key must be a reasonable param name
(letters/digits/underscore); the dot-path is unconstrained (resolved at runtime;
absent path ⇒ that connect fails with a clear error, not a panic). Existing
validation unchanged when `path_params` is absent.

### `ResolveAuthHeader` (`internal/head/agent/authflow.go`)

Return a struct instead of the 4-tuple, carrying the captured path params:

```go
type AuthResult struct {
	HeaderName  string            // e.g. "Authorization"
	HeaderValue string            // resolved header value (with {token} filled)
	RawToken    string            // the raw token (for WS query/header/subprotocol)
	PathParams  map[string]string // url-param -> captured value (F3)
}
```

`ResolveAuthHeader(ctx, svcURL, actor) (*AuthResult, error)`. The login response JSON
is parsed once; `token_from` extracts the token, and each `path_params` dot-path
extracts its value (same dot-path walker). An absent `path_params` dot-path ⇒
`PathParams[name] = ""` (the connect-time template resolution then fails clearly).

Caller ripple: `internal/session/auth_setup.go:44` is the only production caller;
`authflow_test.go` call sites (6) update to the struct. (A prior signature change —
`wsIdx` — rippled similarly and was managed.)

### `Credentials` (`internal/project/schema.go`)

Add a runtime-only `PathParams`:

```go
type CredentialRef struct {
	Email    string            `yaml:"email"`
	Password string            `yaml:"password"`
	Headers  map[string]string `yaml:"headers,omitempty"`
	RawToken string            `yaml:"-" json:"-"`
	// PathParams holds url-param -> value captured by the auth flow (F3).
	// Runtime-only; populated by auth setup, never loaded from YAML.
	PathParams map[string]string `yaml:"-" json:"-"`
}
```

`auth_setup.go` writes `a.Credentials.PathParams = result.PathParams` alongside
`RawToken`.

### `WSProtocolIndex` (`internal/head/agent/ws_protocol.go`)

Carry actor path params alongside tokens:

```go
type WSProtocolIndex struct {
	ByHost      map[string]*project.Protocol
	ActorTokens map[string]string            // actor -> raw token (existing)
	ActorPathParams map[string]map[string]string // actor -> {url-param: value} (F3)
}
```

`BuildWSProtocolIndex` populates `ActorPathParams[a.Name] = a.Credentials.PathParams`.

### `doConnect` URL templating (`internal/head/agent/websocket.go`)

After role-param injection and before dialing, resolve `{param}` placeholders in
the dial URL from the connecting actor's path params. The actor is the role's
`credential_ref` (or the protocol default). Resolution:

```go
// resolveURLPathParams substitutes {param} placeholders in rawURL from params.
// A placeholder with no value ⇒ the URL is returned with the placeholder intact,
// and the caller's dial will fail clearly (or a dedicated error if preferred).
```

Applied to `dialURL` after role params, before `websocket.Dial`. Placeholders that
have no captured value are left as-is (the dial then fails with a recognizable
`{userId}` in the URL — a clear signal, not a silent wrong URL). A declared
`path_params` capture whose response dot-path was absent yields `""`, which
substitutes to empty — flagged by validation guidance to declare only present paths.

`preInjectionURL` (the secret-free URL returned in `WSResult`) is computed AFTER
path-param substitution but with the auth `token` param still stripped, so a path
id like `userId` shows in the echoed URL (it is the endpoint, not a credential —
the token remains the only scrubbed value). This matches today's behavior (the URL
path is not secret; the auth token is).

## Behavior changes

- A service URL with `{param}` placeholders + a role whose actor captured those
  params ⇒ the dial URL has them substituted at connect.
- No `path_params` declared, or no `{param}` in the URL ⇒ byte-identical to today.
- The auth flow response is still fetched once per session; F3 only extracts more
  fields from it (no extra HTTP).
- Secret hygiene preserved: only the auth token is scrubbed from `WSResult.URL`;
  the path id is part of the endpoint. Captured path params are not placed in the
  prompt (authdiscover guarantee carries over — they ride the same Credentials
  runtime fields as RawToken).

## Constraints

- Go 1.25, pure-Go; `coder/websocket v1.8.14` ONLY; no new deps; no expression
  evaluator; no protocol-schema change beyond the new optional `path_params` field.
- Commit author `binoctal <binoctal@gmail.com>`; no Co-Authored-By; English; docs
  only in `cerberus-docs/`; `make check` green.
- Secret hygiene: captured path params ride the runtime-only Credentials fields
  (yaml:"-"), never loaded from YAML, never in the prompt. The token stays the only
  scrubbed URL value.

## Testing

- `AuthFlow.PathParams` YAML round-trip + validation (absent ⇒ unchanged; bad param
  name rejected).
- `ResolveAuthHeader` returns `PathParams` extracted from the login response
  (dot-path); absent dot-path ⇒ empty value; multiple params.
- `auth_setup` stores `Credentials.PathParams`.
- `BuildWSProtocolIndex` populates `ActorPathParams`.
- `doConnect`: a `{userId}` URL + a role whose actor has `userId` captured ⇒ dials
  the substituted URL; no placeholder / no capture ⇒ unchanged (or clear failure).
- Existing WS/auth tests green (backwards-compat: no path_params ⇒ identical).

## Non-goals

- Runtime derivation of path params from a source OTHER than the auth flow response
  (e.g., a separate provisioning call). The auth flow already fetches the response;
  F3 captures more fields from it.
- Path-param templating for non-WS executors (HTTP). WS only for now.
- Multiple actors contributing to one URL (each role resolves its own actor's
  params).
- F4 (done) / batching beyond type-aliases.

## Open questions (resolve in the plan)

1. Whether an unresolved `{param}` (no captured value) should fail the connect with
   a dedicated error vs dial with the literal `{param}` (which fails at the server).
   Lean: dedicated error — clearer than a server 404 on a `{userId}` URL.
2. Whether `path_params` validation should require the dot-path to be non-empty.
   Lean: yes (an empty dot-path captures nothing useful).
3. Whether to also template role `params` values (e.g., a role param `{userId}`).
   Lean: no for now — role params are static discriminators; path params come from
   the auth flow. Keep them separate.
