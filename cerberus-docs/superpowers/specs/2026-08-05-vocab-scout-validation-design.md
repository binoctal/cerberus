# Scout Vocabulary Value Validation — Design

> Date: 2026-08-05
> Scope: Validate that loading a real WS routing vocabulary into Scout's
> planning prompt measurably changes planning output for the better.
> Status: Design (pre-implementation).

## Background

The `protocol-vocabulary-v2` work (merged on `main`) wired the extracted WS
routing vocabulary into Scout's prompt in two places:

- **Direct planning** — `buildPlanningContext` appends
  `renderVocabSummary(s.config.Services)` to the context block
  (`internal/head/scout/direct_planning.go`).
- **ToT planning (default)** — `executeDeepPlanning` calls
  `planner.SetVocabSummary(renderVocabSummary(...))`, which prepends the
  summary to every propose prompt via `buildProposeTask`
  (`internal/head/scout/tot.go`).

The dogfood config at `dogfood/ws-realtime/.cerberus/` carries a real
`vocab/open-agents.vocab.yaml` (70 edges, ~65 concrete message types such as
`device:online`, `session:created`, `workflow:task_progress`). The loader
auto-loads it into `Service.Vocabulary` whenever `protocol_ref` is set
(`internal/project/loader.go:136-143`).

What is **not yet shown**: that injecting this vocabulary actually improves
what Scout plans. Without that evidence the feature is a prompt change with an
untested hypothesis.

## Goal

Demonstrate, via a repeatable qualitative comparison, that a real vocabulary
changes Scout's WS planning output in a direction that is clearly better:
plans reference concrete message types that actually exist and author
choreography along real routing directions, instead of inventing types.

## Non-goals (this round)

- Wiring vocabulary into the Agent or Examiner. That is independent follow-on
  work, decided after this validation lands a result.
- A `cerberus plan --dump` CLI. Deferred; the dump logic written here can be
  lifted into such a command later if we want to repeat the comparison
  routinely.
- Quantitative regression metrics. The signal is qualitative + a lightweight
  type-hit count, not a coverage score.

## Hypothesis & signal

**Hypothesis:** With the vocabulary present, Scout's plans cite message types
that exist in the real vocabulary and follow real web↔bridge routing
directions; without it, Scout invents plausible-but-nonexistent types.

**Primary signal (hard):** count `namespace:action` tokens in each plan and
classify each against the real vocabulary type set:

- `hit` — the token is a real vocabulary type.
- `invented` — the token is not in the vocabulary (e.g. `message:received`).

**Secondary signal (qualitative):** human review of the authored WS
choreography — does it follow real routing directions (web→bridge,
bridge→web), and does it drive lifecycle/relay flows that match the real
`open-agents` behavior recorded in
`cerberus-docs/technical/dogfood/2026-08-02-cerberus-openagents-mapping.md`.

Because LLM output is non-deterministic, a single run cannot separate
vocab-driven change from noise. Each condition is run **N = 3** times and the
signal is the **commonality** across runs (a type or pattern counts if it
appears in ≥2 of 3 runs).

## Approach

**Layered execution:**

1. **Core (this spec): a one-off `//go:build manual` Go test** that calls
   `Scout.Plan` directly and dumps the plan text. This alone can produce the
   comparison and reach a conclusion.
2. **End-to-end confirmation (optional, gated on Core's result):** a real
   `cerberus run` against live `open-agents` (wrangler dev) to confirm that a
   "better-looking" plan actually hits more real routes. Only done if Core
   shows a worthwhile difference.
3. **CLI extraction (deferred):** lift the dump helper into `cerberus plan --dump`
   only if we later want to repeat this routinely.

## Design details

### Test carrier & isolation

- File: `internal/head/scout/vocab_validation_test.go`, build tag
  `//go:build manual`.
- `//go:build manual` (not `integration`): this is an LLM experiment requiring
  `ANTHROPIC_API_KEY`, not a target-service-online integration. It must not
  run in `make test` or CI.
- Skip guard: `t.Skip` when `ANTHROPIC_API_KEY` is unset.
- No target service needed — `Scout.Plan` is a pure LLM call; `open-agents`
  stays offline for the Core step.

### Simulating "no vocabulary"

Do not move files. Load the dogfood config once, then construct the no-vocab
condition in-memory by zeroing `svc.Vocabulary = nil` on each service before
building the no-vocab Scout. This is byte-equivalent to a missing vocab file
(`renderVocabSummary` returns `""` when `Vocabulary` is nil or edgeless) and
keeps the test self-contained and non-destructive.

### Planning goal (deliberately type-name-free)

> Cover the realtime WebSocket service's message relay between web and bridge
> actors: session lifecycle, bridge join/leave signaling, and workflow task
> progress broadcast. Author WS choreography that drives messages from each
> role and asserts what each peer receives.

Concrete type names are intentionally omitted so the model chooses them —
exposing the real-type vs invented-type split.

### Runs

- Default planner = ToT (`scout.go:49` sets `deepPlan = true`). Validate ToT
  first; it is the production path.
- **N = 3 runs per condition** (with-vocab, without-vocab) = 6 full ToT
  planning passes. Cost/ signal tradeoff; revisit if cost is a concern.

### Type-hit extraction

Scan each dumped plan for `namespace:action` tokens (regex roughly
`[a-z][a-z0-9_]*:[a-z][a-z0-9_-]*`). Classify against the real vocabulary type
set (the set of `type:` values in
`dogfood/ws-realtime/.cerberus/vocab/open-agents.vocab.yaml`). Record per plan:
hit count, invented count, invented list.

### Outputs

- Plan dumps: `runtime/vocab-validation/{with,without}-vocab-run{1..3}.md`
  (gitignored runtime location).
- Summary report:
  `cerberus-docs/technical/validation/2026-08-05-vocab-scout-validation.md` —
  type-hit table per condition, observed choreography differences (human
  notes), and a stated conclusion.

### Optional direct-planner arm

The test accepts a flag to run the direct planner under the same two
conditions, reusing the identical dump + hit-extraction helpers. Low cost; the
direct planner also injects vocab and is worth a glance, but ToT is the primary
arm.

## Success criteria

The vocabulary's value is considered demonstrated when, across the 3 runs:

- With-vocab plans: `invented ≈ 0`; choreography follows real routing
  directions.
- Without-vocab plans: `invented` types appear with commonality (≥2 of 3).
- The split is consistent across runs (not a single-run artifact).

If with-vocab plans still invent many types, or the split is noise, that is a
negative result and is reported as such — it would mean the prompt injection is
not enough and the feature needs rework.

## Risks

- **Non-determinism:** mitigated by N=3 + commonality; report will show
  per-run breakdown so noise is visible.
- **LLM cost:** 6 ToT passes (each multi-round) use real budget. Acceptable
  for a one-off; the test is opt-in via the `manual` tag.
- **Type extraction false positives:** some `namespace:action` tokens in plan
  prose may be illustrative rather than asserted types. Mitigation: report the
  raw invented list so a human can discount noise; the signal is the
  *difference* between conditions, not the absolute count.
- **API-key dependency:** test skips cleanly without it; no fallback path
  needed.
