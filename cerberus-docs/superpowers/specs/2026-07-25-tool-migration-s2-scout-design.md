# Tool-Migration S2 — Scout 双层 Tool-Calling — Design — 2026-07-25

> Revised after adversarial review: corrected the call-site count (4 → 7,
> adding `contract.go`), aligned tool enums to the executor's actual dispatch,
> pinned three assembly details, re-scoped fallback, and replaced the test
> section with a real impact inventory.

## Background

S1 made the mock client tool-aware and locked provider tool-parse coverage, but
**no head calls `DecideWithTools`** — Scout still uses `Driver.Decide` (structured
JSON output) at every LLM call site. This is the root cause of GLM drift.

drift is not abstract — it is evidenced by code written to absorb it:

| drift point | expected shape | actual LLM output | absorption code |
|---|---|---|---|
| `tech_stack` | `["go","make"]` | `[{"language":"go","confidence":1.0}]` | `flexibleStrings`+`firstStringValue` (`scout/types.go`) |
| `ws_relay` body | valid `relayIntent` JSON | role typo / missing step / `<2` roles / unknown role | 6-layer validate + silent drop (`ws_relay.go`) |
| `PlanOutput` | closed `{"cases":[{8 fields}]}` JSON | missing field / unclosed JSON / bad nesting | `ParseStructuredOutput` fail → `fallbackPlan` |
| `priorities` | `{"high":["go/build"]}` (map→[]string) | `{"health_check":"critical"}` (map→string) | `Priorities.UnmarshalJSON` dual-shape (`contract/types.go`) |

Why drift happens: "emit JSON matching this schema" is a **soft constraint**
(prompt text); larger JSON drifts more, GLM especially. tool-calling makes the
schema a **hard constraint** (provider enforces tool-call input) and breaks one
large JSON into many small typed calls — drift becomes structurally impossible.

## Goal

Migrate **all seven** Scout `Decide` call sites to `DecideWithTools` via a
two-tier tool surface, delete every drift-absorption patch, and remove the
secondary LLM round-trips that exist only to compensate for structured-output
brittleness.

The seven call sites (verified by `grep '\.Decide(' internal/head/scout`):
1. `directPlanning` / `runAIPlanning` → `PlanOutput{Cases[]}` (main plan path)
2. `Analyze` / `runAIInference` → `AnalyzeOutput{Endpoints,Pages,TechStack}`
3. `verifyServiceAttribution` → `ServiceAttributionCorrections` (post-hoc fix)
4. `ToT.evaluate` / `aiScore` → `EvaluateOutput` (deep plan scoring) — **deterministicized**
5. `BuildCoverageContract` → `contract.Contract` (coverage standard)
6. `SelfAssessContract` → `{Notes[]}` (contract critique)
7. `ToT.propose` → `ProposeOutput` — **deferred to S3** (no drift evidence; minimal schema)

## Design

### 1. Two-tier tool surface (the core idea)

The current architecture is already mixed: most cases are high-level TestCases
with no `Steps` (executor rule-engine expands them), while `ws_relay`/`ws_cases`
produce TestCases **with `Steps`** (low-level choreography). The two-tier surface
lets the LLM pick the tier per case instead of forcing one.

| Tier | Tools | When the LLM uses it |
|---|---|---|
| **High-level intent** (one call = one TestCase/Contract item) | `test_http_endpoint`, `check_invariant`, `run_process`, `analyze_code`, `check_file`, `navigate`, `report_endpoint`, `report_page`, `declare_tech`, contract tools, `report_contract_gap` | Single-step semantic assertion / single contract item |
| **Low-level step** (bare operation) | `ws_connect`, `ws_send`, `ws_receive`, `ws_disconnect` | Multi-step choreography (WS; extensible) |
| **Grouping boundary** | `begin_case{name, expectation}` | Marks "the following step calls belong to one TestCase" |

Assembly rules:
- A high-level intent tool call → one TestCase with no `Steps` (or one contract
  field, for contract tools).
- `begin_case` followed by low-level step calls, up to the next `begin_case` /
  high-level tool / end-of-stream → one TestCase **with `Steps`**, and that
  TestCase's `Action` is set to **`"ws_flow"`** (so `filterWSEndpointDrift`'s
  `isWSAction` recognizes it; a bare/`ws_relay` action would be silently dropped).
- This maps 1:1 onto the existing `TestCase{Steps?}` structure, so the executor
  rule-engine and step runner need **zero changes** (verified against
  `rules_http/browser/code/file_other/process` + `execute_phases_steps.go`).

Minimal overlap: low-level step tools exist **only for multi-step choreography**
(WS connect/send/recv). http/file/process are one-shot cases served by
high-level tools — there is no `http_request` step tool to collide with
`test_http_endpoint`.

### 2. Call-site mapping

#### directPlan — main plan path

| Tool | Input fields | Produces |
|---|---|---|
| `test_http_endpoint` | `method, path, body?, service?, expect_status?, expect_body?` | http TestCase `{Method, Target:path, Body, Expectation}` (no `Action`; executor dispatches on Method) |
| `check_invariant` | `invariant_id? \| (description, assertion?)` | invariant TestCase `{Target:description, Expectation:description, Severity?}` — **executed via Steer** (no executor rule keys on invariants today; not a regression) |
| `run_process` | `action: build\|exec, cmd?, expect?` | TestCase `{Action: process_build\|process_exec}` (**only build/exec** — executor dispatches these two; test/lint go through `exec` with `cmd:"go test ./..."`, mirroring `GenerateExecutorCases`) |
| `analyze_code` | `action: analyze\|lint\|symbols, target?` | TestCase `{Action: code_analyze\|code_lint\|code_symbols}` (**all three** — `rules_code.go` dispatches all three; dropping `analyze` is a coverage regression) |
| `check_file` | `action: exists\|read\|glob, path?, pattern?, expect?` | TestCase `{Action: file_exists\|file_read\|file_glob}` |
| `navigate` | `path, expect?` | TestCase `{Action: navigate, Target:path}` |
| `begin_case` | `name, expectation` | opens a Steps TestCase (`Action:"ws_flow"`) |
| `ws_connect` | `role, url?` | Step `{Action:ws_connect, ConnectionID:role, Role:role}` |
| `ws_send` | `role, type` | Step `{Action:ws_send, ConnectionID:role, Message:{"type":type}}` (assembly wraps `type` in `{"type":...}` to match `wsSendBody`) |
| `ws_receive` | `role, type?, aliases?, timeout?, assert?` | Step `{Action:ws_receive, ConnectionID:role, Type, Asserts, Timeout}` |
| `ws_disconnect` | `role` | Step `{Action:ws_disconnect, ConnectionID:role}` |

- `TestCase.ID` is **generated by code** (`tc-001` in emission order), never by
  the LLM — removes one drift vector.
- `expect_status?/expect_body?` are formatted by assembly into the single
  `Expectation` string (e.g. `"status 200; body contains X"`).
- WS relay authoring: the LLM emits `begin_case` + a ws_* sequence directly. The
  `relayIntent` body-JSON disappears; the intent **is** the tool-call sequence.

#### Analyze — three high-level tools, one call per item
- `report_endpoint{method, path, confidence?}`
- `report_page{path, confidence?}`
- `declare_tech{stack: [string]}`  ← kills `flexibleStrings` (schema enforces string array)

#### verifyServiceAttribution — DELETED
`attributeService` (path-prefix match) runs in assembly for **every** case and
**overrides** any LLM-tagged `service` when it returns a non-empty, disagreeing
result — so wrong attribution is corrected deterministically without an LLM
round-trip (closing the gap the reviewer flagged: the old `attributeService`
only filled empty values). When `attributeService` returns `""` (no prefix
match), the LLM-tagged value (or `Services[0]`) stands. Trade-off accepted:
path-prefix matching is coarser than LLM semantic judgment, but it is
deterministic, drift-free, and removes a full LLM round-trip.

#### ToT
- `propose` → **deferred to S3** (no drift evidence; `ProposeOutput` is a minimal
  `{description, cases[]}` schema). ToT keeps `Decide` for propose in S2.
- `evaluate` → **deterministicized** (§3). The `aiScore` LLM call and
  `evaluateDriver.Decide` are removed.

#### contract.go — two call sites (NEW in scope)
- `BuildCoverageContract` → six contract tools, one per field, so each field's
  schema is hard-enforced:
  - `declare_scope{modules: [string]}`
  - `declare_path_types{types: [happy|alternative|boundary|edge]}`
  - `declare_error_scope{scopes: [4xx|validation|exception]}`
  - `declare_boundaries{boundaries: [empty|zero|max|invalid|extreme]}`
  - `set_priority{bucket, modules: [string]}`  ← schema forces `[]string`, kills
    `Priorities.UnmarshalJSON` dual-shape patch
  - `set_coverage_gate{module, line_threshold?, branch_threshold?}`
  Assembly builds the `contract.Contract` from the call set; `Depth` and
  config-carried `Invariants` are still applied in code (unchanged).
- `SelfAssessContract` → `report_contract_gap{note}` tool (one call per gap
  note), replacing `{Notes[]}` JSON. (`SelfAssessContract` notes are diagnostic-
  only today, logged; that contract is preserved.)

### 3. ToT evaluate deterministicization

Current scoring (`tot_evaluators.go:45`): `Score = AIScore/10*0.7 + Coverage*0.3`.
`AIScore` (LLM, 70%) measured strategy quality; `Coverage` (deterministic, 30%)
is endpoint path string-matching. Removing `AIScore` and leaving `Coverage` alone
would collapse ranking to a 30% weak signal.

Replacement: a **multi-signal deterministic score**, each signal normalized to
[0,1], combined by weighted sum. Weight ranges (to be pinned in the plan):
- endpoint coverage — weight **0.30–0.40** (existing `coverageScore`)
- invariant coverage — weight **0.20–0.30** (case text vs `model.InvariantHints`)
- page coverage — weight **0.10–0.15** (case text vs `model.Navigation.Pages`)
- action diversity — weight **0.15–0.25** (distinct action categories)
- goal keyword overlap — weight **0.10–0.15** (case text vs goal tokens)
- `case-count adequacy` is **dropped** (reviewer: it incentivizes padding).
Weights sum to 1.0; exact values fixed in plan. `AIScore`/`aiScore()`/
`evaluateDriver` removed; `reasoning` is replaced by a deterministic per-signal
breakdown for logging (a deliberate observability change, accepted).

**Fail-safe guard retained:** today `evaluate` returns an error when *every*
candidate's AI score fails (`tot_evaluators.go:55-57`) — a systemic-failure
signal. The deterministic replacement preserves an analogue: if the top
candidate's composite `Score` is below a floor (e.g. all signals near 0 → the
propose step produced nothing actionable), `evaluate` returns an error instead
of silently ranking near-random candidates. Threshold pinned in plan.

**Trade-off:** deterministic signals are less nuanced than LLM judgment. Accepted
because ToT is optional (`deepPlan` defaults off) and the win is stable,
reproducible, drift-free, zero-LLM-cost scoring.

### 4. Fallback policy — drift降级 砍, 瞬态失败 保留

Two distinct failure modes, treated differently (reviewer-caught conflation):
- **Zero tool calls / unparseable intent** (drift or quality problem) → Scout
  returns an error directly (`fmt.Errorf("scout %s: zero tool calls", phase)`).
  No deterministic case-generation mask. Problems surface.
- **LLM call error** (transient: rate limit / 5xx / budget exhausted / network)
  → `fallbackPlan` (directPlan) / config-only model (Analyze) **retained** as the
  degradation path. A rate-limited CI run still produces a plan from the model;
  it does not abort the session.

Assembly distinguishes these by inspecting the `DecideWithTools` return: a nil
error with zero `ToolCalls` is the drift path (error); a non-nil error from the
client is the transient path (fallback).

### 5. augmentPlan aftermath

`augmentPlan` (`plan_phases.go:55-62`) currently does three things; migration impact:
- `expandWSRelayCases` — **deleted**. Its "body-JSON → Steps" job vanishes (LLM
  authors Steps directly). Its validation (declared/dup/valid role) and
  `covered`-roles computation **move into the assembly layer**.
- **`covered` data flow (reviewer-flagged):** `covered` is produced during
  assembly (inside directPlan) and consumed by `WSCasesCovered` (inside
  `augmentPlan`, which runs after directPlan). `directPlan` is changed to return
  `(TestPlan, covered)`, and `Scout.Plan` threads `covered` into `augmentPlan`.
  (`TestPlan` gains no field — `covered` is a separate return value.)
- **`fillBody` retained:** assembly calls `fillBody` (POST/PUT/PATCH bodies from
  `service.BodyTemplate` when the LLM omits `body`) exactly as `convertPlanOutput`
  does today. `service.BodyTemplate` stays live config.
- `appendExecutorCases` — process/code cases are now LLM-authored via high-level
  tools; the deterministic `GenerateExecutorCases` supplement is dropped.
  `WSCasesCovered` (protocol-derivable connect/relay cases) is **retained** as a
  deterministic complement, consuming `covered` as above.
- `filterWSEndpointDrift` — **retained** as cheap insurance; correct here because
  assembly sets `Action:"ws_flow"` (§1) so legitimate WS cases survive.

### 6. Prompt changes

Each call site's prompt drops its `Output(...)` JSON-schema section and gains a
tool-definitions section (the tool names + input schemas above) plus guidance:
"use high-level tools for single-step cases; use `begin_case` + ws_* steps only
for multi-step connection choreography". Embedded prompt templates
(`internal/prompts` + `internal/head/scout` prompt constants) updated accordingly.

## Out of Scope

- Agent / Steer / Examiner heads (S3 / S4).
- `ToT.propose` (deferred to S3 — no drift evidence).
- Provider implementation (done in S1; S2 adds no provider code).
- `Driver.Decide` itself — other heads still use it; removing `Decide`
  project-wide is an S4 follow-up once all heads migrate.

## Testing

Real impact inventory (replaces placeholder). Tests asserting deleted code are
removed alongside it; tests that inject JSON via helpers need new tool-call
fixtures, not just deletion.

| Area | Affected tests | Files |
|---|---|---|
| `flexibleStrings`/TechStack | 3 | `analyze_techstack_test.go`, `scout_test.go` |
| `relayIntent` / ws_relay body | ~8 (5 core + 3 neighbor) | `ws_relay_test.go`, `scout_live_test.go`, `llm/mock_test.go`, `ai/driver_test.go` |
| `verifyServiceAttribution` | 4 | `direct_planning_test.go` |
| `fallbackPlan` (drift path) | 6+ | `direct_planning_test.go` (prefix helper), `scout_test.go`, `session/lifecycle_test.go` — **transient-path tests stay**, drift-path tests go |
| `AnalyzeOutput` | 6 | `analyze_techstack_test.go`, `scout_test.go` |
| `PlanOutput` | 8 direct + ~15 session helpers | `scout_test.go`, `tot_test.go`, `direct_planning_test.go`, `smoke/*`, `session/lifecycle_test.go`, `session/contract_integration_test.go` — helpers switch to tool-call fixtures |
| `ProposeOutput` | 4 (stay — propose deferred) | `tot_test.go` |
| `Priorities.UnmarshalJSON` | enumerate in plan | `contract/*_test.go` |
| `expandWSRelayCases` | 5 | `ws_relay_test.go` |
| `GenerateExecutorCases` | 7 | `plan_executor_test.go`, `smoke/dogfood_test.go` |
| `augmentPlan`/`appendExecutorCases` | 2 | `plan_phases_test.go` |
| `aiScore`/`AIScore`/`evaluateDriver` | **0 existing** | — **deterministic evaluate is net-new test work** |
| `BuildCoverageContract`/`SelfAssessContract` | enumerate in plan | `contract/*_test.go`, `session/contract_integration_test.go` |

New coverage required: each tool gets a `SetToolResponse` preset test (high-level
→ one TestCase; `begin_case`+ws_* → one Steps TestCase); assembly table tests (ID
generation, service override, Steps ordering, `Action:"ws_flow"`); contract
assembly; deterministic-evaluate per-signal unit tests + a ranking-regression test.

Live gate: one live GLM probe for directPlan (asserts GLM emits `test_http_endpoint`
+ a `begin_case`/ws_* sequence as multiple tool calls) and one for
`BuildCoverageContract` (asserts the six contract tools). Analyze / ToT use
mock + httptest.

## Constraints

Go 1.25 pure-Go; `coder/websocket` v1.8.14 only (untouched); no
expression/evaluator deps; author `binoctal <binoctal@gmail.com>`, no
Co-Authored-By; English; docs only in `cerberus-docs/`; `make check` green.

Batched execution (sizes are uneven — stated honestly):
1. **directPlan (large)** — two-tier assembly + Step validation + `covered`
   wiring + `fillBody` + `Action:"ws_flow"` + `run_process`/`analyze_code` enum
   fixes. The biggest batch; standalone commit + `make check` + live gate.
2. **Analyze** — three tools, delete `flexibleStrings`. Small.
3. **contract.go** — six contract tools + `report_contract_gap`, delete
   `Priorities.UnmarshalJSON`. Medium; live gate on `BuildCoverageContract`.
4. **verifyServiceAttribution deletion** — assembly `attributeService` override.
   Small.
5. **ToT evaluate deterministicization** — multi-signal score + fail-safe guard.
   Medium; net-new tests.

Each batch is a standalone commit + `make check`. ToT.propose stays on `Decide`
until S3.
