# protocol-infer hard-structure recall fix — Design

- Date: 2026-08-02
- Scope: raise `protocol infer` recall on handshake/batch (and roles value
  precision) so the open-agents benchmark moves off 0%. Driven by the N=18
  benchmark result (`protocol-infer-benchmark-n18-result-2026-08-02`):
  handshake 0/18, batch_keys 3/18, roles 15/18.
- Prior conclusion ("model capability ceiling") is retracted — the cause is
  structural, not model-side.

## Root cause (structural)

Two-pass grounding — the only mechanism that reliably landed the verbatim
`devices:sync` — is gated on pass-1 already having emitted a hard structure:

- `internal/protocoldiscover/infer.go:129` — `if hasHardStructure(p)` guards the
  two-pass call. Pass-1 recall on handshake/batch is ~0, so the gate rarely
  opens and two-pass almost never runs.
- `internal/protocoldiscover/twopass.go:150-158` — candidate literals are drawn
  ONLY from the pass-1 draft. When pass-1 omits the structure, there are no
  literals, so even if two-pass ran it would have nothing to anchor on.

Net: a control-flow deadlock. The mechanism that can find hard structures
requires those structures to already be found. This is a pipeline gate, not a
model-capability limit.

Secondary defect: `mergeConfirmation` (`twopass.go:242-251`) only REWRITES
existing role handshakes; it cannot ADD a confirmed handshake to a role that
pass-1 left without one. So even a confirmed handshake would be dropped when
pass-1 omitted it.

## Fix

Four changes in `internal/protocoldiscover/{infer.go,twopass.go}`:

1. **Run two-pass unconditionally.** Remove the `hasHardStructure` gate in
   `inferOnce`; always call `refineSignals` then `mergeConfirmation`. Pass-2 is
   still the judge, so running it on a draft without hard structures is safe
   (it can only confirm absent → no-op, or add a confirmed structure).

2. **Code-seeded candidate literals.** Add `candidateLiterals(corpus string)
   []string`: a regex scan for routing-key-shaped string literals in the source
   (`'devices:sync'`, `'session:output-batch'` — pattern `'(\w+:[\w-]+)'` /
   double-quoted, deduped, capped). `refineSignals` unions these with the
   pass-1-named literals before extracting windows. Over-matching is fine: pass-2
   only confirms a window that shows a guarded send / timer flush, so noise is
   filtered. This is the locate half of locate→read, made unconditional and
   code-driven instead of gated on pass-1 emission.

3. **`mergeConfirmation` adds a confirmed handshake to the web role** when
   pass-1 emitted none. Currently it only rewrites existing role handshakes;
   extend it so a confirmed `handshakePresent` attaches to the role most likely
   to be the connect-side role (the `web` role when present, else the first
   role), creating the `Handshake` if absent.

4. **Roles value precision (both, per decision):**
   - Prompt cue in `buildInferPrompt`: a role's discriminator value must match
     its name (a `bridge` role carries `type: bridge`, not `web`).
   - Post-processing correction in the merge path: if a role literally named
     `bridge` has `params["type"] != "bridge"`, set it to `bridge` (and
     symmetrically `web`→`web`). Bounded to the open-agents role-name==value
   shape; defensive, idempotent, unit-tested.

## Non-goals

- Changing the benchmark, its ground truth, thresholds, or the N-denominator
  rule. The benchmark is the judge; we move the system under test, not the bar.
- Raising pass-1 recall via further prompt wording alone (exhausted; the fix is
  structural). The prompt cue added in (4) is the last prompt touch.

## Testing (TDD)

Pure-function unit tests (no network), added to `twopass_test.go` /
`infer_test.go`:

- `candidateLiterals`: extracts `devices:sync`, `session:output-batch`,
  `session:output`, `device:online` from a room.ts excerpt; dedupes; ignores
  non-routing-key quoted strings; caps length.
- `refineSignals`/merge path: when pass-1 draft has NO hard structure but the
  corpus contains the literals, the merged protocol gains the confirmed
  handshake on the web role and the confirmed batch (mock driver returns a
  confirm_signals call naming them).
- `mergeConfirmation`: confirmed handshake attaches to `web` role that had none.
- roles correction: `bridge` role with `params.type=web` → corrected to `bridge`.

Existing two-pass/infer tests must still pass (the unconditional-run change may
require existing mock-driver tests to supply a pass-2 response; update fixtures
to the new flow).

`make check` stays network-free and green. After landing, re-run the N=18
benchmark and record the new numbers in the dogfood doc — whatever they are.
