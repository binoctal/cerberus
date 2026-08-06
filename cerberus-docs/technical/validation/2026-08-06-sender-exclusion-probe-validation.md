# Sender-Exclusion Probe — Validation Result (2026-08-06)

## Setup
- Same harness as the 2026-08-06 examiner-vocab validation: `examiner.NewJudge(driver, nil, ExaminerConfig{ConfThreshold: 0.9, VocabSummary: <with|empty>})`, model `glm-5.2[1m]` via the GLM relay, four-category `classifyDrift` metric.
- The `routing` case now carries a real fan-out trace WITH a sender negative-probe (`ExpectAbsent`), so `deriveDimensions` yields a membership dimension with a measured `Excluded = *true` (sender timed out, did not receive its own broadcast). Before this change `routing` had no trace and no probe → `Excluded` nil → "sender exclusion not probed".
- This re-measurement ran the two derive-enabled conditions (`vocab-dim`, `novocab-dim`), N=3 each = 6 runs (the probe only matters when dimension derivation is on). The strip conditions are unaffected by the probe and were not re-run here. Total wall-clock: 196 s (~33 s per run).

## Headline — the probe resolved the recurring `routing` drift

The `routing` case was the single recurring drift in every prior validation, always landing `honest-uncertain` (with vocab) or `under-confident` (without vocab). With the probe it now resolves to a confident verdict:

| condition | run | routing verdict | conf | category |
| --- | --- | --- | --- | --- |
| vocab-dim | 1 | pass | 0.95 | clean |
| vocab-dim | 2 | pass | 0.90 | clean |
| vocab-dim | 3 | pass | 0.90 | clean |
| novocab-dim | 1 | pass | 0.90 | clean |
| novocab-dim | 2 | pass | 0.90 | clean |
| novocab-dim | 3 | pass | 0.85 | under-confident |

Across 6 runs: **5 clean, 1 under-confident, 0 honest-uncertain, 0 incorrect.** The `Excluded = *true` fact gave the judge something concrete to confirm instead of an absence to hedge over. The probe did what the spec promised.

## Controlled comparison — `routing` vs `fanout` isolates the probe

`routing` and `fanout` carry an **identical** fan-out trace (sender `c-web` sends `workflow:task_progress`; `c-bridge` and `c-web-2` receive it). The only difference is the sender negative-probe: `routing` has it (`Excluded = *true`), `fanout` does not (`Excluded` nil → "sender exclusion not probed"). Their verdicts diverge cleanly:

| condition | run | routing | fanout |
| --- | --- | --- | --- |
| vocab-dim | 1 | clean (0.95) | honest-uncertain (0.45) |
| vocab-dim | 2 | clean (0.90) | under-confident (0.85) |
| vocab-dim | 3 | clean (0.90) | clean (0.90) |
| novocab-dim | 1 | clean (0.90) | **incorrect (0.25)** |
| novocab-dim | 2 | clean (0.90) | under-confident (0.70) |
| novocab-dim | 3 | under-confident (0.85) | **incorrect (0.85)** |

`routing` (probe) is clean or near-clean everywhere; `fanout` (no probe) drifts on 5/6 runs and goes `incorrect` twice under `novocab-dim`. Same trace, same recipients, same expectation shape — the probe is the variable that flips the verdict from uncertain/fail to confident pass.

## Where the drift went — `fanout`, and a caveat about `dimensionGuidance`

The drift did not disappear; it migrated from `routing` to `fanout`. Per-condition aggregates over the 15 cases (5 cases × 3 runs) of this re-measurement:

| condition | incorrect | honest-uncertain | under-confident | old_drift | new_drift |
| --- | --- | --- | --- | --- | --- |
| vocab-dim | 0 | 1 | 1 | 2 | 1 |
| novocab-dim | 2 | 0 | 2 | 4 | 4 |

`incorrect` stays 0 under `vocab-dim` and the `new_drift` totals are low — consistent with the prior corrected reading that vocab+dimensions keep the judge from mislabeling. The remaining drift is now concentrated on `fanout`.

**Caveat — the dimension guidance over-triggers.** `fanout`'s expectation is "the broadcast reaches both other web peers" — a claim about *recipients*, not about sender-exclusion. Yet the membership dimension renders "sender exclusion not probed", and `dimensionGuidance` instructs the judge to "return uncertain with low confidence" whenever an expectation depends on a dimension with no evidence. The judge, over-cautiously, treats the unprobed exclusion as a gap even though exclusion is not part of the claim. The probe fixed this for `routing` (Excluded is now a measured `*true`); `fanout` still drifts because its trace has no probe.

## Conclusion

1. **The sender-exclusion probe works as designed.** The recurring `routing` `honest-uncertain` drift is resolved: 5/6 clean, never honest-uncertain, never incorrect. The controlled `routing`-vs-`fanout` comparison (identical traces differing only by the probe) isolates the probe as the cause.
2. **Zero regression on the metric's integrity.** `incorrect` is 0 under `vocab-dim`; non-probe cases are unaffected.
3. **Follow-up lever — the drift migrated to `fanout`, exposing an over-broad `dimensionGuidance`.** "sender exclusion not probed" pushes the judge toward uncertainty even when the expectation makes no exclusion claim. Two candidate fixes for the next iteration: (a) only surface the "not probed" annotation when the expectation plausibly depends on sender-exclusion (expectation-shape gating), or (b) soften `dimensionGuidance` so an unprobed dimension that the claim does not reference does not trigger uncertainty. Either converts the remaining `fanout` drift the same way the probe converted `routing`.
4. **N caveat.** This is 6 runs across 2 conditions on one case set. The direction (probe → confident verdict) is consistent and causally isolated, but the absolute rates are small-N.

## Follow-up
The probe is shipped (`feat/sender-exclusion-probe`, merged to main). The next judge-accuracy lever is the `dimensionGuidance` over-trigger, not further probe work — the probe already covers every case where sender-exclusion is actually in the expectation.
