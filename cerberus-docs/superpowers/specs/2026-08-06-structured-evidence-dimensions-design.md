# Structured Evidence by Dimension — Design (2026-08-06)

> Revised after self-review to fix the exclusion-evidence gap, make
> `Dimension` genuinely structured, and state the LLM-soft-behavior limits
> honestly instead of assuming them away.

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
needs has no evidence**.

This is **cross-protocol by construction** (any executor fills the same
dimension struct; the renderer/judge core never changes per protocol). It is
cross-claim for the **common single-step assertion shapes** — see "Known
limitations" for what the five dimensions do not cover.

Judging stays LLM-driven (option A from design) — no change to
`objectiveVerdict` logic, no change to expectation schema.

## Non-goals

- No change to `TestCase.Expectation` (still prose). No structured claim list.
- No change to `objectiveVerdict`'s fast path. Dimensional evidence informs the
  LLM judge; it does not enable new deterministic verdicts in this spec.
- No requirement that every executor populate every dimension in this spec.
  WS membership is the target; other executors (HTTP/process) are incremental
  and may follow.
- No change to the Scout/Agent planning path — this is Examiner-side evidence
  shape. (`BuildEdgeSteps` may change to *collect* better evidence; that is
  execution-side, not planning-side.)

## Design

### The five dimensions

A fixed, closed set. The renderer and the judge guidance only know these five,
which is what makes the mechanism protocol-agnostic:

| Dimension | Claim shape | WS example | HTTP example |
| --- | --- | --- | --- |
| `count` | "exactly N" / "at least N" | "3 peers received" | "2 redirects" |
| `membership` | set membership / fan-out | "these connections received the broadcast" | "response has header X" |
| `ordering` | temporal/buffer order | "connect before send" | "auth precedes request" |
| `value` | field equality / shape | "payload.approved == true" | "status == 200" |
| `presence` | existence / absence | "no error frame observed" | "body has no error field" |

Coverage scope (honest): these cover **common single-step assertions**. They do
not cleanly cover composite/temporal assertions — idempotency ("second call has
no side effect"), state transition ("resource went A→B"), recovery ("failed
then retried successfully"). Such assertions are multi-step and would compose
several dimensions across steps; this spec does not claim to handle them, and
they remain prose-judged.

### Data carrier — extend `EvidenceData`, with genuinely structured fields

Add a `Dimensions` field to the existing `EvidenceData` (zero interface change;
`ExecutorResult.Evidence()` already returns `EvidenceData`). The `Dimension`
carries **typed facts**, not a prose blob — the original draft's `Observed
string` was pseudo-structured and would have just moved the semantic-parsing
problem into the judge. Only the fields relevant to `Kind` are populated:

```go
// internal/types/result_types.go
type EvidenceData struct {
    Type       string      `json:"type"`
    Content    string      `json:"content"`
    Encoding   string      `json:"encoding,omitempty"`
    Dimensions []Dimension `json:"dimensions,omitempty"`
}

// Dimension is one structured observation an executor recorded, classified by
// the assertable dimension it speaks to. Populate only the fields for its Kind.
// The judge decides whether the fact satisfies an expectation claim; the
// dimension never carries a verdict, only observed facts.
type Dimension struct {
    Kind  string `json:"kind"`            // count|membership|ordering|value|presence
    Label string `json:"label"`           // human/LLM-readable, e.g. "broadcast recipients"

    // Kind-specific facts (only the relevant set is populated):
    Recipients []string `json:"recipients,omitempty"` // membership: connections that received
    Sender     string   `json:"sender,omitempty"`     // membership: connection that sent
    // Excluded reports sender-exclusion ONLY when actively probed (a ws_receive
    // was set on the sender connection and timed out). nil when not probed —
    // see "Exclusion requires an active probe" below.
    Excluded *bool  `json:"excluded,omitempty"` // membership
    Count    int    `json:"count,omitempty"`    // count
    Value    string `json:"value,omitempty"`    // value: e.g. "status=200", "approved=true"
    Present  *bool  `json:"present,omitempty"`  // presence: true=observed, false=confirmed-absent
    Order    []string `json:"order,omitempty"`   // ordering: ordered list of events/ids

    // Note is a short free-text supplement, NOT the primary signal. Keep empty
    // when a typed field already carries the fact.
    Note string `json:"note,omitempty"`
}
```

Old results that do not populate `Dimensions` leave it nil → the renderer
emits no dimension block → byte-identical prompt → zero regression.

### Render — `buildEvidenceContext` (layered, no duplication)

The dimension block is read off `r.Result.Evidence().Dimensions` and rendered
as a **distinct layer** ahead of the existing type-specific section (HTTP body,
WS matched/seen messages, step trace). To avoid the duplicate-evidence problem
flagged in review, the type-specific section keeps rendering the raw frames
(what the judge cross-checks against), while the dimension block renders only
the **derived facts the raw section does not state** — e.g. for membership it
renders `recipients=[...]` and the sender, which the raw `MatchedMessage` list
does not name explicitly. If a fact is already obvious in the raw section
(e.g. `count` that equals the number of matched messages shown), the executor
should omit that dimension rather than restate it.

Empty dimension set ⇒ no block ⇒ byte-identical prompt.

### Judge guidance — dynamic, only when dimensions exist, and explicitly soft

The guidance is prepended to the evidence context **only when the result
carries dimensions** (keeps the empty case byte-identical — consistent with the
2026-08-06 Examiner vocab-injection no-regression rule). Place it in
`buildJudgePrompt` (the method added in the prior Examiner vocab-injection
work), ahead of the evidence string:

> The evidence below is organized by **dimension**: count, membership,
> ordering, value, presence. Map each claim in the expectation to its matching
> dimension and check the typed fact. **If the expectation depends on a
> dimension for which no evidence is listed, return `uncertain` with low
> confidence — do not infer the outcome from unrelated evidence.**

**This guidance is a soft prompt instruction, not a guarantee.** Whether the
LLM (a) correctly maps a prose claim to a dimension, (b) honors "missing →
uncertain", is itself the thing the verification must measure. The mechanism's
value is empirical, not assured — see "Known limitations".

### First population — WS membership, with exclusion handled honestly

The WS executor records fan-out as a `membership` dimension. **Recipients are
reliably derivable; exclusion is not, unless the choreography probes it.**

**Recipients (reliable):** after a `ws_flow`/multi-step WS case finishes,
derive from the existing per-step trace (`StepResult.Evidence` already records
each step's `ConnectionID` and matched type) the set of connections whose
`ws_receive` matched the broadcast type, plus the connection that `ws_send`-ed
it. No change to the execution loop. This is the post-hoc extraction path.

#### Exclusion requires an active probe

The original draft claimed sender-exclusion could be inferred from the trace.
That is false for the current choreography: `BuildEdgeSteps` (from the prior
 Examiner vocab-injection work) gives the sender connection (`c-web`) only a
`ws_receive(device:online)` — it never waits for the broadcast type on the
sender. So "sender not in recipients" just means **not observed**, not
**excluded by the DO**. Inferring exclusion would state a fact the evidence
cannot support.

Two honest options; the plan picks one:

1. **Probe exclusion (recommended):** extend `BuildEdgeSteps` so a
   web→web broadcast also sets a short-timeout `ws_receive(<broadcast type>)`
   on the sender connection. If it times out while a recipient observes the
   frame, `Excluded = false`-meaning-sender-was-excluded can be set
   *true* from observation. This makes the membership dimension's `Excluded`
   field a real observed fact. Touches `BuildEdgeSteps` (execution-side helper).
2. **Do not assert exclusion this spec:** populate only `Recipients` + `Sender`
   and leave `Excluded = nil`. The renderer explicitly states "sender exclusion
   not probed". Exclusion assertions stay prose-judged until a follow-up adds
   the probe. Lower risk, smaller scope.

If option 1's `BuildEdgeSteps` change proves to destabilize the integration
test, fall back to option 2 for this spec.

## Verification plan

Reuse the `2026-08-06-examiner-vocab-validation` harness
(`internal/head/examiner/vocab_validation_manual_test.go`):

1. **Add a fan-out case with a membership dimension** carrying
   `Recipients=[c-web-2]`, `Sender=c-web`. This proves the fan-out half of the
   routing claim (other peers received) — *not* the exclusion half unless
   option 1 is taken.
2. Keep the existing single-`MatchedMessage` (no dimensions) case as the
   negative control.
3. If option 1 (probe) is taken, add an exclusion case whose membership
   dimension has `Excluded=true` from a real probe.
4. Run with/without the dimension block (present vs stripped), `N=3`.

Success criteria (revised — significance, not zero):
- **Fan-out case:** drift (non-pass or confidence < 0.9) is **strictly lower**
  with the dimension block than without across the 3 runs. Not required to
  reach 0 — the LLM may still waver on wording; the bar is a measurable drop.
- **No-dimension case:** the prompt is byte-identical (unit test asserts no
  dimension block and no guidance text when `Dimensions` is nil). This is the
  hard no-regression gate.
- **Exclusion case (only if option 1):** drift strictly lower with the probed
  `Excluded=true` fact than without.

A null result (no measurable drop) is a valid, reportable outcome: it would
mean the glm-5.2 judge does not reliably honor the dimension guidance, and the
follow-up should target deterministic (objective-path) handling instead — the
opposite of over-claiming.

## Known limitations (stated honestly)

- **Claim→dimension mapping is LLM-performed.** Expectation stays prose, so
  "which dimension does this claim map to" is itself an LLM inference that can
  be wrong (e.g. mapping an exclusion claim to `count`). This is the cost of
  not structuring expectation (Non-goal #1). The mechanism reduces drift from
  *missing evidence*; it does not eliminate drift from *mis-mapped claims*.
- **The guidance is soft.** "Missing dimension → uncertain" depends on LLM
  compliance; verification measures it rather than assuming it.
- **Coverage is single-step assertions.** Composite/temporal assertions
  (idempotency, state transition, recovery) are not handled by five flat
  dimensions and remain prose-judged.
- **Exclusion is not free.** Without an active probe (option 1), membership can
  name recipients but cannot honestly state exclusion.

## Relationship to the Examiner vocab injection (2026-08-06)

Complementary, not redundant:
- **Vocab injection** constrains *type legality* (the judge knows which
  `namespace:action` types are real, guarding against invented-type drift). It
  showed little marginal value on glm-5.2 because that model infers legality
  from type names.
- **Structured dimensions** constrain *fact sufficiency* (the judge sees which
  assertable facts were observed, guarding against missing-evidence drift).
  This is the lever the validation actually pointed at.

Structured dimensions succeeding does not make vocab injection worthless, but
it is the more direct drift lever; if a future trim pass is needed, vocab
injection (zero-marginal-value on this model) is the candidate to demote, not
dimensions.

## Risks

- **WS fan-out extraction fidelity:** recipients are reliably derivable;
  exclusion needs option 1's probe. Scoped to the membership dimension; the
  generic mechanism does not depend on it.
- **Guidance over-triggering:** "missing dimension → uncertain" could make the
  judge return uncertain even when prose evidence suffices. Mitigation: guidance
  fires only when dimensions exist; the no-dimension path is byte-identical;
  verification checks precise/vague cases still pass.
- **Prompt growth from dimension block:** mitigated by the no-restatement rule
  (executors omit dimensions whose fact is already obvious in the raw section).

## Scope priority (for the plan)

- **P0 (must):** `Dimension` type + `EvidenceData.Dimensions` + layered
  `buildEvidenceContext` rendering + gated `buildJudgePrompt` guidance +
  no-regression unit tests. Cross-protocol mechanism. Zero-regression.
- **P1 (target):** WS `membership` (recipients, reliably) population + the
  fan-out verification case showing a drift drop. Exclusion via option 1 if it
  lands cleanly; else option 2 and defer.
- **P2 (deferred):** HTTP `value`, process `presence`, and the exclusion probe
  if option 2 is taken.
