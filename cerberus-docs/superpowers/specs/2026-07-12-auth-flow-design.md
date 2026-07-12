# Declarative Auth Flow — Design

Date: 2026-07-12
Status: Approved (design), pending implementation plan

## Problem

Cerberus can only inject *static* auth headers today. `authHeadersFor`
(`internal/head/agent/rules.go`) sets `X-Test-User` from an actor's email and
copies `Credentials.Headers` verbatim; `withActorHeaders`
(`internal/head/agent/react_loop_helpers.go`) merges those same static headers
into HTTP actions. There is no login/token executor anywhere in
`internal/head/agent`.

Consequently, targets guarded by dynamic credentials (e.g. open-agents, whose
`worker.ts` middleware requires `Authorization: Bearer <JWT>` verified against
`JWT_SECRET`/HS256) cannot be tested authenticated. A signed, expiring JWT
cannot be hard-coded in `project.yaml`. In a real run against open-agents this
produced 6 failing cases where the Agent floundered against protected endpoints
it could not authenticate to — not genuine defects, just an unreachable auth
wall.

## Goal

Let cerberus obtain a dynamic credential once per session, deterministically,
via declarative configuration — with LLM assistance to *author* that
configuration. Keep it general (data-driven, no target-specific code) while
covering the common case: a REST login endpoint returning a token in JSON,
injected as a Bearer header.

## Decisions (locked during brainstorming)

- **Primary mechanism: declarative `auth:` block**, with LLM discovery as a
  fallback authoring aid. (Chose "both" over pure-declarative or pure-LLM.)
- **Token lifecycle: one-shot.** Log in once at session setup; cache the
  resulting header for the whole session. No auto-refresh, no retry-on-401.
  Sessions run in 1–2 minutes; token TTLs are far longer. YAGNI.
- **LLM authoring: command + Scout fallback.** A `cerberus auth discover`
  command writes the block to disk (with user confirmation); Scout also
  discovers at runtime when the block is missing/failing, but only for the
  current session (in-memory, never writes disk).
- **Extraction: JSON dot-path → Bearer injection.** Covers open-agents
  `/api/dev/setup` (returns `{token: ...}`) and most modern REST logins.
  Cookie / multi-step OAuth deferred until actually needed.

## Component 1 — Config schema (`internal/project/schema.go`)

New optional `Auth *AuthFlow` field on `Actor`. Absent → existing static-header
behavior, zero breakage.

```yaml
actors:
  - name: test-user
    credentials: { email: dev@openagents.local, password: dev123456 }
    auth:
      login:
        method: POST
        path: /api/dev/setup          # relative to service.URL, or absolute
        body: { email: "{email}", password: "{password}" }  # {email}/{password} from credentials
        headers: {}                    # optional static headers on the login request
      token_from: token                # dot-path into response JSON (e.g. data.accessToken)
      inject_as: "Authorization: Bearer {token}"  # {token} = extracted value
```

```go
type AuthFlow struct {
    Login     AuthLogin `yaml:"login"`
    TokenFrom string    `yaml:"token_from"` // dot-path into response JSON
    InjectAs  string    `yaml:"inject_as"`  // header template, {token} substituted
}

type AuthLogin struct {
    Method  string            `yaml:"method"`
    Path    string            `yaml:"path"`
    Body    map[string]string `yaml:"body,omitempty"`
    Headers map[string]string `yaml:"headers,omitempty"`
}
```

## Component 2 — Auth executor (`internal/head/agent/authflow.go`)

Single-purpose unit: run one login, return the header to inject.

```go
// ResolveAuthHeader runs an actor's login flow once and returns the header
// name/value to inject. Called at session setup; result cached for the session.
func ResolveAuthHeader(ctx context.Context, svcURL string, actor project.Actor) (name, value string, err error)
```

Flow:
1. Interpolate `{email}`/`{password}` (from `credentials`) into `login.body`.
2. Send one real HTTP request to `svcURL + login.path` (or an absolute path).
3. Extract the token from response JSON by the `token_from` dot-path (small
   dot-path walker over the decoded map).
4. Interpolate `{token}` into `inject_as`; split into header name + value.

**Integration point:** at session setup (near `SetupHeadDrivers`, where actors
are already loaded), run this once per actor that has an `Auth` block and write
the resulting header into that actor's `Credentials.Headers`. Downstream
`authHeadersFor` and `withActorHeaders` are then **unchanged** — the static
injection path naturally carries the dynamic token. The executor's only job is
to turn a dynamic token into a static header; injection reuses existing code.

## Component 3 — LLM discovery (command + Scout fallback)

### 3a. `cerberus auth discover` (`cmd/cerberus/main_auth.go`)

One-shot discovery, writes to disk after confirmation:
1. Read the target service's code (reuse Scout's codebase-reading capability) —
   scan routes/middleware to locate the login endpoint, request-body shape, and
   the token field in the response.
2. LLM produces an `AuthFlow` suggestion via **structured output** (deserialized
   straight into the Go type — no free-text parsing).
3. Print the suggested YAML; prompt for confirmation (y/N).
4. On confirm, write the `auth:` block back to the matching actor in
   `project.yaml`.

### 3b. Scout runtime fallback

At session setup, if a service needs auth (an actor exists but has no `Auth`, or
login execution failed), Scout discovers an `AuthFlow` during planning and uses
it **for this session only** (in-memory, not persisted), logging a suggestion to
run `auth discover` to make it permanent.

Key distinction: **the command writes disk (with confirmation); Scout fallback
only uses the result in-memory once.** The file-mutating side effect is confined
to the explicit command.

## Component 4 — Error handling, security, testing

### Error handling (degrade, never abort — matches cerberus fault tolerance)
- Login request fails / non-2xx → warning log; actor degrades to unauthenticated
  (still tests `auth-required`-style invariants that expect rejection).
- `token_from` path not found → warning + degrade; log the actual response key
  names to help fix the config.
- `auth discover` LLM discovery fails → command errors out; never writes a
  broken `project.yaml`.
- Missing interpolation variable (e.g. body references `{email}` but credentials
  has none) → config-validation error, not a runtime surprise.

### Security
- Token values NEVER logged or printed (record header name, status code, length
  only).
- `credentials` already lives in gitignored `.cerberus/credentials.yaml`;
  `auth.body` passwords treated the same way.

### Testing
- `authflow_test.go` (httptest login server): (a) JSON extract → correct header;
  (b) dot-path `data.accessToken`; (c) non-2xx degrade; (d) missing field
  degrade; (e) `{email}` interpolation.
- Schema validation test: missing interpolation variable errors.
- `auth discover` command: mock LLM client returns a structured `AuthFlow`;
  verify correct YAML write-back.
- `make check` (fmt + lint + race test) green.

### Acceptance
After configuring open-agents' `/api/dev/setup` flow, re-run a session: the
protected-endpoint cases that previously failed with 401 obtain a real token,
return 200, and are judged normally by the Examiner.
