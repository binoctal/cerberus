# Per-Step ws_connect URL — Cross-Endpoint Multi-Party — Design

> Status: design (autonomous). Date: 2026-07-30.
> Context: roadmap "多连接 / 跨 socket case 编排". An exploration confirmed the multi-connection machinery already exists and is tested (`TestRunStepsMultiConnection` opens N sockets in one `TestCase.Steps`). The single remaining structural gap is the dial URL.

## Problem

A `TestCase` with `Steps` can already open multiple sockets (one per distinct `ConnectionID`) and relay frames between them — but every `ws_connect` step dials the same URL (`tc.Target`, hard-coded in `stepToAction` at `internal/head/agent/execute_phases_steps.go:17`). So a single case cannot orchestrate peers at **different endpoints** (different host or path) — e.g., a `web` role on `svc-a` and a `bridge` role on `svc-b` relaying within one scenario.

## Goal

Let a single `ws_connect` step dial an explicit URL, falling back to `tc.Target` when unset. This unlocks cross-endpoint multi-party relay in one case with no change to the connection table, namespacing (`<caseID>:<connectionID>`), read pump, or await/peer-join semantics (all already multi-connection-ready).

## Design

1. **`agent.TestStep`** (`internal/head/agent/types.go`): add `URL string \`json:"url,omitempty"\``. ws_connect-only (send/receive/disconnect operate on an already-opened ConnectionID; no URL).
2. **`stepToAction`** (`internal/head/agent/execute_phases_steps.go`): the `ws_connect` branch builds `WSConnectAction{URL: ...}`. Use `s.URL` when non-empty, else `tc.Target`. No other step type changes.
3. **Downstream is unchanged**: `WebSocketExecutor.doConnect` already keys off `a.URL`; role/credential/header/subprotocol resolution and the `WSProtocolIndex` host-lookup already handle a per-step URL (each service declaring a protocol is indexed by its host). No `Service` field needed.
4. **Scout authoring**: `assemblePlan`'s `ws_connect` parsing (`internal/head/scout/assembly.go`) sets `TestStep.URL = llm.StrField(call, "url")`. The planning tool `ws_connect` schema (`internal/head/scout/tools.go` `planTools`) gains an optional `url` property. Prompt guidance: ws_connect accepts an optional `url` to dial a peer at a different endpoint than the case target.
5. **Deterministic relay** (`wsRelayCases`) is left as-is (single service, same URL) — cross-endpoint is an LLM-authored scenario; the capability is what's unlocked.

## Behavior

- `TestStep.URL == ""` (the common case, all existing relay steps): identical to today (dials `tc.Target`).
- `TestStep.URL != ""`: that step dials the given URL; its role/auth resolve via the protocol index keyed by that URL's host.

## Testing

- `stepToAction`: empty `s.URL` → action URL == `tc.Target`; non-empty `s.URL` → action URL == `s.URL`.
- One Steps case dialing TWO different httptest servers in one case (extend the `newWSRelayServer` pattern to two servers, assert both accept). Proves cross-endpoint multi-party.
- Scout `assemblePlan`: a `ws_connect` tool call with `url` populates `TestStep.URL`.
- `make check` EXIT 0, clean tree.

## Out of scope

- Per-step `credential_ref` override (role mechanism covers distinct-actor cases).
- Dynamic runtime URL params (`/ws/{userId}` with a generated id) — separate feature.
- Cross-service deterministic relay generation (capability unlock suffices for LLM authoring).
