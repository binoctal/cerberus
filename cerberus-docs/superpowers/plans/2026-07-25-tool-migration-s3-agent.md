# Tool-Migration S3 — Agent Action Tool-Calling — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Migrate the two Agent `Decide` call sites (`steer`, `Recovery.Recover`) to `DecideWithTools` via an action tool surface (one tool per runnable action type), delete the `FallbackParseAction` / `actionFromEnvelope` drift subsystem, and migrate the last Scout `Decide` site (`ToT.propose`) so Scout is fully off `Decide`.

**Architecture:** A new `agent/assembly.go` converts a single `llm.ToolCall` → `types.TypedAction` (the same value the executor/rule-engine dispatch on). `steer` and `Recover` swap `Decide`+envelope for `DecideWithTools`+assembly. The keyword-guessing `FallbackParseAction` is obsolete (tool schemas make malformed actions impossible) and is deleted; only a minimal zero-calls default (`WaitAction` for steer, `skip` for recovery) remains. `ToT.propose` swaps `Decide`+`ProposeOutput` for a `propose_strategy` tool.

**Tech Stack:** Go 1.25, `internal/head/agent`, `internal/head/scout`, `internal/ai`, `internal/types`, `internal/llm`, testify.

## Global Constraints

- Go 1.25, pure-Go (no CGo); module `github.com/binoctal/cerberus`
- `coder/websocket` v1.8.14 only (untouched — WS executor unchanged)
- No expression/evaluator deps; no provider implementation changes (parsing done in S1)
- Commit author MUST be `binoctal <binoctal@gmail.com>`, NO Co-Authored-By trailer
- Code comments + commit messages in English
- Docs only in `cerberus-docs/`
- `make check` (fmt + lint + test -race) must be EXIT 0
- `llm.Tool{Name, Description, InputSchema map[string]any}`; `llm.ToolCall{ID, Name, Input map[string]any}`
- **Schema source of truth:** the action tool input schemas MUST match the `TypedAction` struct fields in `internal/types/actions_*.go` (HTTPAction, NavigateAction, WaitAction, ProcessExecAction, FileRead/Write/Exists/GlobAction, CodeAnalyze/Lint/SymbolsAction, BrowserGoto/Click/Fill/EvalAction, WSConnect/Send/Receive/DisconnectAction, DBQuery/AssertAction, GraphQLQueryAction, MCPCallAction). Read those structs for exact field names before writing `actionTools()`.
- Assembly yields the SAME `types.TypedAction` values the executor already dispatches on — the executor, rule engine, and WS executor are NOT modified.

---

### Task 1: Action tool definitions + assembly

**Files:**
- Create: `internal/head/agent/tools.go` — `actionTools() []llm.Tool`
- Create: `internal/head/agent/assembly.go` — `assembleAction(call llm.ToolCall) (types.TypedAction, error)`
- Test: `internal/head/agent/assembly_test.go` (new)

**Interfaces:**
- Consumes: `llm.Tool`, `llm.ToolCall`, `types.TypedAction` + the concrete action structs in `internal/types/actions_*.go`.
- Produces:
  - `func actionTools() []llm.Tool` — **one tool per LLM-reachable action (~14, NOT all 24 ActionTypes)**. Tool name = the `ActionType` string. Input schema = the action struct's fields. Exclude `ws_*`/`code_*`/`db_*`/`graphql_query`/`process_build` (rule-engine/phase-0 domain, never LLM-chosen in `steer` — see spec §1).
  - `func assembleAction(call llm.ToolCall) (types.TypedAction, error)` — maps one tool call to its `TypedAction` over the ~14 names. Unknown name → error. **`skip` is NOT handled here** (it is a control signal handled by `Recover`, Task 3) — `assembleAction(skipCall)` MUST return an error; document this in its godoc and assert it in a test.

- [ ] **Step 1: Write failing tests (RED)**

Create `assembly_test.go`. Cover representative tools across families + the assembly contract:
```go
func TestAssembleAction_APIRequest(t *testing.T) {
	c := llm.ToolCall{Name: "api_request", Input: map[string]any{
		"method": "POST", "url": "/api/users", "body": `{"name":"x"}`,
	}}
	a, err := assembleAction(c)
	require.NoError(t, err)
	ha, ok := a.(types.HTTPAction)
	require.True(t, ok)
	assert.Equal(t, "POST", ha.Method)
	assert.Equal(t, "/api/users", ha.URL)
	assert.Equal(t, `{"name":"x"}`, ha.Body)
}

func TestAssembleAction_BrowserClick(t *testing.T) {
	c := llm.ToolCall{Name: "browser_click", Input: map[string]any{"selector": "#go"}}
	a, err := assembleAction(c)
	require.NoError(t, err)
	assert.Equal(t, types.ActionBrowserClick, a.GetActionType())
}

func TestAssembleAction_ProcessExec(t *testing.T) {
	c := llm.ToolCall{Name: "process_exec", Input: map[string]any{"command": "go", "args": []any{"test", "./..."}}}
	a, err := assembleAction(c)
	require.NoError(t, err)
	pe, ok := a.(types.ProcessExecAction)
	require.True(t, ok)
	assert.Equal(t, "go", pe.Command)
	assert.Equal(t, []string{"test", "./..."}, pe.Args)
}

func TestAssembleAction_UnknownToolErrors(t *testing.T) {
	_, err := assembleAction(llm.ToolCall{Name: "nope", Input: map[string]any{}})
	assert.Error(t, err)
}

// skip is a control signal for Recover, not an action — assembleAction must reject it.
func TestAssembleAction_SkipIsNotAnAction(t *testing.T) {
	_, err := assembleAction(llm.ToolCall{Name: "skip", Input: map[string]any{}})
	assert.Error(t, err)
}
```
Add per-family cases (file_read, file_glob, navigate, wait, mcp_call) until every tool name in `actionTools()` has at least one assembly assertion.

- [ ] **Step 2: Run tests to verify RED** — `go test ./internal/head/agent/ -run TestAssembleAction -v` → `undefined: actionTools/assembleAction`.

- [ ] **Step 3: Create `tools.go` with `actionTools()`**

For each LLM-reachable `ActionType` (spec §1 table: api_request, navigate, wait, process_exec, file_read/write/exists/glob, browser_goto/click/fill/eval, mcp_call), define a tool whose `InputSchema` mirrors the corresponding struct's fields. Use small schema helpers. Field names EXACTLY match the struct JSON tags. Read each `internal/types/actions_*.go` struct before writing its schema.

- [ ] **Step 4: Create `assembly.go` with `assembleAction()`** + **extract shared field helpers**

A `switch call.Name` constructing the matching `TypedAction` from `call.Input`. **Extract the field helpers (`strField`/`numField`/`strSliceField`/`mapStringStringField`, etc.) into a shared package** (`internal/llm/toolfield.go` or `internal/types/assembly_helpers.go`) and reuse from BOTH Scout (`internal/head/scout/assembly.go`) and Agent — do NOT duplicate (Scout already defines them; the Agent must not re-declare, or the linter flags it). Default case → `fmt.Errorf("assembleAction: unknown action tool %q", call.Name)`.

- [ ] **Step 5: Run GREEN** — `go test ./internal/head/agent/ -run TestAssembleAction -v` → PASS.

- [ ] **Step 6: Commit**
```
feat(agent): action tool definitions + assembly

Add actionTools() (one tool per runnable ActionType, schemas mirror the
TypedAction structs) and assembleAction() mapping a single tool call to a
TypedAction. Foundation for migrating steer/recovery off Decide+envelope.
```

---

### Task 2: Wire steer to DecideWithTools

**Files:**
- Modify: `internal/head/agent/executor_steer.go` — `steer` uses `DecideWithTools` + `assembleAction`
- Modify: `internal/head/agent/prompts.go` (or wherever `promptSteerOutput` lives) — drop JSON `Output(...)`; add tool-use line
- Test: `internal/head/agent/executor_steer_test.go` + ReAct loop tests — switch to `SetToolResponse` action-tool fixtures

**Interfaces:**
- Consumes: `actionTools()`, `assembleAction()` (Task 1), `Driver.DecideWithTools`, `llm.NewMockClient` + `SetToolResponse`.
- Produces: `steer` returns `(types.TypedAction, error)` as today, but sourced from `DecideWithTools`.

- [ ] **Step 1: Write failing test (RED)** — a steer test that presets an action tool call via `SetToolResponse` and asserts `steer` returns the assembled `TypedAction`. Confirm RED (steer still uses Decide/envelope).

- [ ] **Step 2: Rewrite `steer`** — replace `Decide` + `SteerOutput.Envelope` + `actionFromEnvelope` with:
  ```go
  res, err := r.driver.DecideWithTools(ctx, prompt, actionTools())
  if err != nil {
      return nil, fmt.Errorf("steer attempt %d: %w", attempt, err) // transient → loop retries
  }
  if len(res.ToolCalls) == 0 {
      r.logger.Warn("steer: zero action tool calls (drift)", zap.Int("attempt", attempt))
      return types.WaitAction{Duration: "1s"}, nil                  // single drift → minimal default (no keyword guessing)
  }
  action, err := assembleAction(res.ToolCalls[0])
  if err != nil {
      return types.WaitAction{Duration: "1s"}, nil                  // malformed (shouldn't happen post-schema) → default
  }
  return action, nil
  ```
  Drop the `isParseError`/`FallbackParseAction` branch. Keep the observation-context + base-URL prompt assembly.

  **Drift → StepSkipped escalation (spec §3):** a single zero-call `steer` returns `WaitAction` (above), but **2 consecutive zero-call steers must finalize the case as `StepSkipped`, not `StepFailed`** (3 empty steers otherwise exhaust `MaxSteerAttempts` and wrongly read as a test failure). Mechanism (pick one, realize in this task): `steer` returns a sentinel error (e.g. `errSteerDrift`) on the 2nd consecutive zero-call — but `steer` is stateless per-attempt, so the ReAct loop (`execute_phases_react_loop.go`) must track consecutive zero-call attempts and, on the 2nd, break out and finalize via `StepSkipped` with a clear reason. Add the loop-level counter + a way for `steer` to signal "this was a zero-call" (e.g. a small `steerResult{Action, ZeroCall}` return, or a sentinel the loop recognizes). Log the drift exhaustion so the Examiner can distinguish it from a real failure. This is the one non-trivial loop change in S3 — call it out in the commit message.

- [ ] **Step 3: Update the steer prompt** — remove `promptSteerOutput` (JSON schema); add "Emit one action tool call for the next step." Keep `promptSteerSystem`.

- [ ] **Step 4: Update tests** — `executor_steer_test.go` and any ReAct-loop test injecting `SteerOutput` JSON switch to `SetToolResponse("...", []llm.ToolCall{{Name:"api_request",...}})`. Migrate via `grep -rn "SteerOutput\|promptSteerOutput" internal --include="*_test.go"`.

- [ ] **Step 5: `make check` + commit**
```
feat(agent): steer via DecideWithTools + action tools

steer emits the next action as a typed tool call assembled to TypedAction,
replacing Decide+ActionEnvelope. Zero tool calls (drift) → WaitAction
default (no keyword guessing); transient LLM error → loop retries.
```

---

### Task 3: Wire Recovery.Recover to DecideWithTools

**Files:**
- Modify: `internal/head/agent/recovery.go` — `Recover` uses `DecideWithTools`
- Modify: `internal/head/agent/tools.go` — add a `skip` tool to `actionTools()` OR a separate `recoveryTools()` (plan pins: add `skip` to the surface)
- Test: `internal/head/agent/recovery_test.go` — tool fixtures

**Interfaces:**
- Produces: `Recover` returns `(RecoverDecision, error)` as today; `Skip=true` when the LLM emits the `skip` tool (or zero calls).

- [ ] **Step 1: Write failing test (RED)** — preset a recovery action tool call → assert `RecoverDecision.Action`; preset `skip` → assert `Skip=true`.

- [ ] **Step 2: Add `skip` tool** to `actionTools()` (input empty; description "abandon this target"). `assembleAction` does NOT handle `skip` (it's a control signal, not an action) — `Recover` inspects `res.ToolCalls[0].Name == "skip"` directly.

- [ ] **Step 3: Rewrite `Recover`** — replace `Decide` + `RecoverOutput.Envelope`/`Skip` with `DecideWithTools`; if first call is `skip` or zero calls → `RecoverDecision{Skip: true}`; else `assembleAction`.

  **Behavioral change (note in commit):** today `RecoverDecision{Action, Skip}` is bidirectional (both can be set; `tryRecovery` short-circuits on `Skip==true`). Post-S3 they are **mutually exclusive** — `skip` tool / zero calls ⇒ `Skip:true` with nil Action; an action tool ⇒ Action with `Skip:false`. Cleaner; add a recovery-test comment documenting the exclusivity.

- [ ] **Step 4: Update prompt + tests** — drop `promptRecoverOutput` JSON; keep `promptRecoverSystem` + L3 memory context. Migrate `recovery_test.go` fixtures.

- [ ] **Step 5: `make check` + commit**
```
feat(agent): recovery via DecideWithTools + action tools

Recover emits either an action tool call or a `skip` control tool, replacing
Decide+RecoverOutput envelope. Zero/skip calls → RecoverDecision{Skip:true}.
```

---

### Task 4: Delete FallbackParseAction / actionFromEnvelope + envelope fields

**Files:**
- Delete: `internal/head/agent/parse_fallback.go` (and `parse_fallback_helpers.go` if serving only keyword guessing)
- Modify: `internal/head/agent/types.go` — delete `SteerOutput`, `RecoverOutput` (or their `Envelope` fields) once unused
- Modify: `internal/types/actions_registry.go` / `actions_base.go` — ONLY if `ActionEnvelope`/`UnmarshalAction`/`MarshalAction` have no storage callers (verify first)
- Test: delete `parse_fallback_test.go` + neighbors

**Interfaces:** None new. After Tasks 2–3, `FallbackParseAction`/`actionFromEnvelope` have no callers.

- [ ] **Step 1: Confirm dead** — `grep -rn "FallbackParseAction\|actionFromEnvelope\|isParseError\|extractFirstLine\|localActionFor\|fallbackKeywords\|checkParseOutputError\|checkJSONUnmarshalError\|checkJSONSyntaxError\|findSubstr" internal --include="*.go" | grep -v _test.go` → the only hits should be inside the files being deleted (and `executor_helpers.go`'s `contains`, which stays).

- [ ] **Step 2: ActionEnvelope scope — RESOLVED (adversarial review verified, no runtime check needed):**
  - DELETE `types.UnmarshalAction` (sole non-test caller was `parse_fallback.go:107`, deleted).
  - DELETE `types.ActionEnvelope.UnmarshalJSON` (`actions_base.go:32-65`) — LLM-drift code (tolerant three-shape coercion; non-Claude models). The LLM-parse path is gone.
  - KEEP `types.MarshalAction` + the `ActionEnvelope` **struct** — sole non-test caller is `examiner/judge.go:171` (serializes the prior action into judge-evidence prompt text, not storage).

- [ ] **Step 3: Delete**:
  - `agent/parse_fallback.go`, `agent/parse_fallback_helpers.go`, `agent/parse_error_helpers.go`.
  - `agent/executor_helpers.go`: delete `isParseError` + `findSubstr`; **KEEP `contains`** (`prompts_test.go` uses it).
  - `types.UnmarshalAction`, `types.ActionEnvelope.UnmarshalJSON`.
  - `internal/types/actions_tools.go` entirely — `ToolDefinitions()` has zero non-test callers (vestigial GLM-era experiment).
  - `SteerOutput`/`RecoverOutput` (if no remaining use after T2/T3) + their tests.
  - Do NOT delete `agent.Deps.UnmarshalJSON` (storage-path tolerance, not drift).

- [ ] **Step 4: `make check` + commit**
```
refactor(agent): delete FallbackParseAction drift subsystem

Tool schemas make malformed/missing-field actions impossible, so the
keyword-guessing FallbackParseAction/actionFromEnvelope (which existed for
non-Claude malformed JSON) is obsolete. Steer/recovery now get a typed
action tool call directly. Also delete types.UnmarshalAction +
ActionEnvelope.UnmarshalJSON (drift), the dead types/actions_tools.go, and
parse_error_helpers/executor_helpers dead helpers (keep contains).
MarshalAction + ActionEnvelope struct stay (Examiner judge prompt). Deps
stays (storage tolerance).
```

---

### Task 5: ToT.propose → propose_strategy tool (finish Scout)

**Files:**
- Modify: `internal/head/scout/tot_generators.go` — `propose` uses `DecideWithTools`
- Modify: `internal/head/scout/tools.go` — add `proposeTools()`
- Modify: `internal/head/scout/assembly.go` (or a propose assembler) — `assembleProposals(calls) []PlanCandidate`
- Modify: `internal/head/scout/types.go` — delete `ProposeOutput`
- Test: `internal/head/scout/tot_test.go` — switch to tool fixture

**Note:** If this task surfaces unexpected complexity (propose's prompt/memory integration), it may slip to S4 without blocking the Agent win — note it and proceed to Task 6.

- [ ] **Step 1: Write failing test (RED)** — preset `propose_strategy{description, cases:["..."]}` tool call → assert assembled `PlanCandidate`.

- [ ] **Step 2: Add `proposeTools()` + assembler** — `propose_strategy{description: string, cases: [string]}`; assembly → `PlanCandidate{Description, Cases}`. (One call = one candidate; multiple calls = multiple candidates.)

- [ ] **Step 3: Rewrite `propose`** — `DecideWithTools(ctx, prompt, proposeTools())` → `assembleProposals(res.ToolCalls)`. Keep the propose prompt's strategy-generation framing + memory context; drop the JSON `Output(...)`.

- [ ] **Step 4: Delete `ProposeOutput`**, update `tot_test.go` fixtures. `make check` + commit.

---

### Task 6: Live gate — steer action emission

**Files:**
- Test: `internal/head/agent/agent_live_test.go` (new, `//go:build live`)

- [ ] **Step 1: Add `TestSteer_LiveGLM`** following the existing `scout_live_test.go` pattern (`config.Load()`, `t.Skip` if no key, build a real driver + minimal ReAct loop / Scout session). Assert GLM emits ≥1 valid action tool call (e.g. `api_request` / `process_exec`) given a concrete observation.

- [ ] **Step 1b (stretch): Add `TestRecover_LiveGLM`** — recovery's prompt changes substantially in T3 (loses the JSON `Output(...)` block) and mixes failure context + L3 memory, so it is the higher-risk migration; mock coverage alone won't catch GLM confusion. Preset a failed result + an injected procedural memory and assert GLM emits either a real action tool call or the `skip` control tool. If time-boxed, this may defer to a follow-up but is recommended.

- [ ] **Step 2: Run** `go test -tags live -run 'TestSteer_LiveGLM|TestRecover_LiveGLM' -v ./internal/head/agent/`. Report pass/skip/fail. If GLM ignores the tool definitions, adjust prompt guidance, not the provider.

---

## Post-implementation verification

- `make check` → EXIT 0.
- `grep -rn "\.Decide(" internal/head --include="*.go" | grep -v _test` → only Examiner sites remain (`examiner/judge.go`, `assess.go`, `autofix.go`, `learner_run.go`). Scout + Agent fully on `DecideWithTools`.
- `grep -rn "FallbackParseAction\|actionFromEnvelope\|ProposeOutput\|isParseError\|UnmarshalAction\|actions_tools\|ToolDefinitions\|checkJSONUnmarshalError" internal --include="*.go"` → zero (except migration-doc comments). Confirms the drift subsystem + dead `actions_tools.go` + drift `UnmarshalJSON` are gone; `MarshalAction`/`ActionEnvelope` struct remain (Examiner).

## Self-Review notes

- **Scope:** Agent (steer+recovery, T1–T4) is the drift-evidenced core; ToT.propose (T5) finishes Scout; Examiner is S4 (transient-degradation fallbacks, verified not drift by adversarial review). `Deps.UnmarshalJSON` + `MarshalAction` + `ActionEnvelope` struct KEPT (storage/prompt use, not drift); `UnmarshalAction` + `ActionEnvelope.UnmarshalJSON` DELETED (drift).
- **Tool surface:** trimmed to ~14 LLM-reachable actions (WS/Code/DB/GraphQL/process_build excluded — rule-engine/phase-0 domain, never LLM-chosen in steer). Matches S2's focused-surface discipline.
- **Drift policy:** malformed-action drift is now impossible (provider schema), so keyword-guessing is deleted; a single zero-call steer → `WaitAction`, 2 consecutive → `StepSkipped` (not `StepFailed`), paralleling S2 spec §4 but loop-safe.
- **Shared helpers:** field helpers extracted to a shared package (Scout + Agent reuse), not duplicated.
- **Executor unchanged:** assembly yields identical `TypedAction` values; no executor/rule-engine/WS-executor edits.
- **Live gates:** steer action-emission probe (T6) + recovery stretch; propose uses mocks.
- **Adversarial review applied:** this plan was revised after an adversarial pass (ActionEnvelope scope resolved, surface trimmed, deletion list expanded, zero-calls escalation added).
