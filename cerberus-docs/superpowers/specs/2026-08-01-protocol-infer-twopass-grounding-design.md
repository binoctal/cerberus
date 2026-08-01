# `protocol infer` Two-Pass Grounding (Option B) — Design Spec

> Builds on `2026-08-01-protocol-infer-grounding-design.md`. Citation grounding
> (Variant A) works mechanically but hits the model's one-shot copy-fidelity
> ceiling: it substantively paraphrases cited blocks, so the exact-match check
> correctly rejects them and hard-structure samples rarely survive. This spec
> removes the copy burden from the model entirely.

## Problem

The handshake `await_type` is still not landed verbatim (`devices:sync`). The
2026-08-01 diagnostic showed the model picks the wrong **existing** literal
(`device:online`) and cannot verbatim-copy a multi-line block
(`payload:{sessionId,lines}` vs source `payload:{sessionId,lines:batch.lines,
timestamp:Date.now()}`). Grounding-as-exact-substring is therefore low-recall:
structure-bearing samples are usually rejected, so the voting winner is a
clean but structureless draft.

Root cause: the model is asked to simultaneously (a) select the right construct
among candidates, (b) verbatim-copy its block, and (c) transcribe the literal —
in one whole-file pass. It fails at (b), and the whole-file scope hurts (a).

## Goal

Land the verbatim handshake `await_type` (and batch flush details) by splitting
the work: pass 1 names candidate literals (easy, short strings); **code**
extracts anchored source windows for them (exact grep — no model copy burden);
pass 2 reads ONLY those small windows to select the guarded handshake and
transcribe its literal off the anchored text. Composes with N-sample voting.

## Approach (Variant B — Identify → Code-Extract → Verify)

Per sample, when the pass-1 draft contains a handshake or a batch:

1. **Pass 1** — unchanged `protocol_draft` call: produces the full draft
   (envelope, roles, auth) plus candidate handshake/batch literals. The
   handshake/batch `source` field becomes optional (no longer required; the
   copy burden moves to code).
2. **Code-extract** — for each candidate literal (handshake `await_type`, batch
   flush key), `strings.Contains`/index it in the joined corpus; if found,
   extract a window of ±N lines around the (first) match into a `window{literal,
   text}`. Literals not in the corpus are invented and yield no window.
3. **Pass 2** — a second `DecideWithTools` call with a new `confirm_signals`
   tool. The prompt contains ONLY the extracted windows (each tagged with its
   literal). The model reads them and returns whether a guarded post-connect
   handshake is present (and its `await_type`) and whether a flush emit is
   present (flush key, item_type, items_path). Reading a few focused lines is
   high-fidelity; selecting the guarded send among 2–3 windows is far easier
   than among 500 whole-file lines.
4. **Merge** — pass-2 confirmed values override pass-1's hard literals on the
   assembled `*project.Protocol`. A handshake confirmed absent (no guarded
   window) is removed. Unconfirmed/invented literals are dropped (honest
   omission over a wrong value).
5. **Post-merge guard** — confirmed literals must appear in the corpus (they
   were read off extracted windows, so this is a cheap invariant check, not a
   copy-fidelity gate).

### Why this lands `devices:sync`

Pass 2 receives windows for BOTH `device:online` (the connect-handler bridge
broadcast, unguarded) and `devices:sync` (the `sendOnlineDevices` send under
`if (onlineDevices.length > 0)`). "Which window shows a guarded send?" is a
local read off ~5 lines — reliable. The model returns `devices:sync`.

## Non-goals

- No change to envelope/roles/auth inference (already reliable).
- No change to voting, `selectProtocol`, or `scoreProtocol`.
- The old `validateGrounding` block-quote check is retired (replaced by
  code-extract + pass-2 confirmation). The `source` schema field stays but is
  optional and ignored.
- Not adding a third pass or cross-file grounding.

## Architecture

### New file `internal/protocoldiscover/twopass.go`

- `type signalWindow struct { literal, text string }`
- `func extractWindows(corpus string, literals []string, radius int) []signalWindow`
  — pure. For each literal, if `strings.Contains(corpus, literal)`, take the
  match and ±`radius` lines (split on `\n`, clamp at file bounds). De-dup by
  literal. Literals absent from the corpus produce nothing.
- `func confirmSignalsTool() llm.Tool` — hand-written schema mirroring
  `protocolDraftTool`'s style. Input:
  - `handshake`: `{present: bool, await_type: string}` — `present=false` if no
    window shows a guarded post-connect send.
  - `batch`: `{present: bool, flush_key: string, item_type: string, items_path: string}`.
- `func buildConfirmPrompt(windows []signalWindow) string` — instructs the model
  it is reading ANCHORED source windows only, asks which window is the guarded
  post-connect handshake and which is the timer-flush emit, and to call
  `confirm_signals` once.
- `type signalConfirmation struct { handshakePresent bool; awaitType string; batchPresent bool; flushKey, itemType, itemsPath string }`
- `func refineSignals(ctx, driver, draft *project.Protocol, inputs) (*signalConfirmation, error)`
  — gathers candidate literals from `draft` (the role handshake `AwaitType`;
  each batch's flush key = its map key), calls `extractWindows`, and if any
  windows exist runs pass 2 (`DecideWithTools([confirmSignalsTool])`), parsing
  the tool input into `signalConfirmation`. If no windows (no candidates found
  in corpus) returns a zero-value confirmation with both `present=false` (the
  draft's hard structures are invented → will be dropped).

### `infer.go` changes

`inferOnce` after `ValidateProtocol` and before classifying `outcomeFound`:
- If the draft has any handshake or batch, call `refineSignals`.
- **Merge** per `signalConfirmation`:
  - For each role with a handshake: if `handshakePresent && awaitType != "" &&
    strings.Contains(corpus, awaitType)` keep the handshake with that
    `AwaitType` (preserve `Timeout`/`Optional` from pass 1); else **drop** the
    role's handshake.
  - For batches: if `batchPresent` and `flushKey`/`itemType`/`itemsPath` are
    non-empty and present in corpus, set the (single) batch's fields from the
    confirmation (keyed by `flushKey`); else drop all batches.
- If `refineSignals` returns an error (e.g. pass-2 `DecideWithTools` infra
  failure, or zero tool calls) → treat as `outcomeFailed`/`reasonInfra` (or
  `reasonDrift`), letting voting continue with other samples. Pass-2 failure
  must NOT abort the whole run.
- Remove the prior `validateGrounding` call site (replaced by merge). Keep
  `reasonUngrounded` in the summary list for the migration's sake? No —
  `validateGrounding` and `reasonUngrounded` are removed; the block-quote
  grounding is retired. (Tests updated accordingly.)

> Note: this retires the citation-grounding code added earlier this session
> (schema `source` stays optional; `validateGrounding`/`reasonUngrounded`/
> `normalizeWS` removed; `inferOnce` gets the merge). The session's grounding
> experiment is recorded in the dogfood/spec docs.

### Prompt change

Pass 1 `buildInferPrompt`: drop the stern "MUST set source … rejected" sentences
(source no longer required); keep the token-slot steer (Fix A — still valuable).
Add: "For the handshake await_type and batch flush details, name your best
candidate literals; a second pass will verify them against the source."

### Composes with voting

Each of the N samples runs pass 1 (+ pass 2 when hard structures are present).
Voting then selects the highest-scoring confirmed draft as before. A sample
whose pass-2 dropped its handshake scores lower (no handshake bonus), so a
sample that confirmed a real handshake wins — exactly the desired preference.

## Cost

+1 LLM call per sample that has a handshake/batch (typically all of them), so
roughly 2× per run. Default N=3 → up to 6 calls. Within the existing 60k-token
session budget for open-agents. Latency ~2×; acceptable for an interactive aid.

## Testing (TDD)

- `extractWindows` (pure, table): literal present → window with ±radius lines;
  literal absent → no window; radius clamps at file bounds; multiple literals
  → multiple windows; de-dup.
- `confirmSignalsTool`: schema exposes handshake + batch with the expected
  fields.
- `buildConfirmPrompt`: contains the windows' text and the "guarded" steering.
- `refineSignals`: with a mock driver preset for a `confirm_signals` tool
  response, returns the parsed confirmation; no candidates in corpus → both
  `present=false`.
- `inferOnce` two-pass: a pass-1 draft with a wrong `await_type`
  (`device:online`) plus a pass-2 confirmation of `devices:sync` → the merged
  sample carries `devices:sync`. A pass-2 that says no guarded handshake →
  handshake dropped, sample still `outcomeFound`.
- Pass-2 infra failure → `outcomeFailed`/`reasonInfra` (voting continues).
- Voting: a sample that confirmed a grounded handshake outscores one without.
- Existing grounding tests (`validateGrounding*`, `TestInferOnce_Ungrounded*`,
  `TestInferOnce_Grounded*`) are removed/rewritten for the new flow.

## File structure

- **Create** `internal/protocoldiscover/twopass.go` — windows, tool, prompt,
  refine.
- **Create** `internal/protocoldiscover/twopass_test.go` — unit tests.
- **Modify** `internal/protocoldiscover/infer.go` — merge in `inferOnce`; retire
  `validateGrounding`/`normalizeWS`/`reasonUngrounded`; pass-1 prompt tweak.
- **Modify** `internal/protocoldiscover/infer_test.go` — replace grounding tests
  with two-pass tests.
- **Append** dogfood Run 22+ section: did `devices:sync` land?

## Out of scope / follow-ups

- Grounding `framing`/`type_path` (not needed).
- Cross-file windows (single-corpus join is enough here).
- If pass-2 selection still errs, a third "contrast" pass — deferred.
