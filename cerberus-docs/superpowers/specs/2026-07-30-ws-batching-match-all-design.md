# WS Batch "Match All Items" Decisive Mode (design / spec)

> Status: design spec, 2026-07-30. Batching Phase 2 — the only Phase 2 item with
> real verdict-integrity value. Phase 1 (pump decomposition) shipped at 92ea82d;
> this closes the assertion-strength gap it left open.

## Problem

Phase 1 decomposes one batch frame into N synthetic item frames in the read pump.
A `ws_receive` of the item type matches the FIRST item frame and stops
(`readMatching` returns on first match); `checkAsserts` runs against that one
frame. So a contract "every item in the batch satisfies P" is asserted only for
item #1. A batch where item #1 is correct but item #3 is malformed passes — a
false pass for the Examiner.

This cannot be worked around by authoring N receives: the author does not know N
at authoring time, and N is variable per run. Universal quantification over a
batch is exactly the missing capability.

## Current state (confirmed)

- `readMatching` (`websocket.go:238`) acquires `entry.readMu`, consumes frames
  until one matches (non-matching frames are PERMANENTLY moved into `seen` — no
  pushback), returns on first match / timeout / closed.
- `doReceive` (`websocket.go:759`) calls `readMatching` once, runs `checkAsserts`
  against the single matched frame, first-failure fails the receive.
- `WSReceiveAction` (`actions_http.go:233`): `Type`, `Aliases`, `Assert`,
  `Timeout`, `Decisive`. No multi-match field.
- `WSResult` (`result_ws.go:36`): `MatchedMessage` (single), `SeenMessages`,
  `Messages`. No multi-match evidence.

## Design rulings

### R1. New field: `WSReceiveAction.MatchAll` (json `match_all`)

When true, the receive collects ALL matching frames in the arrival burst, not
just the first. `Assert` is evaluated against EACH matched frame; the receive
passes only if every frame satisfies every assert (first failing item fails the
receive, reporting the 1-based item index). Without `Assert`, match-all is
arrival-only per item (collects + counts; all-arrival = pass).

### R2. Burst semantics: consecutive matching frames under a grace window

Match-all collects matching frames that arrive within a tight burst:

1. Block on the FIRST matching frame (up to `Timeout`) — same as today. Timeout
   before any match ⇒ `timeout` (unchanged semantics).
2. After the first match, drain matching frames NON-BLOCKING (already-buffered
   by the pump's atomic push of the batch).
3. When the non-blocking drain finds no more buffered matching frame, wait a
   short GRACE window (`matchAllGrace`, ~10ms) for one more frame:
   - matching frame ⇒ append, re-loop (drain non-blocking again).
   - non-matching frame ⇒ PUSHBACK (re-buffer onto the entry) and STOP — the
     burst is over; the non-match belongs to the next consumer.
   - grace timer fires ⇒ STOP (burst complete).

The grace covers the pump atomic-push race: a batch's N items are pushed in one
tight loop inside `readPump`, but the consumer may grab item #1 mid-push and see
an instantaneously-empty channel before #2 lands. ~10ms amply covers a
sub-millisecond push. The grace ALSO defines the burst boundary for back-to-back
same-type batches (a gap > grace ends the burst).

Trade-off accepted: every match-all receive pays one grace (~10ms) after the last
item. Acceptable for a batch receive.

### R3. Pushback: per-entry pending slot

Add `wsEntry.pending wsMsg; hasPending bool`. A non-matching frame encountered
during the match-all burst is stashed here instead of being lost to `seen`.
`readMatching` and `readMatchingAll` BOTH check `pending` first (under `readMu`,
so no race). This is the single new piece of connection state; it is consumed
before any channel read, preserving frame order.

### R4. Result evidence: `WSResult.MatchedMessages` + `MatchedCount`

Add `MatchedMessages []string` (json `matched_messages`) and `MatchedCount int`
(json `matched_count`). On match-all success: `MatchedMessages` = all matched
item frames rendered; `MatchedCount` = len. `MatchedMessage` keeps the FIRST
matched frame for back-compat readers. On match-all assert failure at item i:
`MatchedMessage` = the FAILING frame, `Err` names the 1-based index, the other
matched frames go into `MatchedMessages` as evidence.

`Summary()` reports `matched=<MatchedCount>` (falls back to 1 when only
`MatchedMessage` is set, preserving non-match-all behavior). `Evidence()`
includes `MatchedMessages`.

### R5. Scope: framing

Match-all is defined for ALL framings (json/text/binary) by reusing
`matchAnyType`. It is primarily useful for json batch items (decomposed by the
pump). Under text/binary it means "all consecutive frames equal to Type within
the burst" — niche but free, no special-casing. No validation restriction.

### R6. Decisive interaction

`MatchAll` is orthogonal to `Decisive`. A decisive match-all receive passes the
case only when ALL items satisfy asserts. A non-decisive match-all collects but
does not gate the case (evidence-gathering). The Steps runner sets `Decisive`
exactly as today; `MatchAll` is LLM-authored per receive.

## Validation

- `WSReceiveAction.Validate`: no new hard constraint (match_all is a bool; the
  json-framing-only restriction does NOT apply — see R5). Keep minimal.

## Phased plan

- **This feature (Phase 2 #2):** `MatchAll` field + `readMatchingAll` (pushback +
  grace) + per-item asserts + `MatchedMessages`/`MatchedCount` + `Summary`/
  `Evidence` updates + prompt registry note for the Steer LLM + tests (all-items
  pass, one-bad-item fail verified RED, non-match pushback preserved for next
  receive, grace not needed when all pre-buffered, back-compat non-match-all
  unchanged).

## Open questions (decided at impl)

- **Grace value:** constant `matchAllGrace = 10 * time.Millisecond`. Documented,
  not configurable (no third knob).
- **Assert-fail evidence:** failing frame in `MatchedMessage`, index in `Err`,
  rest in `MatchedMessages`. Decided in R4.

## Why this and not the other Phase 2 items

- Configurable item envelope (#1): low value — the fixed
  `{"type":..,"payload":<item>}` envelope already lets asserts reach any sub-field
  via `payload.<path>` (or `payload` itself for scalar array elements). A sub-path
  knob adds surface for no new capability.
- Text-framed newline batches (#3): niche; no current target needs it. Defer.
- Match-all (#2, this spec): closes a real false-pass hole in the Examiner's core
  job (judging targets) and cannot be substituted by authoring. Highest value.
