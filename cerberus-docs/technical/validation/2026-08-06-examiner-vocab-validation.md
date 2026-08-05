# Examiner Vocabulary Injection — Validation Result (2026-08-06)

## Setup
- Config: `dogfood/ws-realtime/.cerberus` (real open-agents vocab, rendered to a 1346-char summary).
- Judge: `examiner.NewJudge(driver, nil, ExaminerConfig{ConfThreshold: 0.9, VocabSummary: <with|empty>})`.
- Model: `glm-5.2[1m]` via GLM relay (`https://open.bigmodel.cn/api/anthropic`, bearer) — credentials from `.claude/settings.json` env.
- Fixed case set (`buildValidationCases`): 4 WS relay `StepResult`s, ground truth = pass. Evidence is synthetic but uses real protocol types and the exact frame format `buildEvidenceContext` surfaces. Expectations range from precise to vague.
- `N=3` runs per condition, drift threshold 0.9 (`drift = Status != pass OR CorrectnessConfidence < 0.9`).
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

**No — not as stated.** The spec's criterion was "with-vocab drift rate strictly lower than without-vocab across the 3 runs." Both conditions drift `1/4` on every run, and the single drift is always the same case (`routing`). Vocabulary injection did not reduce judge drift on this case set.

## Why — drift is evidence-limited, not type-limited

The `routing` case expects "every connected web peer except the sender receives the broadcast," but its evidence is a single `MatchedMessage`. That cannot prove fan-out or sender-exclusion, so a careful judge is correctly uncertain/low-confidence — **regardless of whether it knows the legal type set.** Vocabulary cannot repair insufficient evidence.

The `vague` case ("web should get the running task update pushed to it") is where vocabulary was expected to help most. It passed confidently under **both** conditions: `glm-5.2` infers from the `workflow:task_progress` type name in the evidence alone, without needing the vocab summary to tell it the type is legal. On a strong model with content-shaped evidence, the type knowledge is redundant.

## Interpretation

This is a different regime from the Scout validation (2026-08-05), where vocabulary was **decisive** — it converted abstract prose into concrete, protocol-faithful choreography (0 → 11–16 typed messages). The Examiner judge sits downstream: by the time it sees evidence, the types are already concrete in the frame bodies. Its drift is governed by **evidence sufficiency**, not by type vocabulary.

So the Examiner vocab injection is a **defensive, zero-regression** improvement (confirmed: empty summary → byte-identical prompt; non-WS unaffected), not a drift-reducer on this model. It would be expected to help most with (a) weaker models that cannot infer type legality from names, and (b) expectations that ask the judge to distinguish a legal-but-unobserved type from an invented one — neither of which this case set exercised.

## Conclusion

1. **Implementation is sound and zero-regression.** The wiring works end-to-end (vocab renders → config → judge prompt), confirmed by the prompt-injection unit test and this live run.
2. **Drift reduction claim not supported by this data.** On `glm-5.2`, judge drift is dominated by evidence sufficiency, not type knowledge. Keeping the injection is still justified (defensive anchor, zero cost for non-WS), but it should not be credited with drift reduction.
3. **Follow-up — the real drift lever is evidence, not vocabulary.** The `routing`/`exclude_sender` drift recurs because a single matched message cannot prove multi-peer fan-out. The next improvement for judge accuracy on relay cases is richer evidence (e.g. surfacing observed-vs-expected peer set, or a dedicated `ws_flow` summary that records sender-exclusion), not more prompt context.

## Follow-up (extractor noise re-measured)
The Scout validation's follow-up — scan only `target`/`expectation`/`steps` (this PR's Task 5) — is in place and was used by the Scout manual test path. This Examiner run does not use `scanFields` (its evidence is synthetic, not a plan dump), so it is unaffected either way.
