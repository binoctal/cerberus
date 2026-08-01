# `protocol infer` Grounded Literals — Design Spec

> Companion to `2026-08-01-protocol-infer-n-sample-voting-design.md`. N-sample
> voting raised the floor (no false negatives, stable `items_path`) but did not
> fix verbatim value precision: the handshake `await_type` is still the wrong
> existing literal (`device:online` vs the source's `devices:sync`). This spec
> attacks that residual gap.

## Problem

The 2026-08-01 N-sample dogfood (Runs 9–13) confirmed voting's limits. Across
five runs, every handshake that appeared carried a hallucinated or wrong
`await_type`. Inspecting `open-agents` `room.ts` shows *why* verbatim cueing
failed: the connect handler has **two** candidate "post-connect sends" and the
hard literal lives in a separate method:

- `room.ts:183` — bridge connect path emits `device:online` (unconditional, in
  the connect handler).
- `room.ts:209-213` — web connect path calls `sendOnlineDevices`, which under
  `if (onlineDevices.length > 0)` sends `type: 'devices:sync'` (the guarded,
  peer-gated handshake — the correct `await_type`).

A single-shot reader grabs the more visible `device:online`. This is "picks the
wrong **existing** literal," not invention — so source-existence checks alone
will not fully fix it, but grounding substantially raises the bar (see §Why).

The batching literals are analogous: `case 'session:output'` (342) routes to
`batchOutput`, which `setTimeout`-flushes via `flushBatch` as
`type: 'session:output-batch'` with payload array `lines` (490-500).

## Goal

Force the model to **ground** the hard literals — handshake `await_type` and the
batch flush block — in verbatim source quotes, and reject drafts whose quotes
are not actually present in the input. This converts "wrong/hallucinated
literal" into "sample dropped by validation" so voting keeps only grounded
drafts. Single LLM turn, no new orchestration, composes with voting.

## Approach (chosen)

**Citation + source-existence validation.** Extend `protocol_draft` so the hard
fields carry a verbatim `source` quote; add `validateGrounding` to confirm each
quote is a substring of the input file contents. Ungrounded samples become
`outcomeFailed` and are dropped by voting.

Rejected alternatives:
- **Two sequential calls (locate → read):** more robust against wrong-existing-
  literal, but doubles cost/latency and the locate call can itself mis-locate.
  Deferred as the escalation if this spec's approach proves insufficient.
- **Few-shot:** the dogfood already showed pure cueing is insufficient.

## Why this catches the failure modes

- **Invented/paraphrased literals:** the cited `source` snippet is not found in
  the inputs → sample dropped. Deterministic, no LLM judgement in the check.
- **Wrong existing literal (`device:online` vs `devices:sync`):** the handshake
  `source` is required to contain the **guard condition and the `type:`
  literal together**. The unconditional `device:online` send has no guard, so a
  model that has chosen it cannot produce a guard-bearing snippet — it is nudged
  toward the guarded `devices:sync`. This is not a guarantee (it is still
  single-shot recognition) but it materially raises the bar.
- **Honest degradation:** if every sample that emits a handshake fails to ground
  it, the selected draft simply has no handshake (voting picks the highest-
  scoring grounded draft). "No handshake" is preferable to a wrong `await_type`.

## Non-goals

- No change to `project.Protocol` — `source` quotes are inference-time evidence,
  never assembled into the protocol and never written to disk.
- No second LLM call.
- No citation requirement for `framing`, `type_path`, `roles`, `auth` — these
  are reliably correct in the dogfood and not worth the burden.
- No citation requirement for batch `item_type` — it is usually correct; the
  flush-block `source` (which carries the flush key + array field) is required,
  and voting covers `item_type`.

## Architecture

### Tool schema change (`protocol_draft`)

The handshake sub-object and each batch sub-object each gain a `source` string:

- `handshake.source` — "Verbatim source snippet proving `await_type`. MUST
  include the guard condition (e.g. `onlineDevices.length > 0`) AND the emitted
  `type: '<await_type>'` literal, copied exactly from the source."
- `batches.<key>.source` — "Verbatim snippet of the flush emit — the block that
  sends/types the batch routing key and contains the payload array field."

`source` is described (not JSON-schema `required`, which the hand-written map
does not enforce at nested depth); enforcement is in `validateGrounding`
(presence of a handshake/batch without a `source` → ungrounded → dropped).

### `validateGrounding(input map[string]any, inputs []SourceFile) error`

Pure function operating on the **raw tool input map** (before `argsToProtocol`,
so `Protocol` never sees `source`):

1. Join all `inputs` contents into one string (the corpus).
2. For each role in `input["roles"]` that has a `handshake`:
   - If `handshake.source` is missing/empty → error "handshake await_type
     ungrounded (no source quote)".
   - Else if `!strings.Contains(corpus, source)` → error "handshake await_type
     ungrounded (source quote not found in inputs)".
3. For each batch in `input["batches"]`:
   - If `source` missing/empty → error "batch <key> ungrounded (no source quote)".
   - Else if not found in corpus → error "batch <key> ungrounded (source quote
     not found in inputs)".
4. Return nil if all present citations are found.

The error names only the field and the failure mode; it never includes the raw
`source` value (which could be large) or any model payload — leak-safe.

> Whitespace sensitivity: the model must copy the snippet verbatim, so the
> substring match is exact. The prompt instructs a contiguous, copy-pasted
> snippet (not a rephrasing). If real-world copy fidelity proves poor, a
> normalizing matcher (collapse runs of whitespace) is a deferred refinement;
> v1 uses exact `strings.Contains`.

### `inferOnce` flow (one new step)

```
DecideWithTools  → err : outcomeFailed/infra
0 tool calls     : outcomeFailed/drift
!found           : outcomeNotFound
argsToProtocol   → err : outcomeFailed/parse
ValidateProtocol → err : outcomeFailed/invalid
validateGrounding→ err : outcomeFailed/ungrounded   ← NEW
otherwise        : outcomeFound
```

New `failReason`: `reasonUngrounded = "ungrounded"`.

### Prompt change (`buildInferPrompt`)

- Handshake instruction: after the existing "set await_type to the EXACT type:
  literal" guidance, add: "You MUST quote the verbatim source snippet in
  handshake.source — include both the guard condition and the `type:` line. A
  snippet not found verbatim in the source is rejected."
- Batch instruction: add "You MUST quote the verbatim flush-emit block in
  source (the block that types the batch routing key and holds the payload
  array)."

## Interaction with voting

`reasonUngrounded` samples join the failed tail. Voting then selects the
highest-scoring **grounded** Found sample. If the only handshake-bearing samples
are all ungrounded, the winner is a handshake-less draft — the honest outcome.
The all-failed aggregation already carried by `summarizeFailures` covers
unanimous-ungrounded runs (e.g. "3 ungrounded").

## Testing

TDD throughout; `make check` EXIT 0 + clean tree after every task.

- **`validateGrounding`** (table-driven, pure):
  - handshake `source` present in corpus → nil.
  - handshake `source` absent from corpus → error naming the field.
  - handshake present, `source` missing → error.
  - batch `source` checked (present / absent / missing).
  - no handshake/batches → nil (nothing to ground).
- **`inferOnce`**: a draft whose handshake `source` is not in the inputs →
  `outcomeFailed` / `reasonUngrounded`. A grounded draft → `outcomeFound`.
- **`Infer` voting**: one sample grounded-handshake + one ungrounded → the
  grounded one wins (or, if scoring favours it, a no-handshake grounded draft);
  assert the returned handshake (when present) is the grounded literal.
- **Tool schema**: handshake and batch sub-schemas expose `source`.
- **Leak guard**: the ungrounded error message contains no raw `source` text and
  no model payload; not `ErrNoProtocol`.

## File structure

- **Modify** `internal/protocoldiscover/tools.go` — handshake + batch `source`
  in the `protocol_draft` schema.
- **Modify** `internal/protocoldiscover/tools_test.go` — schema exposes `source`.
- **Modify** `internal/protocoldiscover/infer.go` — `validateGrounding`,
  `reasonUngrounded`, the new `inferOnce` step, `buildInferPrompt` citation
  guidance.
- **Modify** `internal/protocoldiscover/infer_test.go` — grounding + voting-
  with-grounding tests.
- **New** `cerberus-docs/superpowers/specs/2026-08-01-protocol-infer-grounding-design.md`
  (this file).
- **Append** `cerberus-docs/technical/dogfood/2026-07-31-m3-3-protocol-infer-dogfood.md`
  — grounded Run 14+ section: did `devices:sync` land?

## Out of scope / follow-ups

- Two sequential calls (locate → read) — the escalation if grounding citation
  does not crack `devices:sync`.
- Normalizing snippet matcher (whitespace-tolerant) — only if exact-match
  rejection rate is high in practice.
- Grounding `item_type`, `framing`, `type_path` — not needed.
