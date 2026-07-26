# Tool-Migration S4 — Examiner Verdict Tool-Calling — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development (recommended) or superpowers:executing-plans. Steps use checkbox (`- [ ]`).

**Goal:** (0) Restore `DecideWithTools` retry parity with `Decide` (fixes a latent S2/S3 regression + enables S4); (1–6) migrate the five Examiner `Decide` sites to `DecideWithTools` via per-site tool surfaces, preserving graceful degradation. `Driver.Decide` RETAINED (autotest/discovery/selftest).

**Architecture:** Task 0 wraps `DecideWithTools`'s `client.Complete` in `executeWithRetry`. A new `internal/head/examiner/assembly.go` maps tool call(s) → existing output structs, reusing `llm.*Field`. Each Examiner site swaps `Decide`+JSON for `DecideWithTools`+assembly.

**Tech Stack:** Go 1.25, `internal/ai`, `internal/head/examiner`, `internal/head/contract`, `internal/llm`, testify.

## Global Constraints

- Go 1.25, pure-Go (no CGo); module `github.com/binoctal/cerberus`
- `coder/websocket` v1.8.14 only (untouched); no expression/evaluator deps; no provider PARSING changes (Task 0 is retry plumbing, explicitly allowed)
- Commit author MUST be `binoctal <binoctal@gmail.com>`, NO Co-Authored-By trailer
- Code comments + commit messages in English; docs only in `cerberus-docs/`
- `make check` (fmt + lint + test -race) must be EXIT 0
- `llm.Tool{Name, Description, InputSchema map[string]any}`; `llm.ToolCall{ID, Name, Input map[string]any}`
- **Schema source of truth:** mirror output-struct JSON tags in `examiner/types.go` (JudgeResult/CritiqueResult/Reflection) + `contract/types.go` (Assessment/Gap).
- Reuse `llm.StrField`/`NumField`/`BoolField`/`StrSliceField` (`internal/llm/toolfield.go`).
- `Driver.Decide` RETAINED for `internal/{autotest,authdiscover,protocoldiscover,smoke}`.

---

### Task 0: `DecideWithTools` retry parity (regression fix)

**Files:**
- Modify: `internal/ai/driver_tools.go` — wrap `client.Complete` in `executeWithRetry`
- Test: `internal/ai/driver_tools_test.go` — transient error is retried (not fatal-on-first)

**Context:** `Driver.Decide` (driver_decide.go) wraps its call in `executeWithRetry(d.retry.MaxRetries, d.retry.BaseDelay, d.retry.MaxDelay, ...)` + caches. `DecideWithTools` (driver_tools.go:12-35) does NEITHER — bare `client.Complete`. This lost retry for every S2/S3 migrated site (Scout falls back on first transient; Agent partially compensated via MaxSteerAttempts). Task 0 restores retry. Caching tool responses is DEFERRED (low hit-rate; needs tool-call-aware cache path).

- [ ] **Step 1: RED** — a test with a mock client that fails N-1 times then succeeds, asserting `DecideWithTools` retries and returns the success (today it fails on the first error). Use the existing retry test pattern from `driver_decide` tests.
- [ ] **Step 2: Wrap** `client.Complete` in `executeWithRetry(...)` matching `Decide`'s usage (the inner func returns tokens + error; on success `budget.Record(tokens)`). Keep the budget check + `Tools: tools` request field.
- [ ] **Step 3: GREEN** — retry test passes; existing `TestDriver_DecideWithTools*` tests still pass.
- [ ] **Step 4: `make check` + commit** — `fix(ai): DecideWithTools retries transient errors (parity with Decide)`. Note: retroactively restores retry for Scout/Agent migrated sites.

---

### Task 1: `objSchema` extraction + Examiner tool definitions + assemblers

**Files:**
- Create: `internal/llm/schema.go` — exported `ObjSchema`, `StrArrSchema`, `EnumArrSchema` (moved from scout/agent tools.go)
- Modify: `internal/head/scout/tools.go`, `internal/head/agent/tools.go` — use `llm.*Schema`, delete local copies
- Create: `internal/head/examiner/tools.go` — `judgeTools()/criticTools()/assessTools()/autofixTools()/learnerTools()`
- Create: `internal/head/examiner/assembly.go` — 5 assemblers
- Test: `internal/head/examiner/assembly_test.go` (new)

- [ ] **Step 1: Extract `objSchema`** to `internal/llm/schema.go`; update scout + agent to `llm.ObjSchema` etc.; `make check` confirms behavior-preserving (pure refactor, like the toolfield extraction in S3-T1).
- [ ] **Step 2: RED** — `assembly_test.go` covering each assembler (judge_result→JudgeResult; critique_verdict→CritiqueResult; assess_coverage{reached,gaps:[2]}→Assessment; suggest_fix{skip:false}→{reasoning,skip}; 2× report_reflection→[]Reflection) + unknown-tool-error.
- [ ] **Step 3: `examiner/tools.go`** — 5 `*Tools()` funcs; schemas per spec §1 (judge: 4 fields, no self_critique; assess: single tool with nested gaps, no coverage_pct; enums for status/type).
- [ ] **Step 4: `examiner/assembly.go`** — 5 assemblers via `llm.*Field`; assess collects the single tool's nested gaps; learner collects N report_reflection calls.
- [ ] **Step 5: GREEN + `make check` + commit** — `feat(examiner): verdict tool definitions + assembly; extract objSchema to llm`.

---

### Task 2: judge + critic → DecideWithTools

**Files:**
- Modify: `internal/head/examiner/judge.go` — judge site (`Evaluate`) + critic site (`critique`) use `DecideWithTools` + assembly
- Modify: `internal/head/examiner/prompts.go` — drop `promptJudgeOutput` + `promptCriticOutput`; add tool-use lines
- Test: `internal/head/examiner/examiner_test.go` + `examiner/*_test.go` — JSON fixtures → `SetToolResponse`

- [ ] **Step 1: RED** — judge test presetting `judge_result` → assembled verdict; critic test presetting `critique_verdict`.
- [ ] **Step 2: Rewrite judge site** — `DecideWithTools(judgeTools())` → `assembleJudge(res.ToolCalls[0])`; error OR zero calls → existing `fallbackVerdict` path. Keep `isHighConfidence`/critique-trigger logic. (`SelfCritique`/`CritiqueTriggered` still set by the code path, not the LLM.)
- [ ] **Step 3: Rewrite critic site** — `DecideWithTools(criticTools())` → `assembleCritique`; error OR zero calls → refund slot (`critiqueUsed.Add(-1)`) + return nil (keep initial), as today. **Add a test for the zero-call refund path** (today's tests only cover the error-refund).
- [ ] **Step 4: Prompts** — drop `promptJudgeOutput`/`promptCriticOutput`; keep system prompts + evidence context.
- [ ] **Step 5: Migrate tests** — `grep -rn "JudgeResult\|CritiqueResult\|promptJudgeOutput\|promptCriticOutput" internal/head/examiner --include="*_test.go"` → `SetToolResponse` fixtures.
- [ ] **Step 6: `make check` + commit** — `feat(examiner): judge + critic via DecideWithTools`.

---

### Task 3: Live gate — `TestJudge_LiveGLM`

**Files:**
- Test: `internal/head/examiner/examiner_live_test.go` (new, `//go:build live`)

- [ ] **Step 1:** Follow `scout_live_test.go` pattern (`config.Load()`, `t.Skip` if no key, real driver + Judge). Run against a synthetic StepResult; assert GLM emits a `judge_result` tool call with `status` ∈ {pass/fail/skip/uncertain}.
- [ ] **Step 2:** `go test -tags live -run TestJudge_LiveGLM -v ./internal/head/examiner/`. Report pass/skip/fail. `make check` EXIT 0.
- [ ] **Step 3: Commit** — `test(examiner): live gate for judge verdict emission`.

---

### Task 4: assess → DecideWithTools

**Files:**
- Modify: `internal/head/examiner/assess.go` — `AssessCoverage` uses `DecideWithTools` + `assembleAssessment`
- Test: `internal/head/examiner/*_test.go`

- [ ] **Step 1: RED** — assess test presetting `assess_coverage{reached:false, gaps:[{kind,detail}]}` → assembled Assessment.
- [ ] **Step 2: Rewrite assess** — `DecideWithTools(assessTools())` → `assembleAssessment(res.ToolCalls[0])`. **Error OR zero calls → `fmt.Errorf("assess coverage: ...")` (propagate, as today; zero-calls = drift → error, NOT silent degrade — spec §2).** Keep the objective-gate override (`m.Pct < threshold → Reached=false`).
- [ ] **Step 3: Migrate tests + `make check` + commit** — `feat(examiner): assess via DecideWithTools + assess_coverage tool`.

---

### Task 5: autofix + learner → DecideWithTools

**Files:**
- Modify: `internal/head/examiner/autofix.go`, `learner_run.go`
- Modify: `internal/head/examiner/prompts.go` — drop `promptAutoFixOutput` + `promptReflectionOutput`
- Test: `internal/head/examiner/*_test.go`

- [ ] **Step 1: RED** — autofix (`suggest_fix{skip:false}`); learner (2× `report_reflection`).
- [ ] **Step 2: Rewrite autofix** — `DecideWithTools(autofixTools())` → `assembleAutofix`; error/zero → `{Attempted:true, Success:false}`; `skip:true` → StatusSkip (preserved).
- [ ] **Step 3: Rewrite learner** — `DecideWithTools(learnerTools())` → `assembleReflections`; error → propagate; zero → empty. Keep quality gate.
- [ ] **Step 4: Drop `promptAutoFixOutput`/`promptReflectionOutput`; migrate tests; `make check` + commit** — `feat(examiner): autofix + learner via DecideWithTools`.

---

### Task 6: Drop JSON Output constants + final cleanup

- [ ] **Step 1: Confirm** — `grep -rn "promptJudgeOutput\|promptCriticOutput\|promptAutoFixOutput\|promptReflectionOutput" internal/head/examiner --include="*.go"` → zero. `grep -rn "\.Decide(" internal/head/examiner --include="*.go" | grep -v _test` → zero (all 5 on DecideWithTools).
- [ ] **Step 2: Post-impl** — `grep -rn "\.Decide(" internal/head --include="*.go" | grep -v _test` → zero in scout/agent/examiner. `Driver.Decide` remains only in autotest/authdiscover/protocoldiscover/smoke. `make check` EXIT 0.
- [ ] **Step 3: Commit** (if cleanup) — `refactor(examiner): finalize tool-calling prompts`.

---

## Post-implementation verification

- `make check` → EXIT 0.
- `grep -rn "\.Decide(" internal/head --include="*.go" | grep -v _test` → ZERO (Scout+Agent+Examiner on DecideWithTools).
- `Driver.Decide` retained for `internal/{autotest,authdiscover,protocoldiscover,smoke}` (7 callers).
- `DecideWithTools` now retries (Task 0) — regression test green.
- Live gate `TestJudge_LiveGLM` ran vs real GLM.

## Self-Review notes

- **Scope:** Examiner only (5 sites). Driver.Decide RETAINED. Drift-evidence-consistent (Examiner has no drift absorption).
- **Task 0:** `DecideWithTools` retry parity — fixes latent S2/S3 regression (transient errors no longer fatal-on-first for migrated sites). Caching tool responses deferred.
- **assess:** single `assess_coverage` tool with nested gaps (not split); zero-calls→error (not silent degrade — protects the contract gate).
- **judge schema:** 4 fields (no self_critique — faithful to today's prompt).
- **autofix:** skip as a field (single object; differs from S3 recovery's separate skip-tool, explained).
- **objSchema:** extracted to `llm` in Task 1 (third consumer = the moment).
- **Live gate:** judge (Task 3, separate from judge+critic batch).
