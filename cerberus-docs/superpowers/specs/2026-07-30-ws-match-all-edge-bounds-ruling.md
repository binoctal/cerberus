# WS Match-All: Pushback Lifecycle & Empty-Batch Bounds (ruling)

> Status: ruling spec, 2026-07-30. Hardened-review follow-up covering two
> boundary questions left open after the match-all decisive receive
> (`2026-07-30-ws-batching-match-all-design.md`). Both resolve to "current
> behavior is correct"; this spec records the reasoning and pins the behavior.

## Q1 — Pushback slot cleanup on pump exit (done/closed)

`readMatchingAll` ends a burst by pushing the terminating non-matching frame
back into `entry.pending` (`hasPending=true`) so the next consumer receives it.
What happens to that slot if the pump exits (connection dies) before the next
read?

**Ruling: no cleanup needed; the slot is correct by construction.**

- `pending`/`hasPending` are plain struct fields mutated only under `entry.readMu`
  (set inside `readMatchingAll`, consumed by `popBuffered`, both readMu-held). No
  channel, goroutine, or OS resource is held — there is nothing to leak.
- **Delivery priority:** `popBuffered` checks `hasPending` BEFORE the channel and
  before any `<-entry.done` select. So a pushed-back frame is delivered to the
  next consumer even when the pump has already exited (`done` already closed).
  This is correct: the frame really did arrive before the peer closed; the dead
  connection only affects receives AFTER the pushed-back frame is drained.
- If no further consumer ever reads, the entry (and its `pending` field) is
  reclaimed when the connection is torn down on ctx-cancel / `doDisconnect`.

Pinned by `TestReadMatching_PendingSurvivesPumpExit` (unit: pending set, `done`
already closed, `readMatching` still returns the pending frame as "matched").

## Q2 — Empty batch: item-type receive semantics

`expandBatch` returns the batch frame UNEXPANDED when `items_path` is missing,
non-array, or an EMPTY array (`extractArray` returns `([], true)` for `[]`, and
`len(items)==0` degrades to pass-through). So an empty batch
`{"type":"event-batch","payload":{"events":[]}}` reaches consumers as a single
`event-batch` frame — it does NOT decompose into zero `event` items.

A `ws_receive` of the item type `event` (match-all or not) therefore never sees a
matching frame → it **times out**. For match-all specifically this means an empty
batch yields a `"timeout"` FAILURE, not a vacuous "all zero items passed" success.

**Ruling: this is the correct, verdict-safe behavior — do not change it.**

- An empty batch means "zero items of this type this turn." A receive that awaits
  the item type correctly finds nothing and times out.
- The alternative — treating match-all over zero items as a vacuous pass — would
  be a false pass: the Examiner would report "every item satisfied P" when no
  item existed. Failing loud (timeout) is the safe direction for a judging tool.
- An author who wants to detect the empty-batch SIGNAL should receive the BATCH
  type (`event-batch`), which still matches the passed-through frame.

Pinned by `TestWSExpandBatch_EmptyArrayPassesThrough` (unit: the decomposition
boundary) and `TestWSReceiveMatchAll_EmptyBatchIsTimeoutNotVacuousPass`
(end-to-end: an empty-only stream fails the match-all receive, not passes it).
