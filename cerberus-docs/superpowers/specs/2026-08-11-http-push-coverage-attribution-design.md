# HTTP-Push Coverage Attribution — Design

**Date:** 2026-08-11
**Branch:** `feat/http-push-coverage-attribution`
**Predecessor:** `feat/ws-http-broadcast-trigger` (merged 2026-08-11) — added the `http_request` step + `http_triggers` declaration + generator; the `device-restart` trigger case passes autonomously (`pass/0.98`) but `coverage_pct` stayed flat at 3/63.

## Problem

The `http_request` feature proved the HTTP→WS push capability (the `device-restart` case passes end-to-end), but the autonomous run's `coverage_pct` did **not** rise — it stayed at 3/63 (`gaps:61`). Root cause, verified by reading `internal/session/coverage.go`:

cerberus's path coverage is **receive-driven and already FromRole-agnostic**: `exercisedEdges` (coverage.go:192-225) credits a declared vocab edge `(From→Rr, T)` whenever a matched `ws_receive` of type `T` is observed by role `Rr` — the `(ToRole, Type)` lookup (`byToType`, coverage.go:194-198) does not consult `FromRole`. So an empty `FromRole` works on the **credit** side, and the rule already handles server-pushed edges (the `bridge→web device:online` push is credited this way today).

The gap is entirely in the **denominator**: `requiredEdges` (coverage.go:271-284) reads only `svc.Vocabulary.Edges` (filtered `Trigger=="message_handled"`). HTTP-triggered pushes are declared in `svc.Protocol.HTTPTriggers` (`{Effect.{MessageType,ToRole}}`), which `requiredEdges` never consults. So the `device-restart` case's `ws_receive device:restart` on the web connection produces valid evidence but has **no declared edge to credit** — `byToType["web|device:restart"]` is empty. The capability is proven; the metric is blind to it.

This is not solvable by choosing a different trigger: every reachable broadcast trigger (`device:restart`, `mission:decomposed`, …) is HTTP-emitted and hits the same blind spot, and no public route emits the one directed-`sendToBridge` type.

## Goal

Teach `requiredEdges` to synthesize a required edge per declared `HTTPTrigger` so the existing receive-driven attribution credits an HTTP-triggered push when its recipient receives it. The coverage metric then honestly reflects the push capability.

## Success Criteria

1. `requiredEdges` includes one synthesized edge per `svc.Protocol.HTTPTriggers` entry: `FromRole=""`, `ToRole=trigger.Effect.ToRole`, `Type=trigger.Effect.MessageType`, `Trigger="http_trigger"`.
2. A matched `ws_receive` of an HTTP-trigger's `MessageType` by its `ToRole` credits the synthesized edge; `pathCoverage.Pct` rises accordingly.
3. The per-edge gap detail renders an empty `FromRole` readably (e.g. `server→web device:restart not exercised`).
4. An autonomous `cerberus run` against the `ws-realtime` dogfood reports an honest `coverage_pct` change from the prior 3/63 baseline (expected 4/64 once the `device-restart` case runs), recorded verbatim from the run.
5. Zero regression: a service without `http_triggers` produces a byte-identical `requiredEdges` output and identical coverage; the receive-driven attribution rule (locked by `TestExercisedEdges_PushProtocolReceiveDriven`) is unchanged.

## Decisions

### D1 — Synthesize from `Protocol.HTTPTriggers` as a second source (no filter change)

`requiredEdges` keeps its vocab loop (`Vocabulary.Edges`, filtered `message_handled && !Unsupported && !Partial`) unchanged and gains a second loop over `svc.Protocol.HTTPTriggers` that appends a synthesized `VocabEdge` per trigger. The synthesized edges are appended directly — they do NOT pass through the vocab `message_handled` filter, so **no filter relaxation is needed**. The `Trigger:"http_trigger"` value is a label distinguishing HTTP-push edges from WS-relayed edges in the gap detail (consistent with the repo's "separate semantically distinct quantities" precedent, e.g. the drift-metric split), not a filter key.

Rejected: reusing `Trigger:"message_handled"` for synthesized edges (conflates WS-relayed and HTTP-pushed edges in the denominator; cannot distinguish them in gap output).

### D2 — Empty `FromRole` models the system origin; display renders it

The synthesized edge carries `FromRole=""`, matching the `VocabEdge` docstring's stated legality of a "DO-spontaneous null" sender (vocabulary.go:32-34). The `edgeKey` (`"|web|device:restart"`) is a legal unique identity, and `byToType` lookup is FromRole-agnostic, so credit flows. The gap-detail string (coverage.go:66-68) currently formats `%s→%s`; a small render helper maps an empty `FromRole` to the label `"server"` so the gap reads `server→web device:restart not exercised` rather than emitting an empty segment.

### D3 — Scope boundary: requires an existing WS vocab

`requiredEdges` and the gap loop only run under `sessionHasVocab(sess)` (coverage.go:60), which is true for the dogfood (it declares a 63-edge WS vocab). v1 folds HTTP-push edges into the existing path-coverage surface and does **not** broaden `sessionHasVocab` to count `HTTPTriggers`. A service declaring `http_triggers` but NO WS vocab would still route to line coverage; broadening `sessionHasVocab` is out of scope (no current dogfood needs it). Recorded as an honest boundary.

## Architecture

```
requiredEdges(sess):
  ① svc.Vocabulary.Edges  → filter message_handled && !Unsupported && !Partial  (unchanged)
  ② svc.Protocol.HTTPTriggers → synthesize {FromRole:"", ToRole, Type, Trigger:"http_trigger"}  (NEW)
  ⇒ required (denominator)

exercisedEdges (UNCHANGED, FromRole-agnostic):
  matched ws_receive of T by Rr ⇒ credit every declared edge (anyFrom→Rr, T)

pathCoverage (unchanged): Pct = |exercised ∩ required| / |required|
gap detail (coverage.go:66): render empty FromRole as "server"
```

## Components

| Unit | Change |
|---|---|
| `requiredEdges` (`internal/session/coverage.go:271`) | Add a loop over `svc.Protocol.HTTPTriggers` synthesizing one `project.VocabEdge` per trigger (`FromRole=""`, `ToRole=tr.Effect.ToRole`, `Type=tr.Effect.MessageType`, `Trigger="http_trigger"`). Append to the same `out` slice. Guard `svc.Protocol != nil`. |
| Gap-detail render (`internal/session/coverage.go:66`) | Replace the inline `fmt.Sprintf("edge %s→%s %s ...", e.FromRole, ...)` with a helper that maps an empty `FromRole` to `"server"`. |
| `originLabel(e project.VocabEdge) string` (new helper) | `e.FromRole`, or `"server"` when empty. |

## Error Handling

- A service with `svc.Protocol == nil` or no `HTTPTriggers` ⇒ no synthesized edges (zero-regression). `validateProtocolHTTPTriggers` already guarantees `ToRole`/`MessageType` are non-empty and reference declared roles, so synthesized edges are well-formed — no defensive checks needed in `requiredEdges`.
- An HTTP-trigger edge synthesized into `required` but never received ⇒ appears as an informational `Kind:"path"` gap (`server→<to> <type> not exercised`), consistent with unexercised WS edges. It does NOT feed the coverage repair loop (gaps are `Kind:"path"`, not `"coverage"`).

## Testing

1. **Unit — `requiredEdges`:** a service with one `HTTPTrigger` yields a synthesized edge in the result (`FromRole=""`, `Trigger="http_trigger"`, correct `ToRole`/`Type`); a service with no `http_triggers` yields a byte-identical result to today (zero regression); a nil `Protocol` is safe.
2. **Unit — `exercisedEdges` + `pathCoverage`:** with the synthesized edge in `required`, a matched `ws_receive device:restart` on the web connection credits it and `Pct > 0` including that edge; without the receive, the edge is unexercised and `Pct` excludes it. Confirm the empty-`FromRole` `edgeKey` does not collide with a same-`(ToRole,Type)` WS edge (distinct keys).
3. **Unit — gap render:** an unexercised synthesized edge produces `server→web device:restart not exercised`.
4. **Autonomous live proof:** `cerberus run` against `dogfood/ws-realtime`; record the verbatim `coverage assessment` line (expected `coverage_pct` rises from 0.0476/3-of-63 to 4/64 once the `device-restart` case runs) and confirm the `device-restart` gap (if unrun) or its absence (if run) is labeled `server→web`.

### Honest expected outcome

With one declared trigger, the denominator grows by one (63 → 64) and the numerator grows by one when the case runs (3 → 4): **4/64 = 0.0625**. If the case does not run (e.g. budget/auth), the denominator still grows to 64 and coverage dips to 3/64 — also honest. The exact number is recorded from the run, not asserted.

## Out of Scope

- Broadening `sessionHasVocab` to count `HTTPTriggers` (D3) — no current dogfood needs it.
- Declaring HTTP-push edges in the vocab FILE or teaching the extractor — synthesized from the protocol declaration only (clean two-source separation: WS edges from vocab, HTTP-push edges from protocol).
- The directed `sendToBridge` push class (`multiagent:task_assign`) — unreachable from any public HTTP route.
- Additional triggers (mission/orchestrator) — the synthesis generalizes to them; only `device-restart` is verified here.

## File Structure

- `internal/session/coverage.go` — `requiredEdges` synthesis loop; `originLabel` helper; gap-detail render call site.
- `internal/session/coverage_test.go` — `requiredEdges` synthesis test; `exercisedEdges`/`pathCoverage` credit test; gap-render test.
- `cerberus-docs/technical/validation/2026-08-07-openagents-integration-test-report.md` — append the autonomous http-push coverage re-verification.
