# Tool-Migration S4 — Examiner Verdict Tool-Calling — Design — 2026-07-26

> Revised after adversarial review: added the `DecideWithTools` retry-parity
> prerequisite (Task 0, a latent regression in S2/S3), collapsed `assess` to a
> single tool with nested gaps, pinned assess zero-calls→error, split the live
> gate into its own task, folded `objSchema` extraction, dropped `self_critique`
> from the initial judge schema, and noted the autofix skip-field choice.

## Background

S2 migrated Scout and S3 migrated the Agent head to `DecideWithTools`. **Examiner
has no drift-absorption subsystem** — its `Decide` sites parse strict JSON and
degrade on error (Judge failed → `fallbackVerdict`; critic failed → refund + keep
initial; autofix failed → `{Attempted,Success:false}`; reflection failed → error).
No tolerant `UnmarshalJSON`, no keyword guessing. So S4 is **architectural
completion** (unify the third head + harden verdict schemas), not drift-elimination
(the migration's drift goal was achieved in S3).

**Latent regression discovered during S4 review (fixes S2/S3 too):** `Driver.Decide`
wraps its call with `executeWithRetry` + response caching; `Driver.DecideWithTools`
does NEITHER — it is a bare `client.Complete`. So every site S2/S3 migrated lost
transient-error retry (Scout now falls back on the first rate-limit instead of
retrying) and response caching. Task 0 restores retry parity (caching tool responses
is deferred — Examiner inputs vary per call, so cache-hit value is low, and it needs
a tool-call-aware cache path). Task 0 retroactively fixes S2/S3.

`Driver.Decide` is NOT removed — it stays for legitimate blob/code-gen callers:
`internal/autotest` (4 sites emitting generated test code), `authdiscover`,
`protocoldiscover`, `smoke/selftest` (scope decision, 2026-07-26).

## Goal

1. **Task 0 — `DecideWithTools` retry parity** (internal/ai): wrap the call in
   `executeWithRetry`, matching `Decide`. Fixes transient resilience for Scout/Agent
   (S2/S3) and Examiner (S4).
2. Migrate the five Examiner `Decide` sites to `DecideWithTools` via small per-site
   tool surfaces, preserving each site's graceful-degradation behavior.

The five Examiner sites (`grep '\.Decide(' internal/head/examiner`):
1. `judge.go:52` — `judgeDriver` → `JudgeResult` (step verdict)
2. `judge.go:102` — `criticDriver` → `CritiqueResult` (self-refine critique)
3. `assess.go:24` — `judgeDriver` → `contract.Assessment` (coverage assessment)
4. `autofix.go:68` — `driver` → `{reasoning, skip}` (repair analysis)
5. `learner_run.go:31` — `driver` → `[]Reflection` (lessons learned)

## Design

### 1. Per-site tool surfaces (one structured object per call)

| Site | Tool | Input (mirrors output struct JSON tags) |
|---|---|---|
| judge | `judge_result` | `status` enum(pass/fail/skip/uncertain), `existence_confidence`, `correctness_confidence`, `reasoning` — the 4 fields today's `promptJudgeOutput` asks for. `SelfCritique`/`CritiqueTriggered` are set by the evaluate code path (`judge.go:116-117`), NOT the initial LLM call, so they are OMITTED from the schema (faithful to today's prompt). |
| critic | `critique_verdict` | `issues_found` bool, `critique`, `suggested_status` enum, `suggested_confidence` |
| assess | `assess_coverage` (single tool) | `reached` bool, `gaps: [{kind, detail}]` (nested array — Assessment is ONE object with a gap list; NOT split into per-gap tools, since the analogy to S2's additive contract tools does not hold), `reasoning`. `coverage_pct` is OMITTED — when coverage is measurable it is always overwritten by the objective `m.Pct` (`assess.go:44`); exposing it misleads the LLM. |
| autofix | `suggest_fix` | `reasoning`, `skip` bool — single tool, `skip` as a FIELD. (Differs from S3 recovery's separate `skip` tool deliberately: recovery had N action tools to be mutually exclusive with; autofix emits one object, so skip is a field.) |
| learner | `report_reflection` | `type` enum(failure/success), `diagnosis`, `strategy`, `condition_pattern`, `category` — one per reflection (multiple calls = multiple reflections) |

Assembly: `internal/head/examiner/assembly.go` maps tool call(s) → the output struct,
reusing `internal/llm/toolfield.go` helpers. `assess` and `learner` collect from the
(single / multiple) call(s).

### 2. Drift / degradation policy (preserve today's behavior; pin ambiguities)

Examiner degrades gracefully and this is PRESERVED — S4 does NOT introduce hard
errors on drift for the degrade-capable sites (judge/critic/autofix/learner):

- **judge**: LLM error OR zero tool calls → `fallbackVerdict` (from execution status), as today. No change to degradation.
- **critic**: error/zero → refund the critique slot + keep initial verdict, as today. (The refund path gains a new zero-call trigger; add a test for it.)
- **autofix**: error/zero → `{Attempted:true, Success:false}`, as today.
- **learner**: error → propagate; zero calls → empty reflections (none stored).
- **assess** (PINNED — was ambiguous): zero tool calls → `fmt.Errorf("assess coverage: zero tool calls (drift)")`. Rationale: assess's verdict flows to the contract gate; silently degrading drift to `{Reached:false}` would be indistinguishable from a real "not reached" verdict and could mask a coverage failure. Matching today's error-propagation is the safe choice. (Transient LLM error also propagates as today.)

The drift/transient distinction (nil error + zero ToolCalls = drift; non-nil = transient) is made as in S2/S3, routed to each site's policy above.

### 3. Task 0 — `DecideWithTools` retry parity

`Driver.DecideWithTools` (internal/ai/driver_tools.go) wraps its `client.Complete` in `executeWithRetry(d.retry.MaxRetries, d.retry.BaseDelay, d.retry.MaxDelay, ...)` — the same retry wrapper `Decide` uses. Budget check + `budget.Record` stay. Caching tool responses is DEFERRED (noted tradeoff: low hit-rate for varying inputs; needs a tool-call-aware cache path). This is a standalone commit + `make check`, and a regression test (transient error → retried, not fatal-on-first).

### 4. Prompt changes

Drop the four JSON `Output(...)` constants (`promptJudgeOutput`, `promptCriticOutput`, `promptAutoFixOutput`, `promptReflectionOutput`); add a one-line tool-use instruction per site. System prompts + context builders unchanged.

### 5. `objSchema` extraction (folded into Task 1)

`objSchema` (and `strArrSchema`/`enumArrSchema`) is duplicated in scout + agent tools.go; S4 adds a third copy. Extract to `internal/llm/schema.go` (alongside `toolfield.go`) as exported `llm.ObjSchema` etc.; update scout + agent to use them. ~30-min standalone-change within Task 1.

### 6. Deletion inventory

- The four `prompt*Output` JSON constants.
- The inline anonymous `{reasoning, skip}` struct in `autofix.go`.

KEPT: `Driver.Decide` (autotest/discovery/selftest); `JudgeResult`/`CritiqueResult`/`Reflection`/`Assessment` structs (assembly targets).

## Out of Scope

- `Driver.Decide` removal (stays); `internal/autotest`/`authdiscover`/`protocoldiscover`/`smoke` unchanged.
- Caching of tool-call responses (deferred — low value, needs new cache path).
- Provider parsing (done in S1).

## Testing

| Area | Affected | Files |
|---|---|---|
| `DecideWithTools` retry | new: transient error retried | `internal/ai/driver_tools_test.go` |
| judge/critic/assess/autofix/learner JSON | switch to `SetToolResponse` fixtures | `examiner/*_test.go` |
| assembly | new per-site tests | `examiner/assembly_test.go` |
| critic zero-call refund | new path | `examiner_test.go` |

Live gate: `TestJudge_LiveGLM` (own task) asserts GLM emits a `judge_result` tool call with a valid status.

## Constraints

Go 1.25 pure-Go; `coder/websocket` untouched; no expression/evaluator deps; author `binoctal <binoctal@gmail.com>`, no Co-Authored-By; English; docs only in `cerberus-docs/`; `make check` green; no provider PARSING changes (Task 0 is retry plumbing, not parsing); `Driver.Decide` retained.

## Batched execution

0. **`DecideWithTools` retry parity** (internal/ai) — regression fix; standalone.
1. **`objSchema` extraction + Examiner tool definitions + assemblers** — foundation.
2. **judge + critic → DecideWithTools** — core verdict path + critic zero-call refund test.
3. **Live gate** — `TestJudge_LiveGLM` (separate; live flake doesn't block batch 2).
4. **assess → DecideWithTools** — single `assess_coverage` tool; zero-calls→error.
5. **autofix + learner → DecideWithTools** — `suggest_fix` + `report_reflection`.
6. **Drop JSON Output constants + cleanup.**

Each batch standalone commit + `make check`.
