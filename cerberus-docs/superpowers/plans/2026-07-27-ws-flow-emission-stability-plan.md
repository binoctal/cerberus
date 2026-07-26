# ws_flow Emission Stability Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stop empty `ws_flow` cases (a `begin_case` the LLM emitted with zero following `ws_*` calls) from reaching execution, and guide the LLM to emit complete `ws_*` sequences.

**Architecture:** Defense + guidance. `assemblePlan.flush()` drops `ws_flow` cases with `len(Steps)==0` (defense — empty cases cannot survive regardless of LLM behavior). `promptPlanToolGuide` gains an explicit "begin_case must be followed by ws_*" rule plus a generic sequence example (guidance — raises the complete-emission rate).

**Tech Stack:** Go 1.25, `internal/head/scout`, testify.

## Global Constraints

- Go 1.25, pure-Go (no CGo); module `github.com/binoctal/cerberus`
- Commit author MUST be `binoctal <binoctal@gmail.com>`, NO `Co-Authored-By` trailer
- Code comments + commit messages in English
- `make check` (fmt + lint + test -race) must be EXIT 0
- Docs ONLY in `cerberus-docs/`
- `llm.ToolCall{ID, Name, Input map[string]any}`; `agent.TestCase{ID, Name, Action, Target, Method, Body, Expectation, Service, Steps []TestStep, Priority float64, ...}`

---

### Task 1: assembly drops empty ws_flow cases (TDD)

**Files:**
- Modify: `internal/head/scout/assembly.go` — the `flush` closure inside `assemblePlan` (lines ~23-38)
- Test: `internal/head/scout/assembly_test.go` — new test appended

**Interfaces:**
- Consumes: `assemblePlan(calls []llm.ToolCall, goal, baseURL string, services []project.Service) (*agent.TestPlan, map[string]map[string]bool)` — existing package-private function.
- Produces: behavior change only — an empty `ws_flow` case (begin_case + 0 ws_*) no longer appears in `plan.Cases`, and `covered` stays empty for it. Signature unchanged.

- [ ] **Step 1: Write the failing test**

Append to `internal/head/scout/assembly_test.go`:

```go
// TestAssemblePlan_DropsEmptyWSFlowCase asserts the defense side of ws_flow
// emission stability (spec 2026-07-27-ws-flow-emission-stability): a begin_case
// the LLM emitted with NO following ws_* calls must NOT become a 0-step ws_flow
// case — it is dropped, so it cannot waste an Agent cycle or confuse the
// Examiner. (GLM does this non-deterministically; run-2 of the 2026-07-26
// dogfood emitted exactly this.) Contrast TestAssemblePlan_WSRelaySequence,
// which has ws_* after begin_case and must still produce a populated case.
func TestAssemblePlan_DropsEmptyWSFlowCase(t *testing.T) {
	calls := []llm.ToolCall{
		{Name: "begin_case", Input: map[string]any{"name": "relay", "expectation": "bridge gets signal", "service": "ws"}},
		// no ws_* follows — the model opened a case and moved on
	}
	plan, covered := assemblePlan(calls, "g", "", nil)
	assert.Empty(t, plan.Cases, "empty ws_flow case (begin_case with 0 ws_* steps) must be dropped")
	assert.Empty(t, covered, "an empty ws_flow case records no connected roles")
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test -run TestAssemblePlan_DropsEmptyWSFlowCase -v ./internal/head/scout/`
Expected: FAIL — `plan.Cases` has length 1 (the empty ws_flow case is currently appended), so `assert.Empty` fails.

- [ ] **Step 3: Write minimal implementation**

In `internal/head/scout/assembly.go`, edit the `flush` closure so that an empty ws_flow is dropped before append. Current:

```go
	flush := func() {
		if open != nil {
			if open.Service != "" {
				for _, st := range open.Steps {
					if st.Action == "ws_connect" && st.Role != "" {
						if covered[open.Service] == nil {
							covered[open.Service] = map[string]bool{}
						}
						covered[open.Service][st.Role] = true
					}
				}
			}
			cases = append(cases, *open)
			open = nil
		}
	}
```

Change to (insert the empty-ws_flow guard as the first thing inside `if open != nil`):

```go
	flush := func() {
		if open != nil {
			if open.Action == "ws_flow" && len(open.Steps) == 0 {
				// A begin_case the LLM opened with no following ws_* calls.
				// Drop it: a 0-step ws_flow case is not a real case — it would
				// waste an Agent cycle and confuse the Examiner. (Defense side
				// of ws_flow emission stability; the prompt handles guidance.)
				open = nil
				return
			}
			if open.Service != "" {
				for _, st := range open.Steps {
					if st.Action == "ws_connect" && st.Role != "" {
						if covered[open.Service] == nil {
							covered[open.Service] = map[string]bool{}
						}
						covered[open.Service][st.Role] = true
					}
				}
			}
			cases = append(cases, *open)
			open = nil
		}
	}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test -run TestAssemblePlan_DropsEmptyWSFlowCase -v ./internal/head/scout/`
Expected: PASS. Also re-run the neighboring WS tests to confirm no regression:
`go test -run 'TestAssemblePlan_WSRelaySequence|TestAssemblePlan_WSDroppedWithoutBeginCase' -v ./internal/head/scout/`
Expected: both PASS (a begin_case + ws_* sequence still produces a populated ws_flow case; ws_* without begin_case still dropped).

- [ ] **Step 5: `make check` + commit**

Run: `make check` — EXIT 0.
Commit message: `fix(scout): drop empty ws_flow cases (begin_case with 0 ws_* steps)`

---

### Task 2: prompt strengthens ws_* sequence rule + adds a generic example

**Files:**
- Modify: `internal/head/scout/prompts.go` — `promptPlanToolGuide` (the ws_* block around lines 75-86)

**Interfaces:**
- Consumes: none (const string).
- Produces: a strengthened `promptPlanToolGuide` const. No signature change; no test (prose).

- [ ] **Step 1: Add the rule + example**

In `promptPlanToolGuide`, the `Rules:` section currently ends with:

````
- For multi-party WS relay (two or more protocol roles exchanging messages), emit begin_case followed by the ordered ws_connect/ws_send/ws_receive sequence — do NOT also emit single-role ws_connect cases the relay already covers.
- Omit JSON; the tool schemas enforce structure.`
````

Insert one new rule between those two lines (before the `Omit JSON` line):

````
- A begin_case MUST be immediately followed by the ws_* steps of the choreography: at least one ws_connect per role, then ws_send/ws_receive. A bare begin_case with no following ws_* produces no case (the planner drops it). Example relay sequence: begin_case -> ws_connect web -> ws_connect bridge -> ws_send web <type> -> ws_receive bridge <type> -> ws_disconnect web -> ws_disconnect bridge.
````

(The `<type>` placeholders keep the example generic so the model does not over-fit a specific message type like `session:start`.)

- [ ] **Step 2: `make check`**

Run: `make check` — EXIT 0 (the change is inside a const string; nothing recompiles beyond `prompts.go`, but the full suite confirms no accidental breakage).

- [ ] **Step 3: Commit**

Commit message: `docs(scout): prompt nudge — begin_case must be followed by ws_* steps`

---

### Task 3: live-gate reference check (non-deterministic, not a hard gate)

**Files:**
- Test (run only, no edit): `internal/head/scout/scout_live_test.go` — `TestScoutRelayEmission_Live`

**Interfaces:** none (verification only).

- [ ] **Step 1: Run the live probe**

Run: `go test -tags live -run TestScoutRelayEmission_Live -v ./internal/head/scout/`
Requires the GLM API key in `.claude/settings.json` (shared with Claude Code).

- [ ] **Step 2: Inspect the categorized output**

Confirm: the probe's `categories` summary shows `ws_flow` cases (complete, multi-step), and NO empty ws_flow case appears (a ws_flow with 0 steps would now be dropped before it ever reaches the plan, so it cannot appear). If the run emits at least one complete ws_flow choreography, the guidance side is working for that run.

- [ ] **Step 3: Report, do not commit**

This is a reference check only — GLM is non-deterministic, so a single run is not proof. Report pass/skip/fail and the category breakdown. No commit (no code change). If two consecutive runs both produce only empty ws_flow cases (i.e. every ws_relay attempt is dropped), that is a prompt-tuning signal — surface it, do not silently proceed.

---

## Post-implementation verification

- `make check` → EXIT 0.
- `go test -run TestAssemblePlan -v ./internal/head/scout/` → all assembly tests green, including the new drop test and the existing WSRelaySequence / WSDroppedWithoutBeginCase tests.
- `TestScoutRelayEmission_Live` ran (reference).

## Self-Review notes

- **Scope:** defense (assembly drop) + guidance (prompt). Explicitly out of scope: single-tool ws_flow schema (reverts S2), Scout retry, step-completeness validation, the deterministic device:online relay regression.
- **Drop condition** is strictly `Action=="ws_flow" && len(Steps)==0`. A ws_flow with steps but missing ws_connect is NOT dropped (Examiner judges it).
- **No-log defense:** the drop is pure; observability is the unit test + the dogfood trace (empty cases stop appearing).
