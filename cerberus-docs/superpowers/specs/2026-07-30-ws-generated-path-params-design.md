# WS Runtime-Generated Path Params (design)

> Status: design, 2026-07-30. Roadmap #2. Self-contained, medium scope.

## Problem

F3 already templates `{name}` placeholders in a WS service URL from
**captured** path params (`auth.path_params` → dot-path into the login response →
`Credentials.PathParams` → `ActorPathParams` → `resolveURLParams`). That only
covers values present in an auth login response.

Some realtime endpoints expect a **client-chosen id** in the URL — e.g.
`/ws/{clientId}` — where the value is not returned by any login response and
must be **generated at runtime** (a fresh uuid per session). Today
`resolveURLParams` hard-errors on any unresolved `{name}`, so such endpoints
cannot be dialed without hard-coding a literal id in config (stale, shared, not
modeling a real client).

## Gap (confirmed by exploration)

There is no runtime param *generation* anywhere. `ActorPathParams` is written
only from `Actor.Credentials.PathParams`, which is assigned only from an auth
flow's captured result (`auth_setup.go:58`). No generator / uuid / random /
timestamp hook exists in the path-param pipeline.

## Design

Add a **declared generator** parallel to captured path params, resolved at
session setup and merged into the same `Credentials.PathParams` the existing
templating pipeline already reads. **Zero executor-side change.**

### Declaration (config)

A new actor-level field (versionable in `project.yaml`, parallel to `auth:`):

```yaml
actors:
  - name: web
    service: realtime
    credentials: { token: demo_token }
    generated_path_params:
      clientId: uuid
```

`generated_path_params` is `name -> generator-kind`. The vocabulary starts with
exactly one generator — `uuid` (a v4 uuid via `github.com/google/uuid`, already
a dependency). The resolver is a `switch`, so adding `timestamp` / `ulid` /
`random` later is one case each. Unknown kinds are rejected at config
validation.

Placed on `Actor` (not `CredentialRef`) so it is not buried in the gitignored
credentials file — it is non-secret config.

### Resolution (session setup)

A new pass `resolveGeneratedPathParams` runs for **every** actor that declares
`generated_path_params`, independent of the auth flow (a no-auth actor may still
need a generated id). It runs **after** the existing auth-resolution loop so
generated values merge into whatever `PathParams` already exist (captured or
nil) — captured and generated params coexist by disjoint name.

```
project.ResolveGeneratedPathParams(spec) -> {name: synthesized-value} | error
session.resolveGeneratedPathParams: for each actor, merge resolved into Credentials.PathParams
```

Failures degrade (unknown generator should not reach here — validation guards it
— but if it does, the param is left unresolved and `resolveURLParams` errors at
connect with a clear message).

### Templating (unchanged)

`BuildWSProtocolIndex` copies `Credentials.PathParams` (now containing generated
values) into `ActorPathParams`; `pathParamsFor` + `resolveURLParams` substitute
`{clientId}` → the generated uuid. No change to the executor.

### Semantic: per-session, not per-connect

A generated param is resolved **once per session** (stable value per actor per
run). For a `/ws/{clientId}` endpoint this models a stable client identity
across reconnects/state-resume. Per-connect freshness (a brand-new uuid every
dial) is a deliberate future extension — it would require generating inside
`pathParamsFor` at connect time rather than at setup, and is not needed for the
client-chosen-id use case.

## Scope / non-goals

- One generator (`uuid`) shipped; switch is extensible.
- Per-session resolution (not per-connect).
- No new executor logic; no protocol/role-level declaration (actor-scoped only,
  matching the actor-scoped `ActorPathParams` plumbing).

## Tests

- `project`: `ResolveGeneratedPathParams` — uuid shape (regex) + freshness across
  calls; unknown generator errors; empty spec → empty result.
- `project`: validation — unknown generator and bad key name are rejected.
- `session`: a **no-auth** actor with `generated_path_params` gets a uuid in
  `Credentials.PathParams` after `resolveActorAuth` (proves independence from
  auth); an actor with **both** captured and generated params keeps both (proves
  merge, no clobber).
- Executor-level templating of a generated value is already covered by the
  existing `TestConnectTemplatedURL` (it templates from `ActorPathParams`
  regardless of how the value got there), so no new executor test is needed.
