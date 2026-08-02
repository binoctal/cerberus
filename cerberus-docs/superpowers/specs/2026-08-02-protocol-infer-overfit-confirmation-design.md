# protocol-infer detector overfit — empirical confirmation — Design

- Date: 2026-08-02
- Scope: upgrade the "detectors are validated only on open-agents" scope claim
  from inference to MEASURED fact, providing the hard evidence plan B needs to
  formally scope `protocol infer` to open-agents-style protocols.
- Supersedes the benchmark-based plan (a second diverse WS target run through
  `protoinferbench`). That plan was abandoned during review — see "Why not the
  benchmark" below.

## Why not the benchmark

The deterministic detectors (`detectGuardedHandshake`, `detectTimerFlushBatch`)
are wired as **fallback-only** in `internal/protocoldiscover/infer.go:141-152`:
they run only when the LLM path (pass-1 draft + pass-2 merge) produced no
handshake/batch. Consequences for a benchmark-based overfit probe:

1. The benchmark measures the LLM, not the detectors. On a diverse target the
   LLM may still emit the correct structure, so the detectors never run and the
   benchmark PASSes — a false "no overfit". The detectors fire only on LLM
   failure, making any FAIL LLM-attributable, not detector-attributable.
2. A target hand-crafted to use the failure axes the design doc already names is
   a tautology: "a thing built to break detector X breaks detector X". It is a
   known-answer regression, not an overfit probe.

Net: the benchmark is the wrong instrument for "are the detectors overfit".

## The right instrument: deterministic inverse-golden tests

The detectors are pure functions of source text. Their overfit is therefore
confirmable directly, with zero LLM cost and full attribution, by asserting they
mis-handle conventions outside open-agents'. This mirrors the positive golden
tests (`twopass_golden_test.go`) that lock the 7/7 behavior on the real
open-agents source.

Coverage already in place (added by the guardrail work, reframed here as
measured overfit evidence):

- `TestGuardShape_OnlyGreaterThanZero` — `detectGuardedHandshake` matches only
  the `> 0` guard; misses `.length` truthiness, `>= 1`, `!== 0` (recall loss).
- `TestGuardedHandshake_FirstGuardWins` — returns the FIRST guarded send in
  corpus order; a non-connect first guard yields a spurious handshake
  (precision risk).
- `TestFlushKey_OnlyBatchSuffix` — `detectTimerFlushBatch` is a no-op on
  `:flush`, `:bulk`, `batch:` prefix flush keys (recall loss).

## Incremental work (this plan)

Two detector overfit modes are NOT yet pinned. Add inverse-golden tests:

1. `extractItemsPath` — returns the first `field: <buffer>.<field>` payload
   entry. On a payload whose first buffer-property entry is a non-array sibling
   leaf preceding the array (e.g. `count: batch.count` before `lines: batch.lines`),
   it returns the wrong leaf → silent wrong `items_path`. Assert this.
2. `connectRole` — prefers a role literally named `web`, else falls back to the
   lexicographically smallest role. A target with no `web` role attaches an
   added handshake to an arbitrary (lex-smallest) role. Assert the lex-smallest
   fallback and document it as the semantic-arbitrariness limit.

Both are pure, deterministic, no-I/O.

## Outcome

With these added, every overfit mode listed in the recall-fix design's
"Generality scope" section is pinned by a test that demonstrates it. The scope
claim becomes measured, not inferred — the evidence plan B cites to formally
declare `protocol infer` open-agents-scoped and draft-only elsewhere.

## Testing (TDD)

- Write the two inverse-golden tests first; they PASS against current code
  (capturing current overfit behavior), then serve as regression guards.
- `make check` (fmt + lint + `go test -race`) must stay green; no network/LLM.

## Deferred

End-to-end robustness on a diverse target ("does the whole pipeline still draft
correctly?") is a separate, valid question that DOES warrant the benchmark — but
with a neutral or real target, not a hand-crafted-to-fail one, and at LLM cost.
Out of scope here; revisit only if `protocol infer` is to be trusted beyond
open-agents.
