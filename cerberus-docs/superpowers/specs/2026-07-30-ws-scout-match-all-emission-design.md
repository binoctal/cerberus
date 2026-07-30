# WS Scout: Auto-Emit match_all for Batch Item Receives (design)

> Status: design spec, 2026-07-30. Closes the Scout-side gap left by the match-all
> decisive receive (`2026-07-30-ws-batching-match-all-design.md`): hand-authored
> `ws_flow` cases could opt into `match_all`, but Scout's auto-generated exchange
> cases could not, so auto-generated coverage of a batch item type still asserted
> only item #1.

## Problem

`wsStepsCase` (Scout) emits a `connect → send → receive` Steps case from a goal's
client-sent → server-receive exchange. The receive step carried `Type` and derived
`Asserts` but never `MatchAll`. When the receive type is a declared batch's
`item_type`, the server may send a BATCH frame that the read pump decomposes into N
synthetic item frames; a first-match receive asserts only item #1 — the exact
false-pass hole `match_all` exists to close. Auto-generated cases were blind to it.

## Design

`wsStepsCase` now sets the receive step's `MatchAll` via `shouldMatchAllBatch`:

```go
MatchAll: shouldMatchAllBatch(svc.Protocol, ex.recvType, ex.asserts)
```

`shouldMatchAllBatch(proto, recvType, asserts) bool` returns **true** when:

1. `recvType` equals a declared batch's `ItemType` (`svc.Protocol.Batches[*].ItemType`).
   This is the signal that the server may send a batch of `recvType` frames. A
   nil/empty protocol, empty recvType, or no matching batch → `false` (a plain
   single-frame receive, unchanged behavior).

2. **AND** no assert key references a matching batch's `items_path` (the key equals
   the items_path or is a sub-path under it). Such an assert is BATCH-LEVEL, not
   per-item — applying it to each decomposed item would false-fail, so match_all is
   declined and the receive falls back to first-match.

Per-item payload keys (e.g. `payload.approved`) pass the guard: each decomposed
item is `{"type":..,"payload":<element>}`, so `payload.x` reaches into the element.
Arrival-only receives (nil asserts) over a batch item type also get `match_all`:
this confirms every item arrived and matched the type, still stronger than
first-match.

## Scope

Only the **goal-exchange** `wsStepsCase` (the case with a send→receive pair and
derived asserts). The no-exchange `connect + receives` loop is intentionally
untouched: those receives are arrival-only and frequently target handshake/relay
signal types, where collecting a burst has weaker value and changes semantics for
non-batch signals. `match_all` is an existing field already propagated through all
four reachability surfaces (TestStep, stepToAction, Steer prompt, executor doc —
see commit 075c879), so no wiring work was needed; Scout is now a new author of it.

## Verified by

- `TestShouldMatchAllBatch` — unit: the rule's six cases (match/per-item,
  match/arrival-only, non-batch, items_path-equal assert, items_path-sub assert,
  nil protocol).
- `TestWSCasesEmitsMatchAllForBatchItemType` — end-to-end: a batch item_type
  receive carries `MatchAll=true` (verified RED by dropping the call).
- `TestWSCasesDeclinesMatchAllForBatchLevelAssert` — the guard: an items_path
  assert declines match_all.
