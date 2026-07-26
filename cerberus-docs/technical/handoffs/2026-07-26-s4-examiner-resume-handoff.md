# S4 Examiner Migration — Resume Handoff — 2026-07-26

> For a new session continuing the S4 (Examiner tool-calling) SDD execution. This
> doc is self-sufficient: it tells you where S4 stands, how to resume the SDD flow,
> and the per-task verified facts so you don't re-research. Authoritative state is
> `git log` + the SDD ledger (see below) — trust those over any stale summary here.

## Big picture

Staged migration migrating cerberus's three heads from drift-prone structured-JSON
(`Driver.Decide`) to tool-calling (`DecideWithTools`):

- **S2 (Scout)** — COMPLETE, on main. All Scout Decide sites → DecideWithTools; drift patches deleted.
- **S3 (Agent)** — COMPLETE, on main. steer + Recovery.Recover + ToT.propose → DecideWithTools; `FallbackParseAction` drift subsystem deleted.
- **S4 (Examiner)** — IN PROGRESS. This handoff. 5 Examiner Decide sites → DecideWithTools.
- Examiner has **no drift-absorption subsystem** (its fallbacks are transient-degradation), so S4 is architectural completion + schema hardening, not drift-elimination.

**`Driver.Decide` is RETAINED** (scope decision): `internal/autotest` (4 code-gen sites), `authdiscover`, `protocoldiscover`, `smoke/selftest` legitimately emit blobs/code, ill-suited to tool-calling. Do NOT remove Driver.Decide.

## How to resume (SDD flow)

1. Invoke skill `superpowers:subagent-driven-development`.
2. Resolve workspace: `bash <skill-dir>/scripts/sdd-workspace cerberus-docs/superpowers/plans/2026-07-26-tool-migration-s4-examiner.md` → prints the ledger dir.
3. **Read the ledger first** (`<workspace>/progress.md`). Tasks with a `Task N: complete` line are DONE — do not re-dispatch. Resume at the first task without one.
4. Read the plan: `cerberus-docs/superpowers/plans/2026-07-26-tool-migration-s4-examiner.md` (T0-T6 + final review). Spec: `cerberus-docs/superpowers/specs/2026-07-26-tool-migration-s4-examiner-design.md`.
5. Per task: dispatch a fresh implementer subagent (model per selection below) with a self-contained prompt (point to the plan task + inline verified facts), then task-review (sonnet), then next task. Continuous execution — no check-ins between tasks.

**Model selection (per SDD):** transcription tasks (T4/T5/T6, small + pattern established) → haiku or sonnet; integration/judgment (T2) → sonnet; live gate (T3) → sonnet; final whole-branch review → opus; fix-loop rounds 4-5 → one tier up.

**Note on briefs:** the SDD `task-brief` script was NOT run for S4; dispatch prompts are self-contained (plan-path + inline facts). Continue that pattern.

## Authoritative state (check these)

- `git log --oneline` — the committed work. S4 commits land on `main` directly (the user works on main).
- SDD ledger: `.superpowers/sdd/2026-07-26-tool-migration-s4-examiner/progress.md` (gitignored scratch — the recovery map; per-task outcomes + deferred minors + verified facts).
- cccmemory keys: `s2-scout-tool-calling-migration-complete`, `s3-agent-tool-calling-migration-complete`, `analyze-drift-degrade-decision`.

## S4 status

- **T0** (`DecideWithTools` retry parity) — ✅ DONE + reviewed (commit `42c5bf4`).
- **T1** (objSchema extract + examiner tools + assemblers) — ✅ DONE + reviewed (commit `a4497cd`).
- **T2** (judge + critic) — ⚠️ IN-FLIGHT at handoff. `git log --oneline a4497cd..HEAD`: if a Task-2 commit exists → review it (review-package `a4497cd..HEAD`, sonnet) then T3; else re-dispatch T2 (brief below).
- **T3-T6 + final review** — pending (briefs below).

## Verified code facts + design (so you don't re-research)

Shared infra (already in place, reuse — do NOT re-create):
- `internal/llm/toolfield.go` — exported `StrField/IntField/NumField/StrSliceField/MapField/BoolField/AnySliceField/MapStringStringField`.
- `internal/llm/schema.go` — exported `ObjSchema(required []any, props)`/`StrArrSchema()`/`EnumArrSchema(vals...)` (conditional `required` inclusion; scout always passes non-empty so behavior preserved).
- `internal/head/examiner/tools.go` — `judgeTools()/criticTools()/assessTools()/autofixTools()/learnerTools()` (Task 1).
- `internal/head/examiner/assembly.go` — `assembleJudge/assembleCritique/assembleAssessment/assembleAutofix/assembleReflections` (Task 1).
- `Driver.DecideWithTools(ctx, prompt, tools)` now retries (T0); returns `*ToolCallResult{ToolCalls, Content, Usage}`.
- `mock.go` has a `"default"` tool-response catch-all + substring matching (S2/S3) — `SetToolResponse("default", []llm.ToolCall{...})` matches any prompt; or key on a prompt substring.
- Live-test pattern: `//go:build live`, `config.Load()`, `t.Skip` if `cfg.LLMAPIKey==""`, `llm.NewClientWithConfig{Model,APIKey,BaseURL,Provider,AuthScheme}`, `ai.NewDriver(client, ai.NewTokenBudget(60000,10000))`, 3-min ctx. API key IS available (live gates ran).

Output structs (assembly targets; schemas already mirror these in Task 1):
- `JudgeResult{Status JudgeStatus; ExistenceConfidence; CorrectnessConfidence; Reasoning; SelfCritique; CritiqueTriggered}` — judge LLM emits the first 4 only.
- `CritiqueResult{IssuesFound bool; Critique; SuggestedStatus JudgeStatus; SuggestedConfidence}`.
- `contract.Assessment{Reached bool; Gaps []Gap; CoveragePct; Reasoning}`, `Gap{Kind; Detail}`.
- autofix inline `{Reasoning string; Skip bool}`.
- `Reflection{Type; Diagnosis; Strategy; ConditionPattern; Category}`.
- `JudgeStatus`: `StatusPass="pass"`, `StatusFail="fail"`, `StatusUncertain="uncertain"`, `StatusSkip="skip"`.

### Task 2 — judge + critic (re-dispatch if no commit)
- `Judge.Evaluate` (`examiner/judge.go`): swap `Decide`+`&judgeResult` for `DecideWithTools(judgeTools())`+`assembleJudge(res.ToolCalls[0])`. Error OR zero calls → `return nil, fmt.Errorf("judge decide: ...")` (caller `examiner.go` maps judge error → `fallbackVerdict`, preserving degradation). Keep `isHighConfidence`/critique-trigger; `SelfCritique`/`CritiqueTriggered` stay code-set.
- `critique` (`judge.go`): `DecideWithTools(criticTools())`+`assembleCritique`. **Error OR zero calls → refund slot (`j.critiqueUsed.Add(-1)`) + return nil (keep initial)** — same as today's error path. Keep `!IssuesFound` + apply-corrections. **Add a critic zero-call refund test** (today only covers error-refund).
- Prompts: drop `promptJudgeOutput`/`promptCriticOutput` (examiner/prompts.go); keep `promptJudgeSystem`/`promptCriticSystem`; add one-line tool-use guidance.
- Migrate `examiner_test.go` JudgeResult/CritiqueResult JSON → `SetToolResponse` tool fixtures (`grep -rn "JudgeResult\|CritiqueResult\|promptJudgeOutput\|promptCriticOutput" internal/head/examiner --include="*_test.go"`).

### Task 3 — live gate (judge)
- New `internal/head/examiner/examiner_live_test.go` (`//go:build live`), `TestJudge_LiveGLM`: build real driver + Judge, run against a synthetic StepResult, assert GLM emits a `judge_result` tool call with `status` ∈ {pass,fail,skip,uncertain}. Run `go test -tags live -run TestJudge_LiveGLM -v ./internal/head/examiner/`; report pass/skip/fail.

### Task 4 — assess
- `Examiner.AssessCoverage` (`assess.go`): `DecideWithTools(assessTools())`+`assembleAssessment(res.ToolCalls[0])`. **Error OR zero calls → `fmt.Errorf("assess coverage: ...")` (propagate — NOT silent degrade; assess feeds the contract gate, so drift must surface, not look like "not reached").** Keep the objective-gate override (`m.Pct < c.CoverageGate.LineThreshold → Reached=false`).

### Task 5 — autofix + learner
- autofix (`autofix.go`): `DecideWithTools(autofixTools())`+`assembleAutofix`. Error/zero → `{Attempted:true, Success:false}` (preserved); `skip:true` → StatusSkip (preserved).
- learner (`learner_run.go`): `DecideWithTools(learnerTools())`+`assembleReflections`. Error → propagate; zero → empty reflections. Keep quality gate.
- Drop `promptAutoFixOutput`/`promptReflectionOutput`; migrate tests.

### Task 6 — cleanup + post-impl
- Confirm the 4 `prompt*Output` constants gone; all 5 examiner sites on DecideWithTools (`grep -rn "\.Decide(" internal/head/examiner --include="*.go" | grep -v _test` → zero).
- Post-impl: `grep -rn "\.Decide(" internal/head --include="*.go" | grep -v _test` → ZERO in scout/agent/examiner; `Driver.Decide` remains only in autotest/authdiscover/protocoldiscover/smoke. `make check` EXIT 0.

### Final whole-branch review
- `review-package` MERGE_BASE=`b7f1ab5` (S4 spec+plan commit) → HEAD; dispatch opus reviewer; triage deferred minors; one fix-wave if needed; then done.

## Global constraints (bind every task)

- Commit author MUST be `binoctal <binoctal@gmail.com>` (repo git config set), NO `Co-Authored-By`/`Co-authored-By` trailer.
- Code comments + commit messages in English. Docs ONLY in `cerberus-docs/`.
- Go 1.25 pure-Go (no CGo); `coder/websocket` untouched; no provider PARSING changes; no new deps.
- `make check` (fmt + lint + test -race) EXIT 0 per task.
- Each task = standalone commit + make check.

## Deferred minors (from T0/T1 reviews; non-blocking, triage at final review)

- T0: one test is a regression guard not strict RED; "llm call with tools:" prefix cosmetic (pre-existing).
- T1: `examiner/assembly.go` could reuse `llm.AnySliceField` for gaps; local `stringFromMap` promote to `llm.StrFromMap` on 2nd consumer.
- (S2/S3 deferred, still open): `objSchema` now extracted (was the 3rd-deferral item — resolved in S4-T1).

## Key gotchas

- `Driver.DecideWithTools` retries as of T0 (`42c5bf4`) — do NOT re-add retry logic at call sites.
- Examiner degradation is graceful (judge→fallbackVerdict, critic→refund+keep, autofix→{Attempted,Success:false}, learner→empty); ONLY assess propagates errors (it feeds the contract gate). Don't homogenize — preserve per-site policy.
- Don't touch `internal/autotest`/`authdiscover`/`protocoldiscover`/`smoke` — they keep using `Driver.Decide`.
