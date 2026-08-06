# Examiner Vocabulary Injection — Validation Result (2026-08-06)

## Setup
- Config: `dogfood/ws-realtime/.cerberus` (real open-agents vocab, rendered to a 1346-char summary).
- Judge: `examiner.NewJudge(driver, nil, ExaminerConfig{ConfThreshold: 0.9, VocabSummary: <with|empty>})`.
- Model: `glm-5.2[1m]` via GLM relay (`https://open.bigmodel.cn/api/anthropic`, bearer) — credentials from `.claude/settings.json` env.
- Fixed case set (`buildValidationCases`): 4 WS relay `StepResult`s, ground truth = pass. Evidence is synthetic but uses real protocol types and the exact frame format `buildEvidenceContext` surfaces. Expectations range from precise to vague.
- `N=3` runs per condition, drift threshold 0.9. `drift` is reported as four
  categories: `incorrect` (fail), `honest-uncertain` (uncertain), `under-confident`
  (pass but `conf < 0.9`), `clean`. Primary metric `new_drift = incorrect +
  under-confident`; `old_drift` (all non-clean) kept for comparison.
- Total wall-clock: 117 s (~19 s per run).

## Drift summary

| condition | run | drift / 4 | drift case(s) |
| --- | --- | --- | --- |
| with-vocab | 1 | 1 | routing (uncertain, 0.40) |
| with-vocab | 2 | 1 | routing (uncertain, 0.50) |
| with-vocab | 3 | 1 | routing (pass, 0.60) |
| without-vocab | 1 | 1 | routing (pass, 0.70) |
| without-vocab | 2 | 1 | routing (pass, 0.60) |
| without-vocab | 3 | 1 | routing (pass, 0.75) |

Per-case confidence (representative):

| case | with-vocab (run1) | without-vocab (run1) |
| --- | --- | --- |
| precise  | pass 0.97 | pass 0.98 |
| vague    | pass 0.95 | pass 0.95 |
| routing  | uncertain 0.40 | pass 0.70 |
| lifecycle| pass 1.00 | pass 0.95 |

## Did it meet the success criterion?

**Yes under `new_drift`; tied under `old_drift`.** The spec's criterion was "with-vocab drift rate strictly lower than without-vocab across the 3 runs." Under the old all-non-clean metric the two conditions tie at `3/12` (every run drifts `1/4`, always the `routing` case). Under the four-category split, `new_drift = incorrect + under-confident` is `1/12` with vocab vs `3/12` without — so vocab does reduce the primary drift metric on this case set, driven by the `routing` case shifting from `under-confident` (without vocab) to `honest-uncertain` (with vocab). See Conclusion for the caveat that this is a single-case signal at `N=3`.

## Why — drift is evidence-limited, not type-limited

The `routing` case expects "every connected web peer except the sender receives the broadcast," but its evidence is a single `MatchedMessage`. That cannot prove fan-out or sender-exclusion, so a careful judge is correctly uncertain/low-confidence — **regardless of whether it knows the legal type set.** Vocabulary cannot repair insufficient evidence; what it shifts on this case is *how* that uncertainty is expressed — `honest-uncertain` (`uncertain`) with vocab vs `under-confident` (sub-0.9 `pass`) without — which is exactly why it moves `new_drift` but not `old_drift`.

The `vague` case ("web should get the running task update pushed to it") is where vocabulary was expected to help most. It passed confidently under **both** conditions: `glm-5.2` infers from the `workflow:task_progress` type name in the evidence alone, without needing the vocab summary to tell it the type is legal. On a strong model with content-shaped evidence, the type knowledge is redundant.

## Interpretation

This is a different regime from the Scout validation (2026-08-05), where vocabulary was **decisive** — it converted abstract prose into concrete, protocol-faithful choreography (0 → 11–16 typed messages). The Examiner judge sits downstream: by the time it sees evidence, the types are already concrete in the frame bodies. Its drift is governed by **evidence sufficiency**, not by type vocabulary.

So the Examiner vocab injection is a **defensive, zero-regression** improvement (confirmed: empty summary → byte-identical prompt; non-WS unaffected). It is **not** a drift-reducer under `old_drift` (3/12 tie) but **does** reduce `new_drift` (3/12 → 1/12) by shifting the `routing` case from `under-confident` to `honest-uncertain` — see Conclusion. It would be expected to help most with (a) weaker models that cannot infer type legality from names, and (b) expectations that ask the judge to distinguish a legal-but-unobserved type from an invented one — neither of which this case set exercised.

## Conclusion

Re-bucketed under the four-category split (`N=12` per condition):

| condition | incorrect | honest-uncertain | under-confident | old_drift | new_drift |
| --- | --- | --- | --- | --- | --- |
| with-vocab | 0 | 2 | 1 | 3 | 1 |
| without-vocab | 0 | 0 | 3 | 3 | 3 |

1. **Implementation is sound and zero-regression.** The wiring works end-to-end (vocab renders → config → judge prompt), confirmed by the prompt-injection unit test and this live run.
2. **Under `new_drift`, vocab reduces drift on this case set — a reversal of the old-metric reading.** The recurring drift is the `routing` case. With vocab, `routing` lands as `honest-uncertain` on 2/3 runs (`uncertain` at conf 0.40 and 0.50) and `under-confident` on 1/3 (`pass` at 0.60). Without vocab, `routing` is `under-confident` on 3/3 (`pass` at 0.60–0.75) and never `honest-uncertain`. So `new_drift` is `1/12` with vocab vs `3/12` without — vocab converts two of the three routing drifts from masked low-confidence passes into explicit uncertainty, which the new metric does not count as drift. Under `old_drift` (all non-clean) the two conditions tie at `3/12`, which is what the previous "no benefit" reading reported.
3. **Caveats — this is a small, single-case signal.** `N=3` runs and every drift is the same case (`routing`), so the reduction is one status shift on one case kind, not a broad effect. The win is that vocab makes the judge more willing to *say* uncertain on evidence-insufficient relay cases, not that it makes the evidence sufficient.
4. **Follow-up — evidence richness remains the structural lever.** The `routing`/`exclude_sender` drift recurs because a single matched message cannot prove multi-peer fan-out; vocab changes how the judge expresses that gap, not whether the gap exists. The next improvement for judge accuracy on relay cases is richer evidence (e.g. surfacing observed-vs-expected peer set, or a dedicated `ws_flow` summary that records sender-exclusion).

## Follow-up (extractor noise re-measured)
The Scout validation's follow-up — scan only `target`/`expectation`/`steps` (this PR's Task 5) — is in place and was used by the Scout manual test path. This Examiner run does not use `scanFields` (its evidence is synthetic, not a plan dump), so it is unaffected either way.
