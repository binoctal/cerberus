# Tool-Migration S3 — Agent Action Tool-Calling — Design — 2026-07-25

## Background

S2 migrated every Scout `Decide` call site (except `ToT.propose`) to `DecideWithTools`
via a two-tier tool surface, deleting Scout's drift-absorption patches. The same
drift disease remains in the **Agent** head, where it is actually more severe: the
Agent runs a ReAct loop that asks the LLM for the *next action* on every step, so a
malformed action aborts or stalls a case mid-execution.

drift is evidenced by absorption code, exactly as in S2:

| drift point | expected shape | actual LLM output | absorption code |
|---|---|---|---|
| Agent action (steer/recovery) | `{"action":{"type":"http_request","action":{...}}}` envelope → `TypedAction` | malformed JSON / known type missing required fields ("common with non-Claude models") | `FallbackParseAction` scans raw text for keywords ("get","click",...) and guesses an action (`parse_fallback.go`); `actionFromEnvelope` treats *any* `UnmarshalAction` failure as fallback-eligible |
| steer parse | valid `SteerOutput` JSON | unparseable | `executor_steer.go:37-40` `isParseError` → `FallbackParseAction` |
| recovery parse | valid `RecoverOutput` JSON | unparseable | `recovery.go:61-63` → skip |

The `FallbackParseAction` keyword table (`navigate`, `api_request`, `get/post/put/...`,
`click`, `type/fill`, `wait`, `goto` + `localActionFor` for file/process) is a
literal map of the action types the LLM *should* have emitted as structured data.
Tool-calling makes each action a typed tool whose schema is hard-enforced by the
provider — the "missing required fields / malformed JSON" failure mode becomes
structurally impossible, and the keyword-guessing fallback is obsolete.

## Goal

Migrate the **two Agent `Decide` call sites** (`steer`, `Recovery.Recover`) to
`DecideWithTools` via an action tool surface (one tool per runnable action type),
delete the `FallbackParseAction` / `actionFromEnvelope` drift subsystem, and migrate
the last remaining Scout `Decide` site (`ToT.propose`, deferred from S2) so Scout is
fully off `Decide`.

The three call sites (verified by `grep '\.Decide(' internal/head --include=*.go`):
1. `agent/executor_steer.go:36` — `steer` → next action (ReAct loop)
2. `agent/recovery.go:61` — `Recovery.Recover` → recovery action or skip
3. `scout/tot_generators.go:23` — `ToT.propose` → `ProposeOutput` (S2-deferred)

## Scope decision: why Agent is S3 and Examiner is S4

drift-evidence-driven, the same methodology as S2.

- **Agent → S3.** Two Decide sites parse an LLM `ActionEnvelope` into a `TypedAction`
  and are backed by a dedicated drift-absorption subsystem (`FallbackParseAction`,
  `actionFromEnvelope`, `parse_fallback_helpers.go`) whose own comments name
  non-Claude models as the cause. This is the strongest remaining drift evidence in
  the codebase.
- **Examiner → S4.** Its five Decide sites (`judge` ×2, `assess`, `autofix`,
  `learner_run`) parse small JSON shapes (`Assessment`, `[]Reflection`,
  `{reasoning,skip}`, `judgeResult`). Their "fallback" code is **transient
  degradation** — `fallbackVerdict` (Judge LLM unavailable → use execution status),
  checker-only Level-2 downgrade — NOT drift absorption for malformed structured
  output. There is no tolerant `UnmarshalJSON`, no keyword-guessing subsystem.
  Drift evidence is weak; Examiner migrates in S4 alongside removing `Driver.Decide`
  project-wide.
- **ToT.propose → S3.** No drift evidence (S2 deferred it for exactly this reason),
  minimal `{description, cases[]}` schema. Included in S3 only to finish Scout; if it
  proves non-trivial it may slip to S4 without blocking the Agent win.

## Design

### 1. Action tool surface (LLM-reachable actions only, ~14 tools)

The `internal/types/actions_*.go` registry enumerates 24 `ActionType` values, but
**most are never LLM-chosen in `steer`** — they are constructed deterministically by
the rule engine / phase-0 runner before the ReAct loop ever runs. Adversarial review
traced the execution paths:

- **WSConnect/Send/Receive/Disconnect** — built from `TestCase.Steps` (`TestStep`),
  run in phase 0 (`execute_phases.go`); WS cases never enter the ReAct loop.
- **CodeAnalyze/Lint/Symbols** — built entirely from `tc.Language` in `rules_code.go`.
- **DBQuery/DBAssert, GraphQLQuery** — built in `database.go` / `rules_file_other.go`;
  never selected by the LLM in `steer`.

So the LLM tool surface is trimmed to the **~14 general-purpose actions the LLM can
actually pick** when the rule engine did not match a case (the ReAct-loop fallback
path). This matches S2's focused-surface discipline (Scout `planTools()` was 11) and
keeps GLM tool-selection reliable. (Schemas mirror the struct fields in
`internal/types/actions_*.go` — read each struct before writing its tool def.)

| Family | Tools (input = action struct fields) | LL count |
|---|---|---|
| HTTP | `api_request{method, url, body?, headers?, timeout?}` | 1 |
| Navigate/Wait | `navigate{url, wait_selector?, wait_for?}`, `wait{duration?, selector?, wait_for_state?}` | 2 |
| Process | `process_exec{command, args?, work_dir?, env?, timeout?}` (build is code-constructed) | 1 |
| File | `file_read{path, offset?, limit?}`, `file_write{path, content, ...}`, `file_exists{path}`, `file_glob{pattern, path?}` | 4 |
| Browser | `browser_goto{url, wait_until?}`, `browser_click{selector, text?, button?, modifiers?}`, `browser_fill{selector, value}`, `browser_eval{expression, args?}` | 4 |
| MCP | `mcp_call{...}` (kept — plausibly LLM-chosen for unmatched cases) | 1 |

Excluded from the LLM surface (rule-engine/phase-0 domain, reached by code paths
that do not go through `steer`): all `ws_*`, `code_*`, `db_*`, `graphql_query`,
`process_build`. These stay fully runnable — they just are not offered as `steer`
tools.

Assembly rule: **one DecideWithTools call → exactly one action tool call → one
`TypedAction`.** A new `agent/assembly.go` maps the emitted tool call to its
`TypedAction` via a switch over the ~14 tool names. The executor and rule engine are
unchanged — assembly produces the same `TypedAction` values they already dispatch on.

### 2. Call-site mapping

#### steer (executor_steer.go)
`steer` calls `DecideWithTools(ctx, prompt, actionTools())`; assembly turns the
returned tool call into the next `TypedAction`. The prompt drops the
`promptSteerOutput` JSON-schema section and the `ActionEnvelope` framing; it keeps
the ReAct observation context and the service base-URL hint.

#### Recovery.Recover (recovery.go)
`Recover` calls `DecideWithTools`. `skip` becomes either a dedicated `skip` tool (no
action) or a flag derived from "no action tool call emitted"; the plan pins which.
Procedural-memory (L3) context injection is unchanged.

#### ToT.propose (scout/tot_generators.go)
`propose` calls `DecideWithTools` with a `propose_strategy` tool
(`{description, cases: [string]}`) instead of `Decide` + `ProposeOutput` JSON. Small.

### 3. Drift / fallback policy (parallels S2 spec §4)

With tool-calling, the "malformed JSON / missing required fields" failure mode is
gone — so `FallbackParseAction`'s keyword-guessing (whose entire purpose was
recovering usable structure from unstructured text) is obsolete and is deleted.

What remains is the **zero-tool-calls** case (the LLM emits nothing actionable) and
the **transient LLM error** case. Treated differently, as in S2:

- **Zero tool calls (drift/quality)** — the ReAct loop must not stall, and must not
  mask drift as a hard failure. Adversarial review traced the loop: `WaitAction` is
  an intermediate step (never terminal), so three empty `steer`s today exhaust
  `MaxSteerAttempts` and finalize as **`StepFailed`**. Policy: a single zero-call
  `steer` returns the deterministic `WaitAction` default (the existing safe default
  for empty content, `parse_fallback.go:40`); after **2 consecutive zero-call
  steers**, `steer` signals drift and the case finalizes as **`StepSkipped`** (not
  `StepFailed`), matching recovery's zero-call semantics and letting the Examiner
  distinguish drift from a real test failure. The mechanism (sentinel error or flag
  returned by `steer`, tracked across attempts by the ReAct loop) is pinned in the
  plan; the loop logs the exhaustion. No keyword guessing. Trade-off acknowledged:
  this is safer than `FallbackParseAction` (never mis-guesses) but makes no
  accidental progress — accepted because the rule engine already handles the cases
  that have a deterministic action.
- **Transient LLM call error** (rate-limit / 5xx / budget / network) — return the
  error to the loop, which already retries/attempts. Unchanged from today's
  non-parse-error branch.

The distinction is made the same way as S2: nil error + zero `ToolCalls` = drift
path; non-nil client error = transient path.

### 4. Deletion inventory

Deleted in S3 (adversarial review resolved the open questions):
- `agent/parse_fallback.go` — `FallbackParseAction`, `actionFromEnvelope`,
  `fallbackKeywords`, `localActionFor`, `extractFirstLine`; plus
  `parse_fallback_helpers.go` (keyword-guessing helpers), `parse_error_helpers.go`
  (`checkParseOutputError`/`checkJSONUnmarshalError`/`checkJSONSyntaxError`), and
  `executor_helpers.go`'s `isParseError` + `findSubstr` (whose only caller after T2
  is the deleted steer branch). **Keep `contains`** in `executor_helpers.go` —
  `prompts_test.go` still uses it.
- `SteerOutput` / `RecoverOutput` (or their `Envelope` fields) and the `isParseError`
  fallback branches in `executor_steer.go` / `recovery.go`.
- `types.UnmarshalAction` — sole non-test caller was `parse_fallback.go:107`
  (deleted). Deletable.
- `types.ActionEnvelope.UnmarshalJSON` (`actions_base.go:32-65`) — this is LLM-drift
  code (tolerant three-shape coercion; its docstring names non-Claude models). The
  LLM-parse path is gone, so it is deleted alongside the Agent drift subsystem.
- `internal/types/actions_tools.go` — `ToolDefinitions()` has **zero non-test
  callers** (a vestigial GLM-era experiment, incompatible with the `llm.Tool` shape).
  Delete in T4.
- `scout` `ProposeOutput` (replaced by the `propose_strategy` tool assembly).

Kept (NOT drift — adversarial review verified):
- `types.MarshalAction` + the `ActionEnvelope` **struct** — `MarshalAction`'s sole
  non-test caller is `examiner/judge.go:171`, which serializes the prior action into
  judge-evidence prompt text (not storage). Keep both.
- `agent.Deps.UnmarshalJSON` — dual-shape tolerance for `TestCase.DependsOn`, used
  by code-constructed WS-step dependencies (`ws_cases.go:139`) and stored-plan
  deserialization. **Not LLM drift; stays.**

### 5. Prompt changes

Each call site's prompt drops its `Output(JSON-schema)` section and gains a one-line
tool-use instruction ("emit one action tool call for the next step"). The ReAct
system prompt and observation formatting are unchanged.

## Out of Scope

- Examiner head (judge/critic/assess/autofix/learner) — S4.
- Removing `Driver.Decide` project-wide — S4 (Examiner + any residual).
- Provider implementation (done in S1).
- Action executor / rule engine / WS executor — unchanged (assembly yields the same
  `TypedAction` values).
- `agent.Deps` tolerance and `ActionEnvelope` storage format — kept.

## Testing

| Area | Affected | Files |
|---|---|---|
| `FallbackParseAction` / `actionFromEnvelope` | tests deleted alongside the code | `agent/parse_fallback_test.go`, `*_test.go` neighbors |
| steer / recovery Decide+envelope | switch to `SetToolResponse` action-tool fixtures | `agent/executor_steer_test.go`, `agent/recovery_test.go`, ReAct loop tests |
| Action assembly | new per-tool assembly tests (each tool → correct `TypedAction`) | `agent/assembly_test.go` (new) |
| `ProposeOutput` | switch to tool fixture | `scout/tot_test.go` |

New coverage: each action tool gets an assembly test (tool call → `TypedAction` with
correct fields); a zero-tool-calls test asserting the deterministic `WaitAction`
default (steer) / `skip` (recovery); a ranking/loop test confirming the ReAct loop
still terminates.

Live gate: one live GLM probe for `steer` (asserts GLM emits a valid action tool
call, e.g. `api_request`, against a real observation) — the action-emission analog
of S2's `TestScoutPlan_LiveGLM`. ToT.propose / recovery use mock + httptest.

## Constraints

Go 1.25 pure-Go; `coder/websocket` untouched; no expression/evaluator deps; author
`binoctal <binoctal@gmail.com>`, no Co-Authored-By; English; docs only in
`cerberus-docs/`; `make check` green; no provider changes.

## Batched execution (sizes uneven — stated honestly)

1. **Action tool definitions + assembly** — `agent/tools.go` (actionTools), `agent/assembly.go` (assembleAction). Foundation.
2. **steer → DecideWithTools** — wire + delete FallbackParseAction use in steer.
3. **recovery → DecideWithTools** — wire + delete its envelope path.
4. **Delete FallbackParseAction / actionFromEnvelope + drift helpers + `ActionEnvelope.UnmarshalJSON`; update prompts** — `ActionEnvelope` storage scope resolved by adversarial review (keep `MarshalAction` + struct for Examiner judge prompt; delete `UnmarshalAction` + the tolerant `UnmarshalJSON`; delete dead `actions_tools.go`).
5. **ToT.propose → propose_strategy tool** — finish Scout. Small; may slip to S4 if non-trivial.
6. **Live gate** — `steer` action-emission probe.

Each batch is a standalone commit + `make check`. ToT.propose stays on `Decide` only
if it proves non-trivial (deferred to S4, not blocking).
