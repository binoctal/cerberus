# Examiner-Driven Targeted Replanning Loop — Design Spec

> Feature #3. Status: design (awaiting plan).
> Spec date: 2026-07-29.
> Related: A1 #4 HTTP smoke fallback (shipped, `internal/head/scout/http_cases.go`); WS A1 runtime fallback (`2026-07-28-ws-a1-runtime-fallback-design.md`); recovered rendering (`2026-07-29-recovered-rendering-design.md`); reflexion loop closure (`2026-06-20-reflexion-loop-closure-design.md`, cross-session learning — distinct from this in-session loop).

## 1. Problem

The three-head pipeline is linear: `Scout.Plan → Agent.Execute → Examiner.Examine → consolidate`. When a case fails, the Examiner judges it and the reflexion loop records a lesson for *future* sessions — but within the current run there is no second attempt. A failure caused by a correctable, diagnosable cause (wrong path, missing auth, wrong payload shape) ends in `fail` even though the Examiner already understands why.

## 2. Goal

Close an in-session loop: when the Examiner diagnoses an actionable failure, Scout re-enters in **repair mode** to emit a targeted replacement case, the Agent executes it, and the Examiner re-judges — bounded so cost never runs away. A replacement that passes marks the original target **recovered**, reusing the action-agnostic recovered machinery shipped in A1 / recovered-rendering.

## 3. Decisions (pinned)

| Decision | Choice |
|---|---|
| Loop location | **Approach A — session-orchestrated.** A bounded `executeRepairLoop` between Examiner and Consolidate in `lifecycle_run.go`. No new top-level component. |
| Trigger | **Actionable hints only.** A verdict enters the loop iff `Status==Fail && RedispatchHint != none`. `none` short-circuits. |
| Replacement ↔ original link | **New `TestCase.Replaces` field** (ID of the failed case). Does NOT overload `FallbackFor`. |
| Hint schema | **Enum category + reuse `FinalVerdict.Reasoning`.** Categories: `endpoint_drift` / `auth` / `shape` / `none`. Judge tool gains one enum output field; the diagnosis text is the existing `Reasoning`. |
| No-progress definition | A replacement `R` (replaces `X`) that re-fails **with the same hint category as `X`** is stuck → dropped from later rounds. A *changed* hint (e.g. drift→auth) is progress and may continue. |
| Termination | **Triple:** global round cap (default 2) + per-target no-progress guard + global token-budget backstop. |
| Iteration unit | One round repairs **all** currently-eligible failing cases together. |

## 4. Data Model

### 4.1 `agent.TestCase` — `Replaces`

`internal/head/agent/types.go`:
```go
// Replaces is the ID of the failed case this case is a targeted replacement for
// (feature #3). Empty on normal/planned cases. A replacement is scheduled
// explicitly by the repair loop (not lazily activated like FallbackFor). When a
// replacement passes, its original target counts as recovered.
Replaces string `json:"replaces,omitempty"`
```

`Replaces` and `FallbackFor` are disjoint concepts:
- `FallbackFor` — a *planned* lazy fallback the Agent activates only on primary failure (A1 Phase 2 / #4).
- `Replaces` — a *runtime* targeted patch generated after an Examiner diagnosis, run explicitly by the repair loop.

### 4.2 Examiner — `RedispatchHint`

The enum type is defined in `internal/head/agent` (see §4.4 — avoids a scout→examiner import cycle). Examiner aliases/uses `agent.RedispatchHint` and adds a `FinalVerdict` field:
```go
type RedispatchHint string

const (
	HintNone          RedispatchHint = "none"
	HintEndpointDrift RedispatchHint = "endpoint_drift" // wrong path/method/verb
	HintAuth          RedispatchHint = "auth"           // missing/bad credentials or scheme
	HintShape         RedispatchHint = "shape"          // wrong payload/contract shape
)
```
`FinalVerdict` gains `RedispatchHint RedispatchHint` (default `HintNone`). `JudgeResult` carries the parsed enum; `newFinalVerdict` propagates it; `fallbackVerdict` sets `HintNone`.

### 4.3 Judge tool schema + prompt (the only Examiner re-open)

The Examiner judge tool output schema gains one enum field:
```json
{ "redispatch_hint": "endpoint_drift | auth | shape | none" }
```
The judge prompt is updated to instruct: emit `none` unless the failure has a correctable, diagnosable cause that a replacement case could address; otherwise emit the matching category, with the diagnostic detail in the existing `reasoning`. No structured correction payload (YAGNI — Scout infers the correction from category + reasoning + the failed case + execution evidence).

### 4.4 RepairInput

`internal/head/scout/`:
```go
type RepairInput struct {
	Case      agent.TestCase
	Hint      agent.RedispatchHint
	Reasoning string
}
```
(Import cycle note: `examiner.RedispatchHint` referenced from `scout`. To avoid a scout→examiner import cycle, define `RedispatchHint` in a neutral package — either `internal/types` or `internal/head/agent` (which examiner already imports and scout already imports). **Decision: define the enum in `internal/head/agent`** next to `TestCase`, since both heads import `agent`; examiner aliases it into its package. This keeps scout→agent (already exists) and examiner→agent (already exists), no new edge.)

## 5. Components

### 5.1 `Scout.RepairPlan` (new)

`internal/head/scout/repair_plan.go`:
```go
func (s *Scout) RepairPlan(ctx context.Context, goal string, model *project.ProjectModel, failures []RepairInput) ([]agent.TestCase, error)
```
- Builds a **repair prompt** (new embedded template `promptRepairPlan*`) that lists, per failure: the failed case (target/method/body/expectation), the hint category, and the Examiner reasoning. Asks the LLM to emit exactly one corrected case per failure via a `repair_case` tool (carrying `replaces=<id>`).
- `assembleRepair` (new, `repair_plan.go`; does **not** reuse `assemblePlan` — different semantics) maps each `repair_case` tool call to a `TestCase` with `Replaces` set and the corrected target/method/body.
- Determinism: iterate `failures` in input order; one replacement per failure; drop any LLM output without a matching `replaces`.
- Degrades on LLM error: return `(nil, err)`; the loop logs and breaks.

### 5.2 `executeRepairLoop` (new, session layer)

`internal/session/run_phases_repair.go`:
```go
func (rp *runPhase) executeRepairLoop(model *project.ProjectModel) error
```

**Shared builders (prerequisite refactor).** Today `executeAgentPhase` (`run_phases_agent.go:28`) and `executeExaminerPhase` (`run_phases_examiner.go:21`) construct their head inline. Factor those constructions into `rp.buildAgentLoop() *agent.ReActLoop` (or the parallel executor) and `rp.buildExaminer() *examiner.Examiner` shared helpers, used by both the original phase and the repair loop. This avoids duplicating the engine/executor/examiner wiring.

State held on `runPhase` (or local): `lastHint map[string]agent.RedispatchHint` keyed by normalized target, derived from the Replaces chain + committed/in-memory verdicts.

Per round (1..`replanMaxRounds`, default 2):
1. **Eligibility:** from `rp.verdicts`, collect failures where `Status==Fail && RedispatchHint!=none`. Drop a failure whose target is already `stuck` (no-progress, see 5.3). If none remain → return.
2. **Budget gate:** if remaining token budget below threshold OR escalation gate says stop → return.
3. `replacements := scoutHead.RepairPlan(ctx, goal, model, eligible)`.
4. Append replacements to `rp.plan.Cases`; re-`SavePlan` (it upserts by session — no versioning needed).
5. **Execute only replacements:** run a sub-plan through the existing executor — `rp.buildAgentLoop().ExecutePlan(ctx, &agent.TestPlan{Cases: replacements}, sessionID)` (parallel path mirrors). Collects replacement `StepResult`s.
6. **Re-judge:** `rp.buildExaminer().Examine(ctx, replacementResults, sessionID, project)` → merge verdicts into `rp.verdicts`; persist via `PersistFinalVerdicts`.
7. Update `lastHint` from the new replacement verdicts.

Any phase error inside a round → `logger.Warn` + return (keep verdicts gathered so far; fall through to consolidate). Repair is an enhancement, never a run abort.

### 5.3 No-progress guard

After step 6, for each replacement verdict `R` that replaces `X`:
- If `R.Status==Fail` AND `R.RedispatchHint == X.RedispatchHint` (same category) → mark `X`'s target `stuck` (drop from future rounds).
- If `R.Status==Fail` AND hint changed → not stuck; `R` itself becomes eligible next round (its target may be repaired again, inheriting `R` as the new predecessor via the Replaces chain).
- If `R.Status==Pass` → target recovered (see 5.4); not eligible.

`X`'s hint comes from `X`'s verdict (verdicts now carry `RedispatchHint`). The predecessor is found by walking `Replaces` to the prior verdict for that case ID.

### 5.4 Recovered wiring for `Replaces`

A passed replacement recovers its original target. **Important:** `StepResult.Recovered` is set by the Agent's `FallbackFor` activation path (`executor_run.go`) and is NOT set for `Replaces` cases (the loop runs them explicitly, not via lazy activation). So the recovered wiring must gate `Replaces` on **pass-status**, not on the `Recovered` bool.

`internal/session/summary.go` pairs primary↔fallback today via `tc.FallbackFor != "" && r.Recovered` → `recoveredPrimaryIDs[tc.FallbackFor]`. Add the `Replaces` analog: a result with `tc.Replaces != "" && r.Status == StepPassed` → add `tc.Replaces` to the recovered-primary set. The existing reclassify-primary-out-of-Failed logic then covers replaced primaries unchanged.

`internal/session/run_phases_consolidate.go` skips fallback verdicts (`FallbackFor != ""`, `Recovered`) so a recovered fallback is not double-counted as an independent unit. Add the `Replaces` analog: skip a passed `Replaces` verdict (its target is already tallied via the recovered primary); a failed `Replaces` verdict stays as its own fail (the target is not recovered).

`internal/report/html*` / `markdown_helpers.go` already renders a `recovered` badge by status — no change needed there beyond the summary feeding it the recovered classification.

Net effect: a target recovered by replacement renders as `recovered` (same badge as #4); the original `fail` verdict stays a fail (mirrors A1 Phase 2 — recovered marks the target covered, not the original verdict flipped).

### 5.5 Termination summary

| Signal | Scope | Effect |
|---|---|---|
| Round cap (default 2, `Settings.replan_max_rounds`) | global | stop after N rounds |
| No-progress (same hint re-fail) | per-target | drop target from later rounds |
| Token budget / escalation gate | global | stop before next round |

## 6. Control Flow

```
Run():
  initialize → resolveActorAuth
  Scout: Analyze → Plan           (Phase 1)
  Agent: ExecutePlan              (Phase 2)
  Examiner: Examine → verdicts    (Phase 3)
  executeRepairLoop(model):       (Phase 3.1, NEW)
    for round in 1..maxRounds:
      eligible = failures(hint!=none) - stuck
      if empty or budget-low: break
      replacements = Scout.RepairPlan(eligible)
      Agent.executeSubset(replacements)
      Examiner.Examine(replacements) → merge verdicts
      update stuck-map
  Consolidate                     (Phase 3.5)
  AutoTest                        (Phase 4)
  buildSummary
```

## 7. Error Handling & Degradation

- Repair is an enhancement, never a dependency: any error in `RepairPlan` / subset-execute / re-judge → `logger.Warn` + break the loop; the run continues to consolidate/summary with whatever verdicts exist.
- `RedispatchHint` defaults to `none` everywhere the LLM is bypassed (fallback verdict, parse failure, empty tool output) — so a degraded Examiner never accidentally triggers replanning.
- Budget backstop is independent of the round cap, so a misconfigured high cap still cannot run away.
- On resume: the loop reads committed verdicts and skips replacements already judged (idempotent — same discipline as reflexion consolidate).

## 8. Out of Scope

- Structured correction payload in the hint (category + reasoning only).
- Replanning non-endpoint failures is allowed if the Examiner tags them, but hint categories are endpoint-flavoured (drift/auth/shape); process/code/lint failures will usually emit `none` and short-circuit. No special handling.
- Cross-session learning of repair outcomes (that is the reflexion loop's job).
- Auth-flow deterministic fallback counterpart (parked from #4).
- A `Replaces`-chain depth limit beyond the round cap (the round cap + no-progress bound it).

## 9. Testing Strategy

- **Examiner hint parsing** (`examiner/judge_test.go`): 4 categories parse; missing/empty → `none`; fallback verdict → `none`.
- **`Scout.RepairPlan`** (`scout/repair_plan_test.go`, new): mock LLM returns `repair_case` calls → assert one replacement per failure, `Replaces` pairing correct, `none`-hint failures produce nothing, LLM error → `(nil, err)`.
- **No-progress guard** (`session/repair_loop_test.go`, new): two synthetic rounds — (a) same-hint re-fail → target dropped; (b) hint change (drift→auth) → target survives to next round.
- **Termination**: round-cap stop; budget-exhausted stop (stub `TokenBudget`).
- **Recovered render** (extend `consolidate_recovered_test.go` / summary test): passed replacement → original target counted recovered; original verdict stays fail.
- **Integration** (extend `session/reflexion_integration_test.go` pattern): one full round — actionable fail → repair → replacement passes → recovered; plus a same-hint re-fail → stopped.

## 10. File Inventory (plan will finalize)

New:
- `internal/head/scout/repair_plan.go` (+ test) — `RepairPlan`, `RepairInput`, `assembleRepair`, repair prompt.
- `internal/session/run_phases_repair.go` (+ test) — `executeRepairLoop`, no-progress map.
- Repair prompt templates in `internal/prompts/` (+ project-level override path).

Modified:
- `internal/head/agent/types.go` — `TestCase.Replaces`; `RedispatchHint` enum (to break the import cycle).
- `internal/head/examiner/types.go` / `policy.go` / `policy_helpers.go` — `FinalVerdict.RedispatchHint`; `newFinalVerdict`/`fallbackVerdict`.
- `internal/head/examiner/tools.go` + `prompts.go` + `assembly.go` — judge tool `redispatch_hint` field + parse.
- `internal/session/lifecycle_run.go` — call `executeRepairLoop` after Examiner.
- `internal/session/run_phases_agent.go` — factor loop construction into `buildAgentLoop()` shared helper (used by phase + repair subset run; subset = `ExecutePlan(&TestPlan{Cases: replacements})`).
- `internal/session/run_phases_examiner.go` — factor examiner construction into `buildExaminer()` shared helper (used by phase + repair re-judge).
- `internal/session/summary.go`, `internal/session/run_phases_consolidate.go` — `Replaces` recovered wiring (gate on `Status==StepPassed`, not the `Recovered` bool).
- `internal/config` — `replan_max_rounds` setting (default 2).
