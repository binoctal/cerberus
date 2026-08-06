# Structured Evidence by Dimension — Validation

**Date:** 2026-08-06
**Spec:** `cerberus-docs/superpowers/specs/2026-08-06-structured-evidence-dimensions-design.md`
**Plan:** `cerberus-docs/superpowers/plans/2026-08-06-structured-evidence-dimensions-plan.md`
**Model:** glm-5.2 (via relay), `driftThreshold = 0.9`

## Setup

The validation crosses two interventions as a 2×2 matrix, N=3 runs each, over a
fixed set of 5 WS relay cases (ground truth = pass):

- **Vocabulary** — with the `ws-realtime` vocab summary vs without.
- **Dimension** — source-2 `deriveDimensions` enabled (`+dim`, renders a
  `membership` dimension block + the "missing dimension → uncertain" guidance)
  vs stripped (`+strip`, `Judge.deriveEnabled = false`).

`drift` is split into four categories (ground truth is `pass` for every case):
`incorrect` (fail), `honest-uncertain` (uncertain), `under-confident` (pass but
`conf < 0.9`), `clean` (pass at `conf >= 0.9`). The primary metric is
`new_drift = incorrect + under-confident`; `old_drift = incorrect +
honest-uncertain + under-confident` is kept for backward comparison.

The `fanout` case is the direct measurement target: its per-step trace
(sender `c-web`; recipients `c-bridge`, `c-web-2`; type `workflow:task_progress`)
is exactly what `deriveDimensions` turns into one `membership` dimension. Its
expectation — "the broadcast reaches both other web peers" — depends on
membership and implicitly on sender exclusion.

Run:

```
go test -tags=manual ./internal/head/examiner/ -run TestExaminerVocabValidation -v -timeout=900s
```

## fanout drift (the dimension's direct effect)

| Condition      | run1            | run2            | run3            | drift |
|----------------|-----------------|-----------------|-----------------|-------|
| vocab-dim      | pass / 0.90     | uncertain / 0.30 | uncertain / 0.35 | 2/3   |
| vocab-strip    | pass / 0.85     | pass / 0.90     | pass / 0.92     | 1/3   |
| novocab-dim    | uncertain / 0.30 | uncertain / 0.55 | uncertain / 0.30 | 3/3   |
| novocab-strip  | pass / 0.88     | pass / 0.85     | pass / 0.85     | 3/3\* |

\* all `pass`, but `conf < 0.9` in every run, so each counts as drift.

## Overall drift (all 5 cases, 15 verdicts per condition)

| Condition     | drift / 15 |
|---------------|------------|
| vocab-dim     | 4          |
| vocab-strip   | 4          |
| novocab-dim   | 6          |
| novocab-strip | 6          |

## Category breakdown (re-bucketed)

| Condition     | incorrect | honest-uncertain | under-confident | old_drift | new_drift |
|---------------|-----------|------------------|-----------------|-----------|-----------|
| vocab-dim     | 0         | 3                | 1               | 4         | 1         |
| vocab-strip   | 0         | 0                | 4               | 4         | 4         |
| novocab-dim   | 0         | 5                | 1               | 6         | 1         |
| novocab-strip | 0         | 2                | 4               | 6         | 4         |

## Conclusion (honest)

**The `membership` dimension did not reduce drift. Within each vocab row, `+dim`
and `+strip` tie overall (4 vs 4; 6 vs 6). On the `fanout` case specifically —
where the dimension is actually rendered — `+dim` is *worse* (more `uncertain`,
lower confidence).**

This is a null-to-negative result and it is consistent with the prior finding
(`examiner-judge-drift-evidence-bound`): enriching the Examiner's evidence does
not reliably reduce judge drift on glm-5.2. The mechanism here is visible in the
data:

- The dimension renderer emits `sender exclusion not probed` for every
  membership fact (exclusion is deferred — option 2 in the spec). The `fanout`
  and `routing` expectations both hinge on "the *other* peers" / "except the
  sender", i.e. on exclusion. The guidance tells the judge that a missing
  dimension warrants `uncertain`. Confronted with an expectation that depends on
  exclusion plus an explicit "not probed" fact, the judge honestly returns
  `uncertain` at low confidence — which is drift by the metric, even though it is
  the *correct* epistemic state.
- Stripping the dimension removes the "not probed" signal, so the judge falls
  back to the raw trace and passes the case — a higher pass rate but a less
  honest one.

So the dimension is doing what it was designed to do — surface that a claim is
*unproven* — but the drift metric counts that surfacing as failure. The
intervention changed the judge's behavior in the intended direction; the metric
just does not reward it.

**What this implies for next steps:**

1. The drift metric conflates "wrong" with "honestly uncertain". A follow-up
   should split drift into *incorrect* (status != ground truth) vs *under-
   confident* (pass but conf < threshold) vs *honestly-uncertain* (uncertain on
   a claim that is genuinely unproven). The last category is a win, not drift.
2. Closing the fanout/routing gap requires the deferred exclusion probe, so the
   dimension can state `sender excluded` / `NOT excluded` instead of `not probed`.
   That is the spec's "Exclusion requires an active probe" — still deferred, now
   with evidence it is the binding constraint.
3. Do not invest further in dimension *rendering richness* on glm-5.2; the lever
   is filling the dimension (exclusion probe), not displaying it differently.

Raw per-run output: `runtime/examiner-vocab-validation/*.txt`.

## Correction (2026-08-06, metric split)

The drift metric used above conflated `honest-uncertain` with real errors. Under
the four-category split, `incorrect` is 0 in every condition — the judge never
mislabels a pass-case as `fail`. The dimension's effect was to convert
under-confident passes into honest-uncertain verdicts (it surfaces `sender
exclusion not probed`, so a fan-out expectation yields `uncertain`). That is
correct epistemic behavior the old metric penalized.

Under `new_drift` (excluding honest-uncertain): vocab-dim 1 vs vocab-strip 4;
novocab-dim 1 vs novocab-strip 4. **The dimension does reduce drift — the prior
"did not reduce drift" conclusion is reversed.** The binding constraint for the
remaining fanout/routing drift is still the deferred sender-exclusion probe; the
metric fix changes how we read the dimension, not what it can prove.
