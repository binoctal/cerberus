# Examiner Vocabulary Injection — Design (2026-08-06)

## Background

The Scout (ToT) planner's vocabulary injection was validated on 2026-08-05
(`cerberus-docs/technical/validation/2026-08-05-vocab-scout-validation.md`):
with vocab, every run emits 11–16 real protocol message types as drivable
`namespace:action` choreography (hit rate ~94–99%); without vocab, zero typed
messages. That validation covered the **Scout/plan path only**. Its first
follow-up reads:

> Wire vocab into Agent/Examiner: this run validates the Scout/plan path only.
> Agent execution and Examiner judging also consume protocol types; confirming
> they route on the same vocabulary is the natural next validation.

This spec covers that follow-up.

## Current state (verified from code)

- **Agent — already vocabulary-driven, no change needed for production.**
  `internal/head/agent/vocabulary_driven_test.go` (integration build tag) loads
  `vocab.yaml`, builds a TestCase per `message_handled` edge (honoring
  `RouteField` → payload shape, `exclude_sender` → second web client), and
  asserts end-to-end relay against a live DO. In the production path the Agent
  executes the cases Scout authors — which are already vocabulary-driven. The
  Agent executor itself is a deterministic step runner; it does not need vocab
  context.

- **Examiner — the real gap.** A full-package grep of
  `internal/head/examiner/` finds zero references to `Services`, `Vocabulary`,
  or any config carrying the protocol vocabulary. `ExaminerConfig`
  (`internal/head/examiner/types.go:51`) holds only `MaxCritiques`,
  `ConfThreshold`, `AutoFix`, `MaxWorkers`. `Judge.Judge`
  (`internal/head/examiner/judge.go:33`) builds its prompt from
  `buildEvidenceContext` (WS matched/seen messages, step trace) plus the
  free-text `expectation`. **The judge never sees the set of legal message
  types or routing direction**, so on a vague expectation it can drift —
  `buildEvidenceContext`'s own comments already flag relay-pass-misjudged-as-
  fail as a known risk.

## Goal

Give the Examiner judge the same routing vocabulary the Scout planner already
uses, so verdicts on WS cases are anchored to concrete legal types and routing
semantics rather than expectation prose alone. Add a non-integration Agent
regression test so vocabulary consumption is covered without a live DO.

## Non-goals

- No change to the Agent production execution path.
- No change to Scout vocab rendering behavior (only its location/export).
- No new vocab extraction; the vocabulary is read from the already-loaded
  `project.Config.Services[*].Vocabulary`.

## Design

### Part 1 — Inject vocabulary into the Examiner judge prompt

**Lift the renderer to a shared location.** `renderVocabSummary` lives today
in `internal/head/scout/vocab_context.go` as an **unexported** function. Move
it to `internal/project/` (the package that already owns the `Vocabulary`
type) as an **exported** `RenderVocabSummary(services []Service) string`,
verbatim. The scout package then calls `project.RenderVocabSummary` (delete
the local copy, keep the call site at `plan_phases.go:42`). This removes any
scout→examiner coupling and gives both heads + the session one renderer.

**Add the field.** Extend `ExaminerConfig`:

```go
type ExaminerConfig struct {
    MaxCritiques  int
    ConfThreshold float64
    AutoFix       string
    MaxWorkers    int
    VocabSummary  string // WS routing vocabulary prepended to judge prompts; "" = no-op
}
```

**Wire it at the session construction site.**
`internal/session/run_phases_examiner.go:buildExaminer` (and the mirror in
`resume_phases_run.go`) already reads `rp.session.Config`. Add:

```go
examinerCfg.VocabSummary = project.RenderVocabSummary(rp.session.Config.Services)
```

**Inject in the judge.** In `Judge.Judge`, prepend the vocab block to the
prompt's context (same `WS Routing Vocabulary:` formatting the Scout propose
prompt uses). When `VocabSummary == ""` the prompt is byte-identical to today
→ zero regression for non-WS projects. The critic (`critique`) deliberately
does **not** receive vocab: it reviews the initial verdict for internal
consistency, not protocol legality, and keeping it unchanged preserves the
scoring-tier separation.

### Part 2 — Non-integration Agent vocabulary-consumption regression test

The only existing Agent vocab coverage is the integration-tagged
`vocabulary_driven_test.go`, which depends on a live DO via `setupOpenAgents`.
Add a `go test` (no build tag) regression test that exercises vocabulary
consumption **without** a live backend, using the existing in-memory WS test
harness (`internal/head/agent/websocket_test.go` already defines one). It will:

1. Build a minimal `project.Vocabulary` with representative edges spanning the
   routing dimensions that matter: `web→bridge send_bridge_by_device`
   (route_field present/absent), `bridge→web broadcast_web`
   (`exclude_sender` true), and a `message_handled` lifecycle edge.
2. Construct the same per-edge TestCase table
   `vocabulary_driven_test.go` builds (reuse the step-building logic — see
   "Refactor opportunity" below), pointed at the in-memory WS server
   programmed to mimic the DO's routing (relay by type, honor
   `exclude_sender`, reject on missing route field).
3. Assert each non-unsupported `message_handled` edge relays correctly:
   receiver observes the type, `exclude_sender` edges are absent at the
   sender, route-field-absent edges produce no relay.

**Refactor opportunity (in scope).** The per-edge TestCase + outbound-message
construction in `vocabulary_driven_test.go:30–130` is hand-rolled inline.
Extract it to a tested production helper, e.g.
`agent.BuildEdgeSteps(edge, connSpec) ([]TestStep, outboundMsg string)`, so
both the integration test and the new unit test call one code path. This makes
"the Agent consumes vocab edges" a property with a single implementation
rather than test-only logic.

### Part 3 — Fix extractor measurement noise (direction 2)

The 2026-08-05 validation's "invented" counts overstate fabrication because
the extractor scans the whole JSON dump, matching the per-case `name` field
truncated to ~60 chars (`…`). Fix in the validation helper test file
(`internal/head/scout/vocab_validation_helpers_test.go`): restrict the scan
to `target`, `expectation`, and `steps` fields only — exclude `name`. This is
measurement-only (no production impact); the prior verdict is unchanged, but
future validation runs report true fabrication.

## Verification plan

**Examiner (new manual validation, mirroring the Scout one):**
with-vocab vs without-vocab, `N=3` runs each, on the same `dogfood/ws-realtime`
config and `glm-5.2[1m]` model. Metric: **judge drift rate** on a fixed set
of WS relay cases with known ground truth (deliberately including one
vague-expectation case that should pass). Success criteria:

- With-vocab drift rate (misjudged verdicts / total) strictly lower than
  without-vocab across the 3 runs.
- Zero regression on non-WS cases (deterministic `objectiveVerdict` path
  unchanged).
- Vocab block absent from every prompt when no service declares a vocabulary
  (byte-identical prompt — assert in a unit test).

**Agent:** the new non-integration test passes in `make test` (no live DO).
The existing integration test still passes when a DO is reachable.

**Extractor:** re-run the Scout validation's invented-token accounting on one
with-vocab run and confirm the invented-list no longer contains truncated
`name` tails.

## Files touched

- `internal/project/vocab_render.go` — new, exported `RenderVocabSummary`
  (moved from scout).
- `internal/head/scout/vocab_context.go` — delete local renderer, call
  `project.RenderVocabSummary`.
- `internal/head/scout/vocab_context_test.go` — adjust import/expectations
  (behavior unchanged).
- `internal/head/examiner/types.go` — add `VocabSummary` field.
- `internal/head/examiner/judge.go` — prepend vocab block in `Judge`.
- `internal/head/examiner/judge_test.go` — assert vocab block present when
  set, absent when empty.
- `internal/session/run_phases_examiner.go`, `resume_phases_run.go` — fill
  `VocabSummary` from `rp.session.Config.Services`.
- `internal/head/agent/edge_steps.go` — new, `BuildEdgeSteps` helper.
- `internal/head/agent/vocabulary_driven_test.go` — call helper.
- `internal/head/agent/vocabulary_unit_test.go` — new, non-integration
  regression test on in-memory harness.
- `internal/head/scout/vocab_validation_helpers_test.go` — restrict scan to
  `target`/`expectation`/`steps`.
- `cerberus-docs/technical/validation/2026-08-06-examiner-vocab-validation.md`
  — new, results write-up.

## Risk

- **Prompt-size growth.** The vocab block for ws-realtime is ~67 edge types
  grouped compactly; acceptable for the judge tier and already paid by the
  Scout propose prompt. If a future vocab is huge, cap rendering (out of scope
  here).
- **ExaminerConfig value semantics.** Adding a string field is safe; existing
  callers using `DefaultExaminerConfig()` get `""` → no-op.
