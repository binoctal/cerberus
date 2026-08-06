# Drift Metric Split — Design

**Date:** 2026-08-06
**Status:** Spec
**Predecessor:** `2026-08-06-structured-evidence-dimensions-design.md`

## Problem

The Examiner validation harness measures judge drift as:

```go
drift := vr.Status != StatusPass || vr.CorrectnessConfidence < 0.9
```

This single boolean conflates three epistemically distinct outcomes (ground
truth is `pass` for every validation case):

1. **incorrect** — judge says `fail` (a real error).
2. **honest-uncertain** — judge says `uncertain` because the claim is genuinely
   unproven from the available evidence (e.g. sender exclusion is not probed).
3. **under-confident** — judge says `pass` but below the confidence threshold.

The conflation invalidated the structured-evidence validation. The `membership`
dimension makes the judge *more honest* (it surfaces `sender exclusion not
probed`, so a fan-out expectation yields `uncertain`), but the metric penalizes
that honesty the same as a real error, so the dimension showed no benefit.

## Evidence (retrospective re-categorization of existing data)

Re-bucketing the 18 runs already on disk (`runtime/examiner-vocab-validation/`):

| Condition     | incorrect | honest-unc | under-conf | OLD drift | NEW drift (excl honest) |
|---------------|-----------|------------|------------|-----------|-------------------------|
| vocab-dim     | 0         | 3          | 1          | 4         | **1**                   |
| vocab-strip   | 0         | 0          | 4          | 4         | 4                       |
| novocab-dim   | 0         | 5          | 1          | 6         | **1**                   |
| novocab-strip | 0         | 2          | 4          | 6         | 4                       |

Two findings:

- **`incorrect` is 0 in every condition** — the judge never mislabels a
  pass-case as `fail`. All drift is `uncertain` or under-confident `pass`.
- Under the split metric, **the dimension shows a clear benefit** (1 vs 4 in both
  vocab rows). The prior "dimension did not reduce drift" conclusion is reversed.

## Design

**Scope:** the validation harness's statistics layer only. No change to the
examiner product code, the dimension/guidance logic, or the judge prompt.

### Closed category set

```go
// classifyDrift sorts one judge verdict into one of four drift categories.
// Ground truth is pass for every validation case, so fail is the only incorrect
// verdict; uncertain is treated as honest (exclusion-gated claims are genuinely
// unproven until an active probe lands), and a low-confidence pass is
// under-confident.
func classifyDrift(status JudgeStatus, conf, threshold float64) string {
    switch {
    case status == StatusFail:
        return "incorrect"
    case status == StatusUncertain:
        return "honest-uncertain"
    case status == StatusPass && conf < threshold:
        return "under-confident"
    default:
        return "clean"
    }
}
```

The category set is closed: `incorrect | honest-uncertain | under-confident |
clean`. Every verdict maps to exactly one.

### Reporting

Every drift number is reported as a breakdown, not a single boolean. Two drift
totals are computed side by side:

- `old_drift = incorrect + honest-uncertain + under-confident` — preserves the
  original definition for backward comparison.
- `new_drift = incorrect + under-confident` — the primary metric, excluding
  honest-uncertain.

Per-case output gains a category column; per-run and overall summaries print the
four counts plus both drift totals.

### Honest-uncertain treatment

The asymmetry between the two penalized categories is intentional. An
under-confident `pass` is an *unreliable correct* — the judge landed on the
right verdict but cannot justify it, so the next run may flip it; that
instability is exactly what drift measures. An honest-uncertain verdict is a
*reliable unknowable* — the judge correctly recognizes the claim is unproven,
which is the right call when evidence is genuinely missing. Penalizing it would
punish the judge for honesty we asked the dimension/guidance to produce.

All `uncertain` verdicts are classified honest. This is correct for the current
case set (exclusion is genuinely not probed). It will stop being fully correct
once the deferred sender-exclusion probe lands — then an `uncertain` on a case
whose exclusion *was* probed is no longer "honestly" uncertain. That refinement
(per-case evidence-sufficiency annotation, "option B") is explicitly deferred
and noted as a follow-up; it is not needed to rescue the current conclusions.

## Files

- **Create** `internal/head/examiner/drift_classify_test.go` — defines
  `classifyDrift` AND its table test in one `_test.go` file. Living in a test
  file keeps this validation-only helper out of the production binary, while
  still compiling under every `make test` (no `//go:build manual` tag, so the
  unit test runs by default). The manual validation test is the same
  `package examiner`, so it calls `classifyDrift` directly under `-tags=manual`.
  One case per category plus the `conf == threshold` boundary (clean, since the
  check is strict `<`).
- **Modify** `internal/head/examiner/vocab_validation_manual_test.go` — replace
  the inline `drift` boolean with `classifyDrift`; accumulate four counters;
  update the per-case line, per-run summary, and overall `summary.txt` formats.
- **Modify** `cerberus-docs/technical/validation/2026-08-06-structured-evidence-validation.md`
  — change the drift definition to the four-category split; **add a correction
  section** stating the prior "dimension did not reduce drift" conclusion is
  reversed under the split metric; keep the original data table, re-read under
  the new metric.
- **Modify** `cerberus-docs/technical/validation/2026-08-06-examiner-vocab-validation.md`
  — change the drift definition to the four-category split and **re-evaluate**:
  the retrospective data (`with-vocab`: 2 honest-uncertain + 1 under-confident →
  new drift 1; `without-vocab`: 0 honest-uncertain + 3 under-confident → new
  drift 3) suggests the prior "vocab shows no benefit" conclusion may also
  reverse under the split metric. Rewrite the conclusion from the re-bucketed
  numbers rather than assuming it is unaffected.

## Testing

- `classifyDrift` table test: one row returning each of the four categories,
  covering boundary (`conf == threshold` is clean, not under-confident).
- The manual validation test is itself the integration run; no new LLM-touching
  test is added. Format changes are verified by a dry parse of the output
  strings (the retrospective script already proves the categorization logic).

## Out of scope

- Sender-exclusion active probe (separate feature; the binding constraint once
  the metric is honest).
- Per-case evidence-sufficiency annotation ("option B" classification) — deferred
  to when exclusion is probeable.
- Changing the drift threshold (0.9) or run count (3).

## Follow-up

Re-run the manual validation under the split metric to confirm the retrospective
numbers reproduce live (the existing raw data already proves the logic, but a
fresh run stamps the result with the new reporting format).
