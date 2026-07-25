# ADR: S2 Analyze Intentional Drift→Degrade (Not Error)

## Context

The S2 tool-calling migration (`cerberus-docs/superpowers/specs/2026-07-25-tool-migration-s2-scout-design.md`) moved Scout's LLM call sites from free-form JSON to typed tools. Spec §4 sets the general fallback policy with two distinct paths:

- **Drift** (zero tool calls / unparseable intent) → Scout returns an error directly. Problems surface.
- **Transient** (LLM error: rate limit / 5xx / budget / network) → fallback (`fallbackPlan` / config-only model) retained.

The spec's split is applied at `directPlan` (`internal/head/scout/direct_planning.go`) and `BuildCoverageContract` (`internal/head/scout/contract.go`): both return a hard error on zero tool calls and fall back on a client error.

## Decision

`Scout.runAIInference` (`internal/head/scout/analyze_phases.go`) **intentionally deviates** from spec §4: it collapses both drift (zero tool calls) and transient (LLM error) into a single `config-only degradation` path:

```go
res, err := s.driver.DecideWithTools(ctx, prompt, analyzeTools())
if err != nil || len(res.ToolCalls) == 0 {
    s.logger.Warn("AI analysis failed/empty, using config-only model", zap.Error(err))
    return configModel, nil // Graceful degradation
}
```

This was an explicit choice from plan Task 4 Step 4 (`cerberus-docs/superpowers/plans/2026-07-25-tool-migration-s2-scout.md`), recorded here rather than relitigated at the call site.

## Rationale

Analyze is structurally different from Plan / Contract:

- **Plan (`directPlan`)** has no safe ground truth — `fallbackPlan` exists but is itself LLM-authored; on drift there is nothing authoritative to fall back to, so erroring surfaces the problem.
- **Contract (`BuildCoverageContract`)** likewise has no non-LLM source — a config-derived contract would be a different product.
- **Analyze enhances a ground-truth config model.** `buildConfigModel` already produced a `*project.ProjectModel` from `project.yaml` invariants + service health endpoints before the LLM is consulted. The LLM only enriches it. If the LLM drifts or fails, returning the config model is the **correct** result, not a degraded one — the config is authoritative.

Erroring on analysis drift (per spec §4's general rule) would abort the entire session and lose downstream planning/contract work, even though a perfectly usable config-only model is already in hand. The safe-fallback asymmetry justifies Analyze keeping a single degrade path.

## Trade-off / Revisit

The cost is that **analysis drift is not surfaced as a hard error** — only a `Warn` log line. This is acceptable because:

- The config model is authoritative; a config-only Analyze result is still a valid input to Plan and Contract.
- Drift in analysis (vs. plan/contract) is the cheapest place to absorb failure: the next stages re-derive their inputs from the model + goal, not from Analyze's tool calls.

Revisit if:

- Analyze grows a code path where the LLM is the **only** source for some field (today it is purely additive — `mergeAIInference` overlays tool-call-derived endpoints/pages/tech onto the config model). At that point a drift→error split matching spec §4 may become correct.
- The `Warn` log proves insufficient for operators noticing analysis drift in CI (e.g., add a metric / counter).

## References

- Spec: `cerberus-docs/superpowers/specs/2026-07-25-tool-migration-s2-scout-design.md` §4
- Plan: `cerberus-docs/superpowers/plans/2026-07-25-tool-migration-s2-scout.md` (Task 4 Step 4)
- Code: `internal/head/scout/analyze_phases.go:50`
- Related: `internal/head/scout/direct_planning.go`, `internal/head/scout/contract.go` (split-policy siblings)
