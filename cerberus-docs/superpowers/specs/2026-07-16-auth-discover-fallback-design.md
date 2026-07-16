# Auth Fallback (Session-Only Discovery) — Design

Date: 2026-07-16
Status: Approved (design), pending implementation plan
Builds on: `2026-07-12-auth-flow-design.md` (core loop) and `2026-07-16-auth-discover-design.md` (Component 3a `cerberus auth discover`, both merged to main).

## Problem

An actor without an `auth:` block cannot authenticate to targets that require a dynamic credential. Today the only fix is to run `cerberus auth discover` first to persist a block. For a one-off session against a new target, that round-trip is friction.

## Goal

When an actor has no `auth:` block and the fallback is enabled, discover an `AuthFlow` in-memory at session setup, use it for that session only (never write disk), and log a suggestion to persist it with `cerberus auth discover`.

## Decisions (locked)

- **Trigger point: `resolveActorAuth` (session setup), NOT Scout planning.** `authdiscover` is independent of Scout after 3a (it owns its source selection), and the shared `s.Driver` is ready at session construction (`lifecycle_factory.go`), so `resolveActorAuth` can call it directly at setup. This reuses the existing auth path and avoids touching `Scout.Plan`. (Revises the original auth-flow spec's "during planning": that assumed 3a would reuse Scout's code analysis, which it does not.)
- **Trigger condition: actor has no `Auth` block (`a.Auth == nil`).** A login failure on an *existing* block does NOT trigger fallback — that is usually a config error (wrong path/field) and re-discovering a flow the user already configured adds little value. The existing degrade behavior covers it.
- **Persistence: in-memory only.** Set `a.Auth` on the session's in-memory config copy; never write `project.yaml`. A persist hint is logged.
- **Opt-in switch.** `settings.auth.discover_fallback` (bool), default **off**. A setup-time LLM call has cost and side effects; it must not be the default behavior.
- **Security: reuse `authdiscover`'s guarantees.** Credential values never enter the prompt; `Driver.Decide` parse errors are wrapped so the raw response never leaks.

## Component

Modify `internal/session/auth_setup.go` `resolveActorAuth`: when `a.Auth == nil` and the fallback is enabled, call `authdiscover.Discover(ctx, s.Driver, s.Config, actor.Name, serviceURL)`; on success set `a.Auth` in-memory, then proceed through `agent.ResolveAuthHeader` as usual, and log a persist hint. On failure, degrade.

Add a settings field on `project.Settings` (`internal/project/schema.go`):

```go
type AuthSettings struct {
    DiscoverFallback bool `yaml:"discover_fallback,omitempty"`
}
type Settings struct {
    // ... existing fields ...
    Auth AuthSettings `yaml:"auth,omitempty"`
}
```

## Flow

`resolveActorAuth`, per actor:

1. `a.Auth != nil` → existing path: `agent.ResolveAuthHeader`; on success write header, on failure warn + degrade.
2. `a.Auth == nil` AND `settings.auth.discover_fallback` → `authdiscover.Discover`:
   - success → set `a.Auth` (in-memory), call `ResolveAuthHeader`, write header, info-log "discovered auth for actor X (session-only); run `cerberus auth discover --actor X` to persist".
   - `ErrNoAuthFlow` or any error → warn-log, actor stays unauthenticated.
3. otherwise (`a.Auth == nil`, fallback off) → skip; actor unauthenticated (today's behavior).

## Error handling / security

- `Discover` failure (`ErrNoAuthFlow`, parse error, network) → warn log, actor unauthenticated, session never aborts.
- Credential values are never logged or placed in the prompt (`authdiscover` guarantee).
- `s.Driver == nil` (some test/in-memory sessions) → degrade gracefully (skip fallback, warn).

## Testing

- Mock `*ai.Driver` injected via the session: actor with no `auth` + fallback on → `Credentials.Headers` filled, `a.Auth` set in-memory, persist hint logged.
- `Discover` fails (ErrNoAuthFlow / parse error) → no header written, actor unauthenticated, no panic.
- Fallback off → `Discover` never called, actor stays unauthenticated.
- `s.Driver == nil` → graceful degrade.
- `make check` (fmt + lint + race) green.

## Acceptance

Against a target requiring a JWT, with no `auth:` block and `settings.auth.discover_fallback: true`: the Agent authenticates using the session-discovered flow, and the log suggests running `cerberus auth discover` to persist. After persisting and re-running, behavior is identical (the persistent block path). With the fallback off, behavior is unchanged from today (unauthenticated).

## Out of scope

- Persisting the discovered flow (that is `cerberus auth discover`).
- Re-discovery on login failure of an existing block.
- Refreshing an expired token mid-session (token lifecycle is one-shot by design).
