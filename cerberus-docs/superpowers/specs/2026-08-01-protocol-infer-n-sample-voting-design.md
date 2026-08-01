# `protocol infer` N-Sample Voting — Design Spec

> Companion to `2026-07-31-m3-3-protocol-infer-enhancement-design.md`. The
> M3-3 pipeline (T1–T4) is in place and dogfood-validated; this spec addresses
> the open quality risk that dogfood surfaced: run-to-run variance.

## Problem

The 2026-08-01 value-accuracy dogfood pass sampled four runs against the same
`open-agents` target and got four different shapes:

| Run | Outcome |
|---|---|
| 5 | full draft (correct `items_path`) |
| 6 | `found=false` — false negative (the input is a WS Durable Object) |
| 7 | partial draft (roles + auth only) |
| 8 | `could not parse model output` — malformed args |

Prompt copy edits have hit diminishing returns: `items_path` likely improved,
but `await_type` did not, and **variance** (1/4 false negative, 1/4 parse
failure) swamps both. The dogfood record concludes reliable value accuracy
needs an architecture change, not further wording. The cheapest such change is
N-sample voting: run the draft several times, discard the failure / false-
negative tails, and keep the strongest surviving draft.

## Goal

Absorb `protocol infer` run-to-run variance by running N drafts and selecting
the best, without changing the three-state error model or the underlying model.

- **Default N=3**, overridable by a new `--samples` flag.
- **Selection strategy: best-of-N by score** (not field-level merge). Rationale:
  merging structured maps (roles/batches) across runs risks combining
  hallucinated fields and is complex; picking the single strongest validated
  draft is simple, contradiction-free, and directly targets the observed failure
  mode (tails omit or mangle structures).

## Non-goals

- Field-level consensus merge / union of roles across runs.
- Parallel (concurrent) sampling. Sequential sampling keeps budget accounting
  and mock determinism simple; latency is acceptable for an interactive
  authoring aid. Parallelism is a deferred optimization.
- Two-step grounding extraction or few-shot. These remain open follow-ups;
  voting composes with either if added later.
- Any change to the `protocol_draft` tool schema, `argsToProtocol`, or
  `ValidateProtocol`.

## Architecture

Split the current single-shot `Infer` into a pure per-sample function and a
voting orchestrator.

```
inferOnce(ctx, driver, cfg, service, inputs) -> sample
Infer(ctx, driver, cfg, service, inputs, samples) -> (*project.Protocol, error)
```

### `sample` descriptor (unexported)

`inferOnce` classifies one LLM outcome into a category; it does not collapse
distinct outcomes into a single error the way the old `Infer` did, because the
voter must count categories across samples.

| Category | Trigger |
|---|---|
| `outcomeFound` | A populated, parsed, **validated** `*project.Protocol`. Carries the protocol and its precomputed score. |
| `outcomeNotFound` | The model explicitly returned `found=false` (or omitted `found`). |
| `outcomeFailed` | Drift (zero tool calls), parse error, validation error, OR `DecideWithTools` itself failed (budget exhausted, retries exhausted, ctx cancelled). Carries a non-leaking reason tag for diagnostics. |

`inferOnce` returns `sample` only (no error). Every adverse `DecideWithTools`
outcome — budget exhausted, retries exhausted, drift, parse failure, validation
failure — maps to `outcomeFailed`, so the voter keeps going. Systemic
cancellation is handled one level up: the voting loop checks `ctx.Err()`
between samples and short-circuits by returning it. This avoids fragile
error-type introspection inside `inferOnce`.

> Refined during design: a returned-error channel would require `inferOnce` to
> distinguish ctx cancellation from budget/retry exhaustion by error type.
> Instead `inferOnce` never returns an error; only the loop's `ctx.Err()` check
> short-circuits. Cleaner boundary, identical semantics.

### `Infer` aggregation rules

1. **≥1 `outcomeFound`** → return the highest-scoring Found sample. Ties broken
   by earliest sample index (deterministic given sequential order).
2. **0 Found, ≥1 NotFound** → return `ErrNoProtocol`. If every model attempt
   either found nothing or failed, the clean interpretation is "no protocol
   here", matching the existing `ErrNoProtocol` contract.
3. **0 Found, 0 NotFound (all Failed)** → hard error. A static message (no raw
   LLM payload leak), with reason counts in the message, e.g. `"protocol
   inference failed across all samples: 2 drift, 1 parse"`. NOT `ErrNoProtocol`.

This preserves the public contract: `Infer` still returns `(*Protocol, error)`
with the `ErrNoProtocol` sentinel; the command layer in `main_protocol.go`
needs no semantic change, only the new `samples` argument.

### `scoreProtocol` — deterministic scoring

The observed false-negative signature is **omission** — tail runs drop
structures. So the score rewards drafts that recognized more (and harder)
structures. It is a pure function of `*project.Protocol`:

- +1 if `TypePath != ""`
- +1 if `Auth != nil`
- +`len(Roles)`
- +`len(Batches)` × 2  (batching is a non-obvious structure; weight it)
- + (count of roles with a non-nil `Handshake`) × 2  (handshake is the hardest
  structure to recognize; weight it)
- consensus tie-break: +1 if `Framing == modalFraming`, +1 if `TypePath ==
  modalTypePath` (both are near-always-correct fields, so consensus only breaks
  ties, never overrides coverage).

The voter computes modal `Framing` and `TypePath` over the Found samples first,
then scores. Weights are constants, documented in-code by the motivation above.
The weighting is deliberately opinionated but simple; it is not tuned and need
not be — the dominant signal is "more structures beats fewer".

## Cost, budget, sequencing

- Samples run **sequentially** in a `for i := 0; i < samples; i++` loop.
- Each sample calls `driver.DecideWithTools`, which shares the driver's
  `TokenBudget`. `--samples N` multiplies token cost by up to N (tails that
  drift or parse-fail still spend input tokens). With the default budget
  (200000 total / 10000 per-call), N=3 is comfortable.
- If the budget exhausts mid-run, that sample's `DecideWithTools` errors →
  `outcomeFailed`; the loop continues. The run degrades gracefully rather than
  aborting.
- `samples < 1` clamps to 1 (single-shot, the legacy behaviour).

## Test infrastructure — `MockClient` response sequences

`llm.MockClient.Complete` is deterministic per prompt key today: it always
returns the same fixture. **This cannot represent variance**, so N-sample
voting cannot be tested against it as-is. Add an additive capability:

- `SetToolResponseSequence(key string, sequence [][]llm.ToolCall)` — for a
  matching key, return `sequence[i]` on the i-th `Complete` call, advancing a
  per-key counter; when exhausted, hold the last element.
- Backed by a `map[string][]ToolCall` sequence store + `map[string]int` counter.
- `SetToolResponse` (single stable fixture) is unchanged. A sequence takes
  precedence when present for the matched key (matches the "more specific wins"
  intuition). `matchKey` logic is untouched; the sequence is indexed after the
  key is resolved.

This is a small, self-contained change to `internal/llm/mock.go` with its own
unit test, independent of the voting logic.

## Testing

TDD throughout: write the failing test, run it RED, implement, run it GREEN,
commit. Every task ends with `make check` (fmt + lint + test) EXIT 0 and a clean
tree.

- **`mock`**: successive `Complete` calls with a sequence rotate through the
  fixtures and hold on the last; a key without a sequence still uses
  `SetToolResponse`.
- **`inferOnce`**: one case per category — found (validated protocol), not-found
  (`found=false`), drift (zero tool calls), parse error (malformed args),
  invalid (bad `credential_ref`), and a `DecideWithTools` error (budget/retries
  exhausted) → `outcomeFailed`. `inferOnce` returns no error.
- **`scoreProtocol`**: table-driven; a complete draft (roles + batches +
  handshake) outscores a partial one (roles only); consensus tie-break is
  exercised.
- **`Infer` voting** (sequence-backed mock):
  - all NotFound → `ErrNoProtocol`.
  - one Found + two failed tails → returns the Found.
  - complete draft + partial draft → returns the complete (higher score).
  - all Failed → hard error, NOT `ErrNoProtocol`, message carries reason counts.
  - false-negative absorption: one NotFound + one Found → returns the Found
    (the false negative does not poison the result).
  - `samples` clamping (`n<1` → single-shot).
- **`main_protocol`**: `--samples` defaults to 3 and is plumbed through to
  `Infer`; the existing `--dry-run` happy path still passes.

Negative-verification: a guard that the all-Failed error message does NOT
contain the raw tool args / LLM payload (leak protection retained).

## File structure

- **Modify** `internal/llm/mock.go` — `SetToolResponseSequence` + sequence store.
- **Modify** `internal/llm/mock_test.go` (or create) — sequence rotation test.
- **Modify** `internal/protocoldiscover/infer.go` — `sample` type, `inferOnce`,
  `scoreProtocol`, voting in `Infer`, new `samples` param + default constant.
- **Modify** `internal/protocoldiscover/infer_test.go` — migrate the three-state
  tests to `inferOnce`; add voting tests on the sequence mock.
- **Modify** `cmd/cerberus/main_protocol.go` — `--samples` flag (default 3) +
  `protocolInferOpts.Samples`.
- **Modify** `cmd/cerberus/main_protocol_test.go` — `--samples` plumbing test.
- **Append** `cerberus-docs/technical/dogfood/2026-07-31-m3-3-protocol-infer-dogfood.md`
  — N=3 Run 9+ section showing variance absorbed.

## Out of scope

Path params (`/ws/{userId}`), `await_type` verbatim accuracy, and two-step
grounding remain follow-ups. Voting raises the floor (the best of N is more
likely to carry the verbatim `devices:sync` than a single shot) but does not
guarantee it; that guarantee is the two-step follow-up's job.
