# WS Match-All: Mid-Burst Terminator Semantics (ruling)

> Status: ruling spec, 2026-07-30. Hardened-review follow-up to the match-all
> decisive receive (see `2026-07-30-ws-batching-match-all-design.md`). Pins and
> documents the behavior of `readMatchingAll`'s three phase-2 burst terminators.

## Question

`readMatchingAll` phase 2 (the burst collector) ends its `select` on one of three
non-frame events after at least one matching frame has been collected:

```go
case <-grace.C:    // matchAllGrace (10ms) idle gap
    return matched, seen, "matched"
case <-timer.C:    // the receive's OVERALL deadline
    return matched, seen, "matched"   // <-- partial-as-pass?
case <-entry.done: // pump exit (peer close / ctx cancel)
    return matched, seen, "matched"
```

All three currently return `"matched"` (success). The concern raised in review:
when the **overall timeout** fires while frames are still expected, returning the
already-collected subset as a pass could mask a malformed item in the truncated
tail (`partial-as-pass`). Should this instead be a `"timeout"` failure?

## Ruling: keep as success (document, do not change to failure)

A match-all receive is a **success** when at least one matching frame arrived and
every collected frame satisfies the assert. After the first match, ALL three
phase-2 terminators are legitimate burst endings, not failures:

| Terminator | Meaning | Outcome |
|---|---|---|
| `<-grace.C` | 10ms idle gap: server paused, burst complete | success |
| `<-timer.C` | overall deadline reached WHILE BUFFER DRAINED (idle) | success |
| `<-entry.done` | peer closed mid-batch: "done sending" | success |

Only **phase 1** (zero matches before the first frame) returns `"timeout"` /
`"closed"` as a failure.

### Why overall-timeout mid-burst is also a success

1. **The grace window already bounds idle.** To reach the phase-2 `select` at all,
   the buffer must be empty (the non-blocking drain loop exited). So every phase-2
   terminator fires from an idle/drained state — semantically identical to grace.
2. **A live stream cannot be truncated here.** If frames keep arriving (inter-frame
   gap < 10ms), each arrival resets `grace` and the loop keeps collecting; the
   select is only re-entered when the buffer goes empty again. The overall timer
   therefore fires only when the buffer was drained at the deadline — i.e. "no more
   frames came within budget", the same signal grace encodes, just at a longer
   horizon.
3. **Failure semantics would contradict the feature.** Match-all exists because the
   item count is unknowable at authoring time. Making overall-timeout a failure
   would fail every large/slow batch even when correct — pure false-fail noise for
   the Examiner, the worse direction for a judging tool (false-pass is bounded to
   ≤ one grace window; false-fail is unbounded).

### Residual partial-as-pass window

Negligible by construction: because grace (10ms) terminates any idle burst, the
only frames a mid-burst timeout could miss are those arriving within the < 10ms
between the last collected frame and the deadline, AND only if the buffer was
drained at that instant. A tight batch (< 10ms total) completes before any
realistic timeout; a sparse batch (> 10ms gaps) is ended by grace first.

## Verified by

- `TestWSReceiveMatchAll_OverallTimeoutMidBurstPasses` — a server that streams
  matching frames faster than grace for longer than the receive deadline; the
  receive passes with `MatchedCount >= 2` (proves multiple frames were collected
  before the deadline, i.e. mid-burst), never reporting a timeout error.
- `TestWSReceiveMatchAll_PeerCloseMidBurstPasses` — server sends a few matching
  items then closes; the receive passes with the collected subset.
- Negative verification: flipping the `<-timer.C` case to return `"timeout"`
  makes the mid-burst test go RED.
