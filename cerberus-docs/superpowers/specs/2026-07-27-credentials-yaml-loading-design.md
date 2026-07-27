# credentials.yaml Loading — Design

> Brainstormed 2026-07-27. Approach chosen by the user: **full mechanism**
> (load Token + Email + Password), **layered override** precedence
> (env > credentials.yaml > project.yaml inline), load inside `LoadFromFile`,
> malformed file → error.
> Companion: plan `2026-07-27-credentials-yaml-loading-plan.md` (to be written).
> Root cause + chain recorded in cccmemory `credentials-yaml-not-loaded-bug`.

## Background

`cerberus init` (`cmd/cerberus/init.go:68`) creates `.cerberus/credentials.yaml`,
gitignores it, and tells the user to fill it ("Set credentials in
.cerberus/credentials.yaml"). The configuration doc
(`cerberus-docs/configuration/credentials.md`) documents a three-tier priority
order: env > credentials.yaml > project.yaml inline.

No code implements the middle tier. `project.ResolveCredentials`
(`internal/project/credentials.go:9`) reads **only** env vars
(`CERBERUS_ACTOR_<NAME>_EMAIL` / `_PASSWORD`) and handles **only** Email and
Password — there is no Token handling and no file load. `credentials.yaml` is a
zombie file: created and advertised, never read.

## Problem

The 2024 dogfood for `device:online` relay passed; the 2026-07-26/27 reruns
fail deterministically. The static-token WS actor (`web`, token `demo_token`,
no auth flow) never reaches the executor with a token:

```
credentials.yaml token demo_token  ──(never loaded)──✗
project.yaml inline Token          ──(empty)────────✗
CERBERUS_ACTOR_WEB_TOKEN env       ──(not read)─────✗
                                                 ↓
ResolveCredentials → actor.Credentials.Token == ""
                                                 ↓
BuildWSProtocolIndex (ws_protocol.go:142-145) → ActorTokens lacks "web"
                                                 ↓
tokenFor("web") not-found → doConnect injectAuth "no token for actor"
                                                 ↓
ws_connect instant fail (matched=0 seen=0)
```

The same gap also starves the LLM `ws_flow` execution path of tokens, which
compounded the `ws_flow` emission-stability work.

## Root cause

`CredentialRef.Token` (`internal/project/schema.go:41`) already exists, is
YAML-loadable (`TestCredentialRef_TokenRoundTrip`), and is read by
`BuildWSProtocolIndex` as the static fallback when an actor has no auth flow.
The loader that would populate it from `credentials.yaml` was never written.
`ResolveCredentials` is the natural merge point but its signature carries no
path, and it only handles Email/Password from env.

## Approach: load `credentials.yaml` in `LoadFromFile`

One new load step, layered cleanly on the existing pipeline. **Zero call-site
changes** — the three run entry points (`main_run.go:32`, `main_verify.go:28`,
`session_handlers_helpers.go:21`) keep calling `LoadFromFile` then
`ResolveCredentials`. Loading inside `LoadFromFile` structurally eliminates the
divergent-call-site blind spot that let this bug breed (the server path is no
longer a special case to remember).

### Layering (final precedence)

```
project.yaml inline  ──┐
project.<env>.yaml ────┤   LoadFromFile  (file tier)
credentials.yaml ──────┘   ← NEW: layered-override on inline
                         ↓
cfg.Actors[].Credentials
                         ↓
ResolveCredentials        env: CERBERUS_ACTOR_<NAME>_{TOKEN,EMAIL,PASSWORD}  ← NEW _TOKEN
                         ↓
session.Config → BuildWSProtocolIndex → ActorTokens → tokenFor → ws_connect
```

**env > credentials.yaml > project.yaml inline.** This matches the precedence
already documented in `cerberus-docs/configuration/credentials.md`, so the fix
makes the documentation true rather than changing it.

Per-field override: `credentials.yaml` overwrites only the fields it provides
with a non-empty value. This sidesteps the `${VAR}` footgun — when an env var
referenced by `project.yaml` is unset, `LoadFromYAML` leaves the literal
`${VAR}` in place (`loader.go:17-23`); under fill-empty semantics that non-empty
literal would block the real value from the secrets file. Layered override makes
the secrets file authoritative for the fields it declares.

### credentials.yaml schema

Unchanged on disk — matches the `cerberus init` template (a map keyed by actor
name, distinct from `project.yaml`'s list form):

```yaml
actors:
  admin:
    email: admin@example.com
    password: changeme
    token: demo_token   # NEW field, now loaded
```

Parsed by an unexported type in `internal/project/` (same package as the loader):

```go
type credentialsFile struct {
    Actors map[string]credentialSecret `yaml:"actors"`
}
type credentialSecret struct {
    Email    string `yaml:"email"`
    Password string `yaml:"password"`
    Token    string `yaml:"token"`
}
```

### `LoadFromFile` change (`internal/project/loader.go:40`)

- **Insert point:** after the env-overlay block, immediately before `return cfg`
  (so actors introduced by a `project.<env>.yaml` overlay also receive secrets).
- **Path:** `credPath := filepath.Join(configDir, "credentials.yaml")`, reusing
  the `configDir` already computed at `loader.go:51`. For
  `.cerberus/project.yaml` this resolves to `.cerberus/credentials.yaml`; for
  `<root>/project.yaml` to `<root>/credentials.yaml` — symmetric with the
  project config location.
- **Read:**
  - `os.IsNotExist` → silent skip (the file is optional and gitignored).
  - Any other read error or YAML unmarshal error → return `error` (fail loud).
    This mirrors the env-overlay malformed handling at `loader.go:74-76` and
    honors the bug's lesson (do not fail silently on a present-but-broken
    secrets file).
- **Merge:** iterate `cfg.Actors`; for each, look up `file.Actors[name]`; if
  present, per-field override (`if v.Token != "" { a.Credentials.Token = v.Token }`,
  likewise Email and Password).
- **No re-validation:** `CredentialRef` scalar fields are not constrained by
  `Config.Validate()` (to be confirmed during implementation).

### `ResolveCredentials` change (`internal/project/credentials.go:9`)

Signature **unchanged**. Add Token to the env overlay, beside the existing
Email/Password blocks:

```go
if token := os.Getenv(envPrefix + "_TOKEN"); token != "" {
    actor.Credentials.Token = token
}
```

### `cerberus init` template (`cmd/cerberus/init.go:61`)

Advertise the now-working token field so it is discoverable:

```yaml
actors:
  admin:
    email: admin@example.com
    password: changeme
    # token: <static WS token — for actors with no auth flow (API key / dev backdoor)>
```

## What stays unchanged

- **Call sites** (`main_run.go:32`, `main_verify.go:28`,
  `session_handlers_helpers.go:21`) — zero changes.
- **`CredentialRef` schema** — `Token` field already exists with a round-trip
  test.
- **`BuildWSProtocolIndex` / `tokenFor`** (`ws_protocol.go:142-145`, `:483`) —
  already read `Credentials.Token` as the static fallback; the chain works once
  `Token` is populated.

## Files

- `internal/project/loader.go` — `LoadFromFile` gains the credentials.yaml load
  + merge step; new unexported `credentialsFile` / `credentialSecret` types.
- `internal/project/loader_test.go` — new tests (see Testing).
- `internal/project/credentials.go` — `ResolveCredentials` adds `_TOKEN` env.
- `internal/project/credentials_test.go` — new `_TOKEN` env test.
- `cmd/cerberus/init.go` — credentials.yaml template gains a `# token:` hint.
- `cerberus-docs/configuration/credentials.md` — document `_TOKEN` env and the
  `token:` field (doc-as-truth alignment).

## Testing (TDD — write the test first, watch it fail, then implement)

`internal/project/loader_test.go`:

- `TestLoadFromFile_CredentialsYAML_MergesAll` — file supplies
  Token/Email/Password for an actor → all three land on the actor.
- `TestLoadFromFile_CredentialsYAML_OverridesInline` — inline values in
  `project.yaml` are overridden by the file (layered override, per-field).
- `TestLoadFromFile_CredentialsYAML_PerFieldOverride` — file supplies only
  `token` → inline Email/Password are preserved.
- `TestLoadFromFile_CredentialsYAML_Missing_NoError` — no credentials.yaml →
  inline values preserved, no error returned.
- `TestLoadFromFile_CredentialsYAML_Malformed_Error` — unparseable YAML →
  `LoadFromFile` returns an error.
- `TestLoadFromFile_CredentialsYAML_AtCerberusLocation` — config at
  `.cerberus/project.yaml` reads `.cerberus/credentials.yaml` (path-resolution
  guard, mirrors `TestLoadFromFile_ProtocolRefResolvesAtCerberusConfigLocation`).

`internal/project/credentials_test.go`:

- `TestResolveCredentials_TokenEnv` — `CERBERUS_ACTOR_<NAME>_TOKEN` overrides a
  file/inline Token.
- Existing `TestEnvOverridesCredentialFile` / `TestCredentialFileFallback`
  remain green (env > inline semantics preserved).

**Regression-chain guard.** The config-layer tests prove "file Token →
`cfg.Actors[].Credentials.Token` populated". The existing
`TestBuildWSProtocolIndex_StaticToken` (`ws_protocol_test.go:211`) proves
"Token → `ActorTokens`". Together they cover the bug's full chain without a
duplicated integration test.

## Verification

1. **Hard gate:** `make check` (fmt + lint + test -race) EXIT 0.
2. **Dogfood rerun** (procedure:
   `cerberus-docs/technical/dogfood/2026-07-24-ws-relay-live-execution-dogfood.md`):
   start the open-agents dev server (`:8989`), `POST /api/dev/setup` (with
   `Origin` header) to provision, write `.cerberus/project.yaml` (relay) and
   `credentials.yaml` (`web: demo_token`, `bridge: deviceToken`), `make build`,
   then `cerberus run --goal device:online peer-join relay`. Confirm the
   `ws-realtime-relay-web-signal-device-online` verdict is **PASS** — `web`
   receives the relayed `device:online` because its token now reaches the
   executor.

## Out of scope (explicit)

- **`${VAR}` interpolation inside `credentials.yaml`.** The secrets file holds
  literal values; the existing env-var mechanism already covers variable
  substitution. Rejected for YAGNI.
- **Loading `Headers` from `credentials.yaml`.** Not part of the confirmed
  scope (Token/Email/Password); `init` does not advertise it. Possible
  follow-up.
- **Changes to the WS execution chain** (`BuildWSProtocolIndex`, `tokenFor`,
  auth flow). Already correct once `Token` is populated.
- **`ResolveCredentials` signature / call-site changes.** Rejected: Approach B
  preserved the divergent-call-site class of bugs; loading in `LoadFromFile`
  removes it.
- **Companion fixes already on `main`** (do not redo): `target_validate` no
  longer downgrades `ws_flow` (cbaa7a8); assembly drops empty `ws_flow` cases
  (8c9df1f); prompt guides `begin_case` + `ws_*` (2f839e7); assembly sets
  `ws_flow` `Target = service URL` (03f2058, `origin/main` HEAD).

## Related

- cccmemory `credentials-yaml-not-loaded-bug` (root cause + fix).
- cccmemory `ws-flow-emission-stability-done`, `llm-ws-flow-emission-unstable`,
  `tool-migration-live-gates-verified`.
