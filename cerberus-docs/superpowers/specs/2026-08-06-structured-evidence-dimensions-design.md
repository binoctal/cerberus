# Structured Evidence by Dimension — Design (2026-08-06)

## Background

The 2026-08-06 Examiner vocab validation
(`cerberus-docs/technical/validation/2026-08-06-examiner-vocab-validation.md`)
found that judge drift is governed by **evidence sufficiency**, not type
vocabulary. The recurring drift was the `routing` case: its expectation
("every connected web peer except the sender receives the broadcast") cannot
be proven from a single `MatchedMessage`, so the judge is correctly uncertain
regardless of vocabulary.

This spec addresses the root cause the validation pointed at
(Follow-up #3): give the judge **structured evidence organized by the
dimensions an expectation can assert against**, so drift from
"expectation needs a fact the evidence never carried" becomes detectable
(judge returns uncertain) or resolvable (evidence carries the fact).

## Current state (verified from code)

- `types.ExecutorResult` interface (`internal/types/result_types.go:6`):
  `Success() / Duration() / Summary() / Evidence() EvidenceData`.
- `EvidenceData` is `{Type, Content, Encoding string}` — a single string blob,
  no dimensional structure.
- `examiner.buildEvidenceContext` (`internal/head/examiner/judge.go:147`)
  stringifies the result (HTTP body, WS matched/seen messages, step trace) into
  one context block. There is no notion of "what categories of fact does this
  evidence support claims about?"
- `TestCase.Expectation` is free-text prose; the judge compares prose against
  the string blob holistically, so any dimension absent from the blob is
  guessed at.

## Goal

Introduce a fixed set of **assertable dimensions**. Executors populate the
dimensions they actually observed; `buildEvidenceContext` renders them by
dimension; the judge prompt tells the LLM to map each expectation claim to a
dimension and to **return uncertain (not a guess) when the dimension the claim
needs has no evidence**. This is cross-protocol (any executor fills the same
dimensions) and cross-claim (one fixed dimension set covers count, membership,
ordering, value, presence).

Judging stays LLM-driven (option A from design) — no change to
`objectiveVerdict` logic, no change to expectation schema.

## Non-goals

- No change to `TestCase.Expectation` (still prose). No structured claim list.
- No change to `objectiveVerdict`'s fast path. Dimensional evidence informs the
  LLM judge; it does not enable new deterministic verdicts in this spec
  (reserved for a possible future spec).
- No requirement that every executor populate every dimension in this spec.
  WS membership is the target; other executors (HTTP/process) are incremental
  and may follow.
- No change to the Scout/Agent path — this is Examiner-side evidence shape.

## Design

### The five dimensions

A fixed, closed set. The renderer and the judge guidance only know these five,
which is what makes the mechanism protocol-agnostic:

| Dimension | Claim shape | WS example | HTTP example |
| --- | --- | --- | --- |
| `count` | "exactly N" / "at least N" | "3 peers received" | "2 redirects" |
| `membership` | set membership / fan-out / exclusion | "all web peers except sender" (`exclude_sender`) | "response has header X" |
| `ordering` | temporal/buffer order | "connect before send" | "auth precedes request" |
| `value` | field equality / shape | "payload.approved == true" | "status == 200" |
| `presence` | existence / absence | "no error frame observed" | "body has no error field" |

### Data carrier — extend `EvidenceData`

Add a `Dimensions` field to the existing `EvidenceData` (zero interface change;
`ExecutorResult.Evidence()` already returns `EvidenceData`):

```go
// internal/types/result_types.go
type EvidenceData struct {
    Type       string      `json:"type"`
    Content    string      `json:"content"`
    Encoding   string      `json:"encoding,omitempty"`
    Dimensions []Dimension `json:"dimensions,omitempty"`
}

// Dimension is one structured observation an executor recorded, classified by
// the assertable dimension it speaks to. It states a fact only; the judge
// decides whether the fact satisfies an expectation claim.
type Dimension struct {
    Kind     string `json:"kind"`     // count|membership|ordering|value|presence
    Label    string `json:"label"`    // human/LLM-readable, e.g. "broadcast recipients"
    Observed string `json:"observed"` // structured fact, e.g. "recipients=[c-web-2]; sender=c-web excluded"
}
```

Old results that do not populate `Dimensions` leave it nil → the renderer
emits no dimension block → byte-identical prompt → zero regression.

### Render — `buildEvidenceContext`

When `result.Evidence().Dimensions` is non-empty, append a section:

```
Structured Evidence (by dimension):
  [membership] broadcast recipients: recipients=[c-web-2]; sender=c-web excluded
  [count] matched messages: 1
  [presence] error frames: none observed
```

Implementation note: `buildEvidenceContext` currently switches on the concrete
result type to pull bodies. The dimension block is read off
`r.Result.Evidence().Dimensions` and rendered **before** the type-specific
section, so it is uniform across result types.

### Judge guidance — dynamic, only when dimensions exist

The guidance is prepended to the evidence context **only when the result
carries dimensions** (keeps the empty case byte-identical — consistent with the
2026-08-06 Examiner vocab-injection no-regression rule). Place it in
`buildJudgePrompt` (the method added in the prior Examiner vocab-injection
work), ahead of the evidence string:

> The evidence below is organized by **dimension**: count, membership,
> ordering, value, presence. Map each claim in the expectation to its matching
> dimension and check the observed fact. **If the expectation depends on a
> dimension for which no evidence is listed, return `uncertain` with low
> confidence — do not infer the outcome from unrelated evidence.**

This is the drift-reduction lever: the `routing` case either gets a
`membership` fact it can check (evidence sufficient → resolvable) or gets none
(evidence insufficient → `uncertain`, not a guessed pass).

### First population — WS membership (the routing-drift target)

The WS executor must record fan-out as a `membership` dimension so the
`routing`/`exclude_sender` claim becomes judgeable. Two implementation paths;
the plan picks based on what the ws_flow trace already carries:

1. **Post-hoc extraction (preferred, lower risk):** after a `ws_flow`/multi-
   step WS case finishes, derive membership from the existing per-step trace in
   `StepResult.Evidence` (each step already records its `ConnectionID` and
   matched message). For a broadcast type, the recipients are the connections
   whose `ws_receive` matched it; the sender is the connection that
   `ws_send`-ed it; exclusion is whether the sender appears among recipients.
   No change to the execution loop.
2. **Inline recording (fallback):** the WS executor records recipients directly
   at broadcast time, into `EvidenceData.Dimensions`. Higher fidelity but
   touches the execution hot path.

Feasibility for path 1 was confirmed against the trace shape: `StepResult.
Evidence` entries already carry per-step connection + matched-type data, which
is sufficient to derive recipients, sender, and exclusion for a broadcast type.

If path 1 turns out to lack enough signal to determine exclusion reliably at
implementation time, fall back to path 2 for the membership dimension only.

## Verification plan

Reuse the `2026-08-06-examiner-vocab-validation` harness
(`internal/head/examiner/vocab_validation_manual_test.go`):

1. **Add an evidence-sufficient routing case.** Its `StepResult` carries a
   `membership` dimension ("recipients=[c-web-2]; sender=c-web excluded") that
   *does* prove the exclude_sender claim.
2. Keep the existing evidence-insufficient routing case (single
   `MatchedMessage`, no dimensions) as the negative control.
3. Run with/without dimensions (dimension block present vs stripped), `N=3`.

Success criteria:
- **Sufficient case:** drift drops to 0 across runs (the `membership` fact
  lets the judge resolve it).
- **Insufficient case:** the judge returns `uncertain` (not a guessed pass) in
  ≥2 of 3 runs — the guidance makes the missing dimension visible rather than
  guessed. Today this case drifts arbitrarily.
- Non-WS / no-dimension results: prompt byte-identical (unit test asserts no
  dimension block and no guidance text when `Dimensions` is nil).

## Files (planned)

- `internal/types/result_types.go` — add `Dimension`, `EvidenceData.Dimensions`.
- `internal/head/examiner/judge.go` — render dimension block in
  `buildEvidenceContext`; prepend guidance in `buildJudgePrompt` when present.
- `internal/head/examiner/judge_test.go` — dimension rendering + guidance
  presence/absence tests.
- WS executor (path TBD at plan time): populate `membership` for broadcast
  types, via post-hoc extraction or inline recording.
- `internal/head/examiner/vocab_validation_manual_test.go` — add the
  evidence-sufficient routing case; add a dimension-strip condition.
- `cerberus-docs/technical/validation/2026-08-06-structured-evidence-validation.md`
  — results write-up.

## Risks

- **WS fan-out extraction fidelity (path 1):** if the trace does not record
  enough to distinguish sender exclusion, the plan escalates to path 2. This is
  scoped to the membership dimension; the mechanism itself does not depend on
  it.
- **Guidance wording tuning:** "return uncertain when a needed dimension is
  absent" could over-trigger (judge returns uncertain even when prose evidence
  suffices). Mitigation: the guidance only fires when dimensions exist, and the
  success criterion explicitly checks the no-dimension path stays byte-identical
  and that precise/vague cases still pass.
- **Scope creep into other executors:** resisted by Non-goal #3. WS membership
  is the target; HTTP/process dimension population is explicitly deferred.

## Scope priority (for the plan)

- **P0 (must):** `Dimension` type + `EvidenceData.Dimensions` +
  `buildEvidenceContext` rendering + `buildJudgePrompt` guidance (gated on
  presence). Cross-protocol mechanism, zero-regression. Unit-tested.
- **P1 (target):** WS `membership` population + verification showing the
  routing-drift drop. If extraction proves too large, P0 still ships the
  generic mechanism and P1 becomes a follow-up.
- **P2 (deferred):** HTTP `value`, process `presence`, etc.
