# Scout Zero-Case → Deterministic Fallback — Design

> Brainstormed 2026-07-27. Approach chosen by the user: **A — direct augment**
> (a zero-case LLM round becomes non-fatal; the plan proceeds straight to
> deterministic augmentation; abort only if the augmented plan is still empty).
> Companion: plan `2026-07-27-scout-zero-case-deterministic-fallback-plan.md` (to be written).
> Motivating incident + precise diagnosis: cccmemory `scout-deterministic-ws-cases-gated-behind-llm`.
> Debuggability (added 2026-07-27): cccmemory `cerberus-logging-debug-howto`.

## Background

While validating the credentials.yaml fix (merged 2026-07-27), `cerberus run`
aborted twice with `scout plan: assembly produced zero cases`. The logging
feature (merged the same day) made the cause visible: GLM returned tool calls
that assembled to zero runnable cases. But the deeper finding was structural —
**deterministic WS relay cases are gated behind LLM planning success.**

`Scout.Plan` (plan_phases.go:67) runs Phase 2 (LLM planning) before Phase 3
(deterministic augmentation). On a zero-case LLM round, `runAIPlanning`
returns an error (direct_planning.go:70 or :87) and `Scout.Plan` aborts at
plan_phases.go:83-85 — **before** `augmentPlan` ever runs. So the
protocol-derived deterministic WS relay case (`ws-realtime-relay-web-signal-
device-online`, which needs no LLM) is never generated. A zero-case LLM run
produces zero verdicts even though deterministic cases could run.

## Problem

Two Scout planning outcomes, semantically similar, are treated oppositely:

| LLM outcome | Current behavior | Deterministic cases run? |
|---|---|---|
| Transient error (429/5xx) | `fallbackPlan` (endpoint+invariant) + augment | **yes** (augment runs) |
| Zero tool calls / zero assembled | hard error → `Scout.Plan` aborts | **no** (augment never runs) |

The harsher path (zero-case) is the one that blocks protocol-driven goals
(e.g. the WS relay dogfood). A run that should be testable purely from the
declared protocol instead dies on LLM non-productivity.

## Root cause

The zero-case check lives inside Phase 2 (`runAIPlanning`) and returns a fatal
error, so Phase 3 (`augmentPlan` → `appendExecutorCases` → `WSCasesCovered` +
`GenerateExecutorCases`) is unreachable. There is no later "is the final plan
empty?" guard — the abort happens before deterministic cases can contribute.

## Approach

### Change 1 — `runAIPlanning` (internal/head/scout/direct_planning.go:60)

The two zero exits stop returning an error and instead return an empty plan:

- **Zero tool calls** (line 70): replace
  `return nil, nil, fmt.Errorf("scout plan: zero tool calls (drift or quality)")`
  with a debug log + `return &agent.TestPlan{}, map[string]map[string]bool{}, nil`.
- **Zero assembled cases** (line 87): replace
  `return nil, nil, fmt.Errorf("scout plan: assembly produced zero cases")`
  with a debug log + `return plan, covered, nil` (`assemblePlan` already
  returns a non-nil `*agent.TestPlan` with empty `Cases` at assembly.go:114).

The existing Task-2 debug logs (`scout planning tool calls received`,
`scout planning assembled`, `scout planning produced zero cases`) are kept. A
new `s.logger.Debug("scout planning proceeding to deterministic augmentation")`
line is added at BOTH zero exits (the zero-tool-calls path currently has no
debug log at all — its `"tool calls received"` line sits after the check — so
this is the first visibility for that path), so a run log always shows exactly
what the LLM returned and why deterministic cases took over.

`runAIPlanning` keeps returning `(*agent.TestPlan, map[string]map[string]bool, error)`;
the signature is unchanged.

### Change 2 — final guard in `Scout.Plan` (internal/head/scout/plan_phases.go:67)

Add a post-augmentation empty check, covering both Phase-2 paths (direct and
ToT — they merge before augment):

```go
s.augmentPlan(plan, goal, covered)
if len(plan.Cases) == 0 {
    return nil, fmt.Errorf("scout plan: no cases generated (LLM produced none; no deterministic cases apply to this goal/project)")
}
return plan, nil
```

Semantics: **the run aborts only when neither the LLM nor deterministic
augmentation produced any case.** A zero-case LLM round with a WS protocol
now yields the deterministic relay case; a zero-case round with nothing to
fall back on still aborts (clearly, with a message that names the cause).

## What stays unchanged

- **Transient-error → `fallbackPlan` path** (direct_planning.go:65-67). A
  transient LLM error still degrades to the endpoint+invariant fallback, then
  augments. The zero-case path does NOT route through `fallbackPlan`
  (deliberate — `fallbackPlan`'s HTTP endpoint cases are noise for a WS-relay
  goal; deterministic WS cases are the right answer there). This asymmetry
  reflects different failure semantics: LLM-unreachable vs LLM-unproductive.
- **ToT deep-planning path** (`executeDeepPlanning`) — unchanged; it already
  falls back internally. The new post-augment guard applies to it too (it
  merges before augment), which is correct.
- **A1 coexistence suppression** (ws_cases.go:57-61) — out of scope; that is a
  separate issue (`llm-ws-flow-emission-unstable`) about the LLM covering a
  role. On a zero-case round `covered` is empty, so A1 suppresses nothing and
  the deterministic relay case is generated.
- `assemblePlan`, `WSCasesCovered`, `GenerateExecutorCases`, `DetectProjectType`.
- `runAIPlanning` signature; `DecideWithTools`; the Agent/Examiner "zero tool
  calls" error sites (recovery.go / judge.go / examiner/* — different heads,
  unrelated).

## Files

- `internal/head/scout/direct_planning.go:70` — zero-tool-calls exit → empty plan + nil.
- `internal/head/scout/direct_planning.go:87` — zero-assembled exit → empty plan + nil.
- `internal/head/scout/plan_phases.go:88` — add post-augment `len(plan.Cases)==0` guard.
- `internal/head/scout/direct_planning_test.go` (or `scout_test.go`) — new tests.
- `internal/head/scout/plan_phases_test.go` (new or existing) — the final-guard test.

## Testing (TDD — write the test first, watch it fail, then implement)

- `TestPlan_ZeroToolCalls_ProceedsToDeterministic` — a mock client whose
  `DecideWithTools` returns zero tool calls, with a config that declares a WS
  protocol (2 roles, optional handshake). Assert `Scout.Plan` returns no error
  AND the plan contains the deterministic relay case
  (`ws-realtime-relay-web-signal-device-online` or equivalent). This is the
  bug-reproducer: it FAILS today (Plan errors with "zero tool calls") and
  PASSES after the change.
- `TestPlan_ZeroAssembled_ProceedsToDeterministic` — mock returns tool calls
  that `assemblePlan` turns into zero cases (e.g. a bare `begin_case` with no
  `ws_*`, which assembly drops). Same assertion: no error, deterministic case
  present.
- `TestPlan_NoCasesAtAll_Aborts` — zero tool calls + a config with NO protocol
  AND `Code.Root` pointed at a non-code `t.TempDir()` (so `DetectProjectType`
  returns `ProjectHTTP` and `GenerateExecutorCases` is empty). Assert `Plan`
  returns an error containing "no cases generated". Proves the guard fires
  when nothing can run.
- Existing tests stay green: `TestDirectPlan_ToolCallingAssembly` (LLM produces
  cases), the fallback-on-transient-error tests, and the Task-2 observer tests.

## Verification

1. `make check` (fmt + lint + test -race) EXIT 0.
2. Live: re-run the WS relay dogfood with `CERBERUS_LOG_LEVEL=debug`. Even on a
   GLM zero-case round, the run now produces the deterministic relay case (the
   debug log shows "proceeding to deterministic augmentation" + the relay case
   in the plan), and the relay verdict is reachable. This closes the loop that
   blocked the credentials validation.

## Out of scope (explicit)

- A1 coexistence suppression (separate; `llm-ws-flow-emission-unstable`).
- Making GLM reliably emit ws_flow choreography (LLM-side; same memory).
- Routing zero-case through `fallbackPlan` (rejected as Option B — endpoint
  noise on WS goals).
- Changing `runAIPlanning`'s signature or the transient-error path.
- Moving the `"test plan generated"` Info log (currently fires in Phase 2 on
  success; zero-case runs now reach augment without it — acceptable, augment
  has its own "appended executor cases" Info line).

## Related

- cccmemory `scout-deterministic-ws-cases-gated-behind-llm` (diagnosis),
  `cerberus-logging-debug-howto` (how to see it), `credentials-yaml-not-loaded-bug`
  (the fix this unblocks reliable validation of).
- `cerberus-docs/superpowers/specs/2026-07-27-llm-call-logging-design.md`
  (the logging feature that made this diagnosable).
