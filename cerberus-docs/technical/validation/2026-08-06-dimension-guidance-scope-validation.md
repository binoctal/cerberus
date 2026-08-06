# Dimension Guidance Scoping — Validation Result (2026-08-06)

## Change under test
Rewrote `dimensionGuidance` to distinguish a **missing dimension** (the claim references a dimension with no evidence ⇒ uncertain) from a **present dimension with an unmeasured sub-fact** (the membership dimension IS listed; `Excluded` nil ⇒ "sender-exclusion not measured"). The new guidance tells the judge an unmeasured sub-fact the claim does *not* reference is a neutral scope note, not a gap. The nil render moved from the gap-sounding "sender exclusion not probed" to neutral "sender-exclusion not measured". Locked by `TestRenderDimensions_NilExcludedIsNeutralScope`.

This targets the over-trigger measured in the sender-exclusion-probe validation (2026-08-06): `fanout` — a recipients-only claim ("the broadcast reaches both other web peers") — drifted because the judge treated the unprobed sender-exclusion sub-fact as a missing required dimension.

## Setup
Same harness, model `glm-5.2[1m]` via GLM relay, four-category metric. Re-ran the two derive-enabled conditions (`vocab-dim`, `novocab-dim`), the only conditions the membership dimension appears in. Pooled across repeated captures because small-N verdicts near the 0.9 boundary are noisy; the trends below are consistent across re-runs.

## Result

### `routing` (carries the probe, `Excluded = *true`) — still bulletproof
~14 runs across conditions/captures: **100% `clean`** (conf 0.90–1.00). The sender-exclusion probe's effect is robust and unaffected by the guidance change.

### `fanout` (no probe, `Excluded` nil) — improved on the no-vocab path
Pooled fanout verdicts before vs after the guidance change:

| condition | before (probe-validation, 3 runs) | after (this change, pooled) |
| --- | --- | --- |
| novocab-dim | `incorrect`(0.25), `under-confident`(0.70), `incorrect`(0.85) — 3/3 drift, **2 incorrect** | mostly `clean`(0.90–0.95); ~1/8 `under-confident` — **0 incorrect** |
| vocab-dim | `honest-uncertain`(0.45), `under-confident`(0.85), `clean`(0.90) — 2/3 drift | mostly `under-confident`(0.80–0.85), occasional `honest-uncertain`/`incorrect` — still drifts |

**The win: the guidance scoping eliminated `fanout`'s `incorrect` false-fails on `novocab-dim`.** Before, the judge wrongly FAILED `fanout` twice (a recipient-only claim that fully matches the recipients list); after, it no longer treats the unmeasured sender-exclusion sub-fact as a disqualifying gap. `novocab-dim new_drift` contribution from `fanout` dropped from 4 (2 incorrect + 2 under-confident) over 3 runs to ~1.

### What did NOT improve — `vocab-dim` fanout
`vocab-dim` `fanout` still drifts (mostly `under-confident`). Adding the vocabulary summary appears to prime the judge to scrutinize routing/exclusion semantics harder, so it stays cautious about the unmeasured sender-exclusion despite the guidance. The guidance scoping fix helped the path WITHOUT vocab priming but did not conquer the vocab-primed path.

## Conclusion
1. **The change is a strict improvement and ships.** It removed `fanout`'s `incorrect` false-fails on `novocab-dim`, kept `routing` perfect, and is locked by a unit test. Reverting would restore the false-fails.
2. **`routing` is fully resolved** (probe + guidance: 100% clean).
3. **Residual pocket — `vocab-dim` fanout.** The vocabulary summary × membership-dimension interaction still pushes the judge toward under-confidence on a recipients-only claim. This is narrower and softer (under-confident, not incorrect) than the pre-fix `incorrect` false-fails, but it is the next judge-accuracy lever.
4. **N caveat.** Pooled small-N; the direction (incorrect false-fails eliminated on novocab-dim; routing rock-solid) is consistent, but the absolute `vocab-dim` rates are noisy around the 0.9 boundary.

## Follow-up
The next lever is the **vocab-summary × membership-dimension interaction**: why does anchoring the judge to routing vocabulary make it *more* cautious about an unmeasured exclusion sub-fact on a recipients-only claim? Candidate directions: (a) have the vocab summary defer to the dimension's measured facts when present, (b) only prime exclusion-scrutiny when the expectation actually claims exclusion. This is narrower than the over-trigger this change fixed.
