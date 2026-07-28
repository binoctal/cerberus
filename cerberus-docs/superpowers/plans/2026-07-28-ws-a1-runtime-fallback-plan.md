# A1 Runtime WS-Flow Fallback (Phase 2) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** When a sound LLM `ws_flow` case fails at execution, activate a pre-emitted lazy deterministic relay fallback for the role it covered, so the role is recovered instead of stranded.

**Architecture:** Scout emits, alongside a sound LLM `ws_flow` covering a relay receiver, a lazy fallback case (a copy of the deterministic relay case) tagged `FallbackFor=<primaryID>` + `Priority<0`. The Agent pre-scans these into a `fallbacksByPrimary` index, skips them by default, and runs one only when its bound primary returns a non-environmental `StepFailed`. The primary keeps its `fail`; a passing fallback sets `Recovered=true`. Examiner is untouched.

**Tech Stack:** Go 1.25, `internal/head/agent` (`types.go`, `executor_run.go`, `parallel_execute*.go`), `internal/head/scout` (`assembly.go`, `ws_cases.go`, `plan_phases.go`, `direct_planning.go`), testify, `internal/types` (`ErrorResult`, `IsEnvironmentalFailure`).

## Global Constraints

- Go 1.25, pure-Go (no CGo); module `github.com/binoctal/cerberus`
- Commit author MUST be `binoctal <binoctal@gmail.com>`, NO Co-Authored-By trailer
- Code comments + commit messages in English
- Docs only in `cerberus-docs/`
- `make check` (fmt + lint + test -race) must be EXIT 0
- Follow existing comment density/naming idiom
- Spec: `cerberus-docs/superpowers/specs/2026-07-28-ws-a1-runtime-fallback-design.md`

## File Structure

- `internal/head/agent/types.go` — `TestCase.FallbackFor`, `StepResult.Recovered`
- `internal/head/scout/assembly.go` — `assemblePlan` also returns `coveringCase`
- `internal/head/scout/direct_planning.go` — `runAIPlanning`/`directPlan` thread `coveringCase`
- `internal/head/scout/plan_phases.go` — `executeDirectPlanning`/`Plan`/`augmentPlan`/`appendExecutorCases` thread `coveringCase`
- `internal/head/scout/ws_cases.go` — `WSCasesCovered` consumes `coveringCase`, emits lazy fallback
- `internal/head/agent/executor_run.go` — serial pre-scan + activation + `isEnvironmental`
- `internal/head/agent/parallel_execute.go` — parallel pre-scan + activation in `executeAndStore`

---

### Task 1: Data model — `TestCase.FallbackFor` + `StepResult.Recovered`

**Files:**
- Modify: `internal/head/agent/types.go` (TestCase struct ~line 22; StepResult struct ~line 90)
- Test (new): `internal/head/agent/fallback_types_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `TestCase.FallbackFor string` (omitempty), `StepResult.Recovered bool`. Later tasks set/read these.

- [ ] **Step 1: Write the failing tests (RED)**

Create `internal/head/agent/fallback_types_test.go`:

```go
package agent

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTestCase_FallbackForRoundTrip(t *testing.T) {
	tc := TestCase{ID: "tc-001", FallbackFor: "tc-000"}
	b, err := json.Marshal(tc)
	require.NoError(t, err)
	var got TestCase
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, "tc-000", got.FallbackFor, "FallbackFor round-trips")

	// omitempty: an empty FallbackFor is absent from the JSON.
	eb, err := json.Marshal(TestCase{ID: "tc-002"})
	require.NoError(t, err)
	assert.NotContains(t, string(eb), "fallback_for", "empty FallbackFor is omitted")
}

func TestStepResult_RecoveredZero(t *testing.T) {
	var r StepResult
	assert.False(t, r.Recovered, "Recovered zero value is false")
	r.Recovered = true
	assert.True(t, r.Recovered)
}
```

- [ ] **Step 2: Run tests to verify RED**

Run: `go test ./internal/head/agent/ -run 'TestTestCase_FallbackForRoundTrip|TestStepResult_RecoveredZero' -v`
Expected: COMPILE ERROR — `TestCase.FallbackFor undefined` / `StepResult.Recovered undefined`.

- [ ] **Step 3: Add the fields**

In `internal/head/agent/types.go`, in the `TestCase` struct, add immediately after the `Steps` field:

```go
	Steps       []TestStep `json:"steps,omitempty"` // Deterministic multi-step WebSocket flow
	// FallbackFor is the ID of the primary case this case is a lazy fallback for
	// (A1 Phase 2). Empty on normal cases. The Agent skips a lazy fallback by
	// default and activates it only when its primary case fails at execution.
	FallbackFor string `json:"fallback_for,omitempty"`
```

In the same file, in the `StepResult` struct, add immediately after the `Error` field:

```go
	Error    error
	// Recovered is true when this result is a lazy fallback case that ran because
	// its primary case failed, and the fallback passed (A1 Phase 2). The primary
	// case's own result stays a fail; this marks the role recovered, not passed.
	Recovered bool
```

- [ ] **Step 4: Run tests to verify GREEN**

Run: `go test ./internal/head/agent/ -run 'TestTestCase_FallbackForRoundTrip|TestStepResult_RecoveredZero' -v`
Expected: PASS.

- [ ] **Step 5: Regression — full agent package**

Run: `go test ./internal/head/agent/ -count=1`
Expected: PASS. New optional fields do not change existing construction.

- [ ] **Step 6: Commit**

```bash
git add internal/head/agent/types.go internal/head/agent/fallback_types_test.go
git -c user.name='binoctal' -c user.email='binoctal@gmail.com' commit -m "feat(agent): TestCase.FallbackFor + StepResult.Recovered fields

Add the data model for A1 Phase 2 runtime fallback: a TestCase may declare
itself a lazy fallback for a primary case (FallbackFor), and a StepResult may
record that it recovered a role the primary case stranded (Recovered). Both
fields are optional/omitempty; no behavior is wired yet."
```

---

### Task 2: Scout — produce `coveringCase` and thread it to `WSCasesCovered`

**Files:**
- Modify: `internal/head/scout/assembly.go` (`assemblePlan` ~line 17)
- Modify: `internal/head/scout/direct_planning.go` (`runAIPlanning` ~line 62, `directPlan` ~line 110)
- Modify: `internal/head/scout/plan_phases.go` (`executeDirectPlanning` ~line 50, `Plan` ~line 67, `augmentPlan` ~line 58, `appendExecutorCases` ~line 96)
- Modify: `internal/head/scout/ws_cases.go` (`WSCasesCovered` signature ~line 42, `WSCases` wrapper ~line 18)
- Test: `internal/head/scout/ws_relay_test.go` (append)

**Interfaces:**
- Consumes: `llmWSFlowSound` (existing, Task 1 of the Phase 1 plan), `agent.TestCase.ID`.
- Produces: `assemblePlan` now returns `(*agent.TestPlan, map[string]map[string]bool, map[string]map[string]string)` — the third value is `coveringCase` (svc → role → covering sound case ID). `WSCasesCovered` gains a `coveringCase map[string]map[string]string` parameter (consumed in Task 3).

- [ ] **Step 1: Write the failing test (RED)**

Append to `internal/head/scout/ws_relay_test.go`:

```go
// TestAssemblePlan_RecordsCoveringCase is the A1 Phase 2 producer: when a
// sound LLM ws_flow covers a role, assemblePlan records that case's ID in
// coveringCase, so WSCasesCovered (Task 3) can emit a lazy fallback bound to
// it. An unsound case still records nothing (Phase 1 behavior).
func TestAssemblePlan_RecordsCoveringCase(t *testing.T) {
	cfg := &project.Config{Services: []project.Service{{Name: "rt", URL: "ws://h/ws", Protocol: relayProtocol()}}}

	sound := []llm.ToolCall{
		{Name: "begin_case", Input: map[string]any{"name": "relay", "expectation": "ok", "service": "rt"}},
		{Name: "ws_connect", Input: map[string]any{"role": "web"}},
		{Name: "ws_receive", Input: map[string]any{"role": "web", "type": "device:online"}}, // grounded
	}
	plan, _, covering := assemblePlan(sound, "goal", "ws://h/ws", cfg.Services)
	require.NotEmpty(t, plan.Cases)
	primaryID := plan.Cases[0].ID
	assert.Equal(t, primaryID, covering["rt"]["web"], "sound case ID recorded as web's coverer")

	unsound := []llm.ToolCall{
		{Name: "begin_case", Input: map[string]any{"name": "relay", "expectation": "ok", "service": "rt"}},
		{Name: "ws_connect", Input: map[string]any{"role": "web"}},
		{Name: "ws_receive", Input: map[string]any{"role": "web", "type": "message"}}, // invented
	}
	_, _, coveringUnsound := assemblePlan(unsound, "goal", "ws://h/ws", cfg.Services)
	assert.Empty(t, coveringUnsound["rt"]["web"], "unsound case records no coverer")
}
```

- [ ] **Step 2: Run test to verify RED**

Run: `go test ./internal/head/scout/ -run 'TestAssemblePlan_RecordsCoveringCase' -v`
Expected: COMPILE ERROR — `covering` (3rd return) undefined; `assemblePlan` returns 2 values.

- [ ] **Step 3: Produce `coveringCase` in `assemblePlan`**

In `internal/head/scout/assembly.go`, change the signature and body. Replace the function signature line and the `covered := …` line:

```go
func assemblePlan(calls []llm.ToolCall, goal, baseURL string, services []project.Service) (*agent.TestPlan, map[string]map[string]bool, map[string]map[string]string) {
	var cases []agent.TestCase
	covered := map[string]map[string]bool{}
	// A1 Phase 2: side table mirroring covered, carrying the ID of the sound
	// LLM case that covered each (svc, role), so WSCasesCovered can emit a lazy
	// fallback bound to it. covered stays bool; this adds only the binding.
	coveringCase := map[string]map[string]string{}
```

In the same function, in `flush()`, inside the `if llmWSFlowSound(open, svcProtos[open.Service], goal) {` block, replace the covered-marking inner block with one that also records the coverer:

```go
				if llmWSFlowSound(open, svcProtos[open.Service], goal) {
					for _, st := range open.Steps {
						if st.Action == "ws_connect" && st.Role != "" {
							if covered[open.Service] == nil {
								covered[open.Service] = map[string]bool{}
							}
							covered[open.Service][st.Role] = true
							// A1 Phase 2: record which sound case covered this
							// role, so WSCasesCovered can bind a lazy fallback.
							if coveringCase[open.Service] == nil {
								coveringCase[open.Service] = map[string]string{}
							}
							coveringCase[open.Service][st.Role] = open.ID
						}
					}
				}
```

Change the final `return` line of `assemblePlan` to return the side table:

```go
	return &agent.TestPlan{Goal: goal, Cases: cases, ProjectURL: baseURL}, covered, coveringCase
}
```

- [ ] **Step 4: Thread `coveringCase` through the planning chain**

In `internal/head/scout/direct_planning.go`:

`runAIPlanning` signature → add a third return value:

```go
func (s *Scout) runAIPlanning(ctx context.Context, prompt string, goal string, model *project.ProjectModel) (*agent.TestPlan, map[string]map[string]bool, map[string]map[string]string, error) {
```

In its body, update every return:
- `return fb, map[string]map[string]bool{}, nil` → `return fb, map[string]map[string]bool{}, map[string]map[string]string{}, nil`
- `return &agent.TestPlan{}, map[string]map[string]bool{}, nil` → add `, map[string]map[string]string{}`
- `plan, covered := assemblePlan(...)` → `plan, covered, coveringCase := assemblePlan(...)`
- `return plan, covered, nil` (both occurrences) → `return plan, covered, coveringCase, nil`

`directPlan` signature and body:

```go
func (s *Scout) directPlan(ctx context.Context, goal string, model *project.ProjectModel) (*agent.TestPlan, map[string]map[string]bool, map[string]map[string]string, error) {
	memory := s.buildEpisodicContext(ctx, goal, model)
	prompt := s.buildPlanningPrompt(ctx, goal, model, memory)
	return s.runAIPlanning(ctx, prompt, goal, model)
}
```

In `internal/head/scout/plan_phases.go`:

`executeDirectPlanning`:

```go
func (s *Scout) executeDirectPlanning(ctx context.Context, goal string, model *project.ProjectModel) (*agent.TestPlan, map[string]map[string]bool, map[string]map[string]string, error) {
	return s.directPlan(ctx, goal, model)
}
```

`Plan` — update the covered declaration, both branches, and the augment call. Replace the block:

```go
	var plan *agent.TestPlan
	var covered map[string]map[string]bool
	var err error
	if s.deepPlan {
		plan, err = s.executeDeepPlanning(ctx, goal, model, memory)
		covered = map[string]map[string]bool{}
	} else {
		plan, covered, err = s.executeDirectPlanning(ctx, goal, model)
	}

	if err != nil {
		return nil, err
	}

	// Phase 3: Augment plan with executor cases
	s.augmentPlan(plan, goal, covered)
```

with:

```go
	var plan *agent.TestPlan
	var covered map[string]map[string]bool
	var coveringCase map[string]map[string]string
	var err error
	if s.deepPlan {
		plan, err = s.executeDeepPlanning(ctx, goal, model, memory)
		covered = map[string]map[string]bool{}
		coveringCase = map[string]map[string]string{}
	} else {
		plan, covered, coveringCase, err = s.executeDirectPlanning(ctx, goal, model)
	}

	if err != nil {
		return nil, err
	}

	// Phase 3: Augment plan with executor cases
	s.augmentPlan(plan, goal, covered, coveringCase)
```

`augmentPlan`:

```go
func (s *Scout) augmentPlan(plan *agent.TestPlan, goal string, covered map[string]map[string]bool, coveringCase map[string]map[string]string) {
	s.appendExecutorCases(plan, goal, covered, coveringCase)
	filterWSEndpointDrift(plan, s.config) // Finding-3: drop WS-endpoint HTTP drift
}
```

`appendExecutorCases` — add the parameter and pass it to `WSCasesCovered`:

```go
func (s *Scout) appendExecutorCases(plan *agent.TestPlan, goal string, covered map[string]map[string]bool, coveringCase map[string]map[string]string) {
	rootDir := s.config.Code.Root
	if rootDir == "" {
		rootDir = "."
	}
	info := DetectProjectType(rootDir)
	cases := GenerateExecutorCases(info, goal)
	cases = append(cases, WSCasesCovered(s.config, goal, covered, coveringCase)...)
```

In `internal/head/scout/ws_cases.go`, add the `coveringCase` parameter to `WSCasesCovered` (not yet consumed — Task 3 consumes it):

```go
func WSCasesCovered(cfg *project.Config, goal string, covered map[string]map[string]bool, coveringCase map[string]map[string]string) []agent.TestCase {
```

Update the `WSCases` compatibility wrapper (~line 18) to pass `nil`:

```go
	return WSCasesCovered(cfg, goal, nil, nil)
```

- [ ] **Step 5: Run the targeted test to verify GREEN**

Run: `go test ./internal/head/scout/ -run 'TestAssemblePlan_RecordsCoveringCase' -v`
Expected: PASS.

- [ ] **Step 6: Regression — full scout package**

Run: `go test ./internal/head/scout/ -count=1`
Expected: PASS. Existing `covered` behavior is unchanged; `coveringCase` is produced but not yet consumed. If any existing test calls `assemblePlan`/`directPlan`/`runAIPlanning`/`executeDirectPlanning`/`augmentPlan`/`appendExecutorCases`/`WSCasesCovered` with the old arity, update those call sites to the new signatures (the `WSCasesCovered` callers in `_test.go` gain a `nil` argument).

- [ ] **Step 7: Commit**

```bash
git add internal/head/scout/assembly.go internal/head/scout/direct_planning.go internal/head/scout/plan_phases.go internal/head/scout/ws_cases.go internal/head/scout/ws_relay_test.go
git -c user.name='binoctal' -c user.email='binoctal@gmail.com' commit -m "feat(scout): produce coveringCase side table (A1 Phase 2 producer)

assemblePlan now also returns coveringCase (svc -> role -> ID of the sound LLM
case that covered it), threaded through directPlan/runAIPlanning/
executeDirectPlanning/Plan/augmentPlan/appendExecutorCases to WSCasesCovered.
covered stays bool; only the binding is added. WSCasesCovered accepts the
parameter but does not consume it yet (Task 3)."
```

---

### Task 3: `WSCasesCovered` consumes `coveringCase`, emits lazy fallback

**Files:**
- Modify: `internal/head/scout/ws_cases.go` (`WSCasesCovered` relay-drop block ~line 60-65)
- Test: `internal/head/scout/ws_relay_test.go` (append)

**Interfaces:**
- Consumes: `coveringCase map[string]map[string]string` (from Task 2), `agent.TestCase.FallbackFor` (Task 1).
- Produces: when a relay receiver is covered by a sound LLM case, `WSCasesCovered` emits a lazy fallback case (`FallbackFor` set, `Priority = -1`) instead of dropping the deterministic relay case.

- [ ] **Step 1: Write the failing test (RED)**

Append to `internal/head/scout/ws_relay_test.go`:

```go
// TestWSCasesCovered_LazyFallbackForCoveredReceiver is the A1 Phase 2 emitter:
// a relay receiver covered by a sound LLM case gets a lazy deterministic
// fallback bound to that case (FallbackFor set, Priority<0), not a normal
// case and not a drop. An uncovered receiver still emits a normal relay case.
func TestWSCasesCovered_LazyFallbackForCoveredReceiver(t *testing.T) {
	cfg := &project.Config{Services: []project.Service{{Name: "rt", URL: "ws://h/ws", Protocol: relayProtocol()}}}
	covered := map[string]map[string]bool{"rt": {"web": true}}
	coveringCase := map[string]map[string]string{"rt": {"web": "tc-l lm-primary"}}

	cases := WSCasesCovered(cfg, "receive devices:sync", covered, coveringCase)

	// web is the relay receiver (optional handshake await_type device:online in
	// relayProtocol). Find the case whose receiver (first connect step) is web.
	var webRelay *agent.TestCase
	for i := range cases {
		c := &cases[i]
		if len(c.Steps) > 0 && c.Steps[0].Action == "ws_connect" && c.Steps[0].Role == "web" {
			webRelay = c
			break
		}
	}
	require.NotNil(t, webRelay, "web relay case present")
	assert.Equal(t, "tc-l lm-primary", webRelay.FallbackFor, "bound to the covering case")
	assert.Less(t, webRelay.Priority, 0.0, "lazy fallback is deprioritized")
	assert.NotEmpty(t, webRelay.Steps, "fallback carries the deterministic relay steps")

	// Sanity: a receiver with no coverer is emitted as a normal case (no FallbackFor).
	coveringNone := map[string]map[string]string{"rt": {}}
	casesNone := WSCasesCovered(cfg, "receive devices:sync", map[string]map[string]bool{"rt": {}}, coveringNone)
	for i := range casesNone {
		assert.Empty(t, casesNone[i].FallbackFor, "uncovered receiver has no FallbackFor")
		assert.GreaterOrEqual(t, casesNone[i].Priority, 0.0, "normal case is not deprioritized")
	}
}
```

- [ ] **Step 2: Run test to verify RED**

Run: `go test ./internal/head/scout/ -run 'TestWSCasesCovered_LazyFallbackForCoveredReceiver' -v`
Expected: FAIL — today a covered receiver's relay case is dropped (`if !svcCovered[rc.Steps[0].Role]`), so `webRelay` is nil.

- [ ] **Step 3: Emit a lazy fallback for a covered receiver**

In `internal/head/scout/ws_cases.go`, in `WSCasesCovered`, replace the relay-emission block:

```go
		relayCases, relayCovered, _ := wsRelayCases(svc)
		svcCovered := covered[svc.Name]
		for _, rc := range relayCases {
			// The receiver role is the first step's Role (A-first connect order).
			if !svcCovered[rc.Steps[0].Role] {
				cases = append(cases, rc)
			}
		}
```

with:

```go
		relayCases, relayCovered, _ := wsRelayCases(svc)
		svcCovered := covered[svc.Name]
		svcCovering := coveringCase[svc.Name]
		for _, rc := range relayCases {
			// The receiver role is the first step's Role (A-first connect order).
			receiver := rc.Steps[0].Role
			if !svcCovered[receiver] {
				// Uncovered receiver: emit the deterministic relay as a normal case.
				cases = append(cases, rc)
				continue
			}
			// A1 Phase 2: receiver covered by a sound LLM case. Emit a lazy
			// fallback copy bound to that case. Priority<0 makes the Agent skip
			// it by default; it activates the copy only if the primary case
			// fails at execution (a runtime hole plan-time soundness cannot see).
			// rc is a value (Steps slice shared read-only — the Agent does not
			// mutate steps), so a shallow copy is sufficient.
			if coverer := svcCovering[receiver]; coverer != "" {
				fb := rc
				fb.FallbackFor = coverer
				fb.Priority = -1
				cases = append(cases, fb)
			}
		}
```

- [ ] **Step 4: Run the targeted test to verify GREEN**

Run: `go test ./internal/head/scout/ -run 'TestWSCasesCovered_LazyFallbackForCoveredReceiver' -v`
Expected: PASS.

- [ ] **Step 5: Regression — full scout package**

Run: `go test ./internal/head/scout/ -count=1`
Expected: PASS. In particular:
- `TestWSCasesCovered_*` tests that hand-build `covered` with no `coveringCase` pass `nil` for it; a covered receiver with no coverer (`""`) is dropped, matching the prior drop behavior.
- `TestAssemblePlan_UnsoundWSFlowDoesNotCover` — an unsound case records no coverer, so the receiver is uncovered and emits a normal relay case as before.

If a test deliberately built `covered` to exercise the drop, pass a matching `coveringCase` (or `nil`) and confirm the expected emission.

- [ ] **Step 6: make check**

Run: `make check`
Expected: EXIT 0 (fmt + lint + test -race).

- [ ] **Step 7: Commit + push**

```bash
git add internal/head/scout/ws_cases.go internal/head/scout/ws_relay_test.go
git -c user.name='binoctal' -c user.email='binoctal@gmail.com' commit -m "feat(scout): emit lazy WS relay fallback for covered receiver (A1 Phase 2)

WSCasesCovered no longer drops the deterministic relay case for a receiver
covered by a sound LLM case: it emits the relay as a lazy fallback (FallbackFor
bound to the covering case, Priority<0). The Agent skips it by default and
activates it only when the primary case fails at execution, recovering the role
instead of stranding it. An uncovered receiver still emits a normal relay case.

Spec: cerberus-docs/superpowers/specs/2026-07-28-ws-a1-runtime-fallback-design.md"
git push origin main
```

---

### Task 4: Agent serial — pre-scan + activate fallback on non-environmental failure

**Files:**
- Modify: `internal/head/agent/executor_run.go` (`isEnvironmental` new helper; `ExecutePlan` pre-scan + skip + activation)
- Test: `internal/head/agent/fallback_activate_test.go` (new)

**Interfaces:**
- Consumes: `TestCase.FallbackFor` (Task 1), lazy fallback cases emitted by Task 3, `types.IsEnvironmentalFailure` (`internal/types/result_environmental.go`).
- Produces: `ExecutePlan` activates a lazy fallback (sets `StepResult.Recovered`) when its primary returns `StepFailed` and `!isEnvironmental(result)`. Fallback results are excluded from `consecutiveFailures`.

- [ ] **Step 1: Write the failing tests (RED)**

Create `internal/head/agent/fallback_activate_test.go`:

```go
package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/embed"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/store"
	"github.com/binoctal/cerberus/internal/types"
)

// fallbackFakeExec succeeds on ws_connect/send and succeeds or fails on
// ws_receive per construction, so a ws_flow case can be driven to a
// deterministic non-environmental StepFailed. types.ErrorResult with an empty
// Err is a success (Success()==true); a non-empty Err is a failure.
type fallbackFakeExec struct{ receiveFails bool }

func (f fallbackFakeExec) Execute(ctx context.Context, a types.TypedAction) types.ExecutorResult {
	switch a.(type) {
	case types.WSReceiveAction:
		if f.receiveFails {
			return types.ErrorResult{Err: "receive timeout"}
		}
		return types.ErrorResult{}
	case types.WSConnectAction, types.WSSendAction, types.WSDisconnectAction:
		return types.ErrorResult{}
	default:
		return types.ErrorResult{Err: "unsupported action"}
	}
}

func fallbackLoop(t *testing.T, receiveFails bool) (*ReActLoop, string) {
	t.Helper()
	s, err := store.New(":memory:")
	require.NoError(t, err)
	require.NoError(t, store.RunMigrations(context.Background(), s.DB(), "../../../migrations"))
	driver := ai.NewDriver(llm.NewMockClient(nil), ai.NewTokenBudget(200000, 10000))
	loop := NewReActLoopWithConfig(ReActLoopConfig{
		Driver:   driver,
		Store:    s,
		Engine:   NewRuleEngine(nil, nil, "."),
		Executor: fallbackFakeExec{receiveFails: receiveFails},
		Config:   DefaultReActConfig(),
		Logger:   zap.NewNop(),
		Embedder: embed.NewTrigramProvider(embed.DefaultDimension),
	})
	sess, err := s.CreateSession(context.Background(), "run", "test", "")
	require.NoError(t, err)
	return loop, sess.ID
}

// wsFlowCase builds a ws_flow Steps case (connect then receive) — connect
// always succeeds under fallbackFakeExec; receive succeeds iff !receiveFails.
func wsFlowCase(id string) TestCase {
	return TestCase{
		ID: id, Action: "ws_flow", Target: "ws://127.0.0.1:1/ws", Service: "rt",
		Steps: []TestStep{
			{Action: "ws_connect", ConnectionID: "web", Role: "web"},
			{Action: "ws_receive", ConnectionID: "web", Type: "device:online"},
		},
	}
}

func TestExecutePlan_ActivatesFallbackOnFailure(t *testing.T) {
	loop, sid := fallbackLoop(t, true) // primary's receive fails
	primary := wsFlowCase("tc-primary")
	fallback := wsFlowCase("tc-fallback")
	fallback.FallbackFor = "tc-primary"
	fallback.Priority = -1

	res, err := loop.ExecutePlan(context.Background(), &TestPlan{
		Goal: "g", Cases: []TestCase{primary, fallback},
	}, sid)
	require.NoError(t, err)
	require.Len(t, res, 2, "primary + activated fallback")
	assert.Equal(t, StepFailed, res[0].Status, "primary failed")
	assert.Equal(t, StepPassed, res[1].Status, "fallback ran and passed")
	assert.True(t, res[1].Recovered, "fallback marked recovered")
	assert.Equal(t, "tc-fallback", res[1].TestCase.ID)
}

func TestExecutePlan_NoFallbackOnPass(t *testing.T) {
	loop, sid := fallbackLoop(t, false) // primary passes
	primary := wsFlowCase("tc-primary")
	fallback := wsFlowCase("tc-fallback")
	fallback.FallbackFor = "tc-primary"
	fallback.Priority = -1

	res, err := loop.ExecutePlan(context.Background(), &TestPlan{
		Goal: "g", Cases: []TestCase{primary, fallback},
	}, sid)
	require.NoError(t, err)
	require.Len(t, res, 1, "primary only — lazy fallback not activated on pass")
	assert.Equal(t, StepPassed, res[0].Status)
}
```

- [ ] **Step 2: Run tests to verify RED**

Run: `go test ./internal/head/agent/ -run 'TestExecutePlan_ActivatesFallbackOnFailure|TestExecutePlan_NoFallbackOnPass' -v`
Expected: FAIL — `ActivatesFallbackOnFailure`: only the primary result is returned (len 1, not 2) and it is `StepSkipped` (a `Priority<0` case is skipped today). `NoFallbackOnPass` may pass trivially today (fallback skipped, len 1).

- [ ] **Step 3: Add the `isEnvironmental` helper**

In `internal/head/agent/executor_run.go`, add `"strings"` and `"github.com/binoctal/cerberus/internal/types"` to the import block, then add the helper (e.g. after `isDeprioritized`):

```go
// isEnvironmental reports whether a failed StepResult is an environmental
// failure (target unreachable) rather than a logic/assertion failure. A lazy
// fallback must NOT be activated for environmental failures: if the target is
// unreachable, the fallback cannot succeed either. The ReAct loop builds the
// unreachable result via buildFailedResultForUnreachableTarget, which sets
// Error="target unreachable: ..." with a nil Result, so check both the Result
// (types.IsEnvironmentalFailure) and the Error string.
func isEnvironmental(r StepResult) bool {
	if r.Result != nil && types.IsEnvironmentalFailure(r.Result) {
		return true
	}
	if r.Error != nil && strings.Contains(strings.ToLower(r.Error.Error()), "target unreachable") {
		return true
	}
	return false
}
```

- [ ] **Step 4: Pre-scan + skip + activate in `ExecutePlan`**

In `internal/head/agent/executor_run.go`, in `ExecutePlan`, add the pre-scan after `remainingCases := 0` (before the loop):

```go
	consecutiveFailures := 0
	remainingCases := 0
	// A1 Phase 2: index lazy fallback cases by primary ID. They are skipped in
	// the main loop and activated only when their primary case fails.
	fallbacksByPrimary := map[string][]*TestCase{}
	for i := range plan.Cases {
		if tc := &plan.Cases[i]; tc.FallbackFor != "" {
			fb := &plan.Cases[i]
			fallbacksByPrimary[fb.FallbackFor] = append(fallbacksByPrimary[fb.FallbackFor], fb)
		}
	}
```

At the top of the loop body, make lazy fallback cases skip execution (before the existing `isDeprioritized` check):

```go
	for i := range plan.Cases {
		tc := &plan.Cases[i]
		remainingCases = len(plan.Cases) - i
		if tc.FallbackFor != "" {
			// Lazy fallback: pre-scanned into fallbacksByPrimary; activated only
			// by its primary's failure below. Do not execute or record here.
			continue
		}
		if isDeprioritized(tc) {
```

After the systemic-failure check at the end of the loop body (after `if r.checkSystemicFailure(...)` block, just before the closing `}` of the `for`), add the activation — placed after the escalation checks so the fallback neither counts toward `consecutiveFailures` nor triggers a budget/systemic checkpoint:

```go
		if r.checkSystemicFailure(ctx, consecutiveFailures, sessionID) {
			return results, fmt.Errorf("execution aborted after %d consecutive failures", consecutiveFailures)
		}

		// A1 Phase 2: activate the primary's lazy fallback on a non-environmental
		// failure. The fallback runs the deterministic runSteps path (no LLM);
		// its result is excluded from consecutiveFailures and budget checks
		// above, which already ran for the primary. Recovered is set iff the
		// fallback itself passed; the primary's fail verdict is unchanged.
		if result.Status == StepFailed && !isEnvironmental(result) {
			for _, fb := range fallbacksByPrimary[tc.ID] {
				fbResult := r.executeStep(ctx, fb, sessionID)
				fbResult.Recovered = fbResult.Status == StepPassed
				results = append(results, fbResult)
				r.emitProgress(ProgressEvent{Type: "case_complete", CaseID: fb.ID, Status: fbResult.Status})
			}
		}
	}
```

- [ ] **Step 5: Run the targeted tests to verify GREEN**

Run: `go test ./internal/head/agent/ -run 'TestExecutePlan_ActivatesFallbackOnFailure|TestExecutePlan_NoFallbackOnPass' -v`
Expected: PASS.

- [ ] **Step 6: Regression — full agent package**

Run: `go test ./internal/head/agent/ -count=1`
Expected: PASS. In particular `TestExecutePlan_SkipsDeprioritizedCases` — a `Priority<0` case with empty `FallbackFor` is still skipped (the new `tc.FallbackFor != ""` guard falls through to `isDeprioritized`).

- [ ] **Step 7: make check**

Run: `make check`
Expected: EXIT 0.

- [ ] **Step 8: Commit**

```bash
git add internal/head/agent/executor_run.go internal/head/agent/fallback_activate_test.go
git -c user.name='binoctal' -c user.email='binoctal@gmail.com' commit -m "feat(agent): activate lazy WS fallback on non-environmental failure (A1 Phase 2)

ExecutePlan pre-scans FallbackFor cases into a fallbacksByPrimary index, skips
them in the main loop, and activates one only when its primary case returns a
non-environmental StepFailed. The fallback result is appended after the primary
with Recovered set iff it passed, and is excluded from consecutiveFailures and
budget checkpoints. isEnvironmental reuses types.IsEnvironmentalFailure plus the
target-unreachable Error string (no new StepResult field)."
```

---

### Task 5: Agent parallel — pre-scan + activate fallback in the worker

**Files:**
- Modify: `internal/head/agent/parallel_execute.go` (`ExecutePlan` pre-scan + skip)
- Modify: `internal/head/agent/parallel_execute_helpers.go` (`executeAndStore` activation)
- Test: `internal/head/agent/fallback_activate_test.go` (append)

**Interfaces:**
- Consumes: `TestCase.FallbackFor` (Task 1), `isEnvironmental` + `StepResult.Recovered` (Task 4).
- Produces: the parallel `ExecutePlan` mirrors the serial path — lazy cases are pre-scanned and skipped; a worker activates its primary's fallback inline after a non-environmental failure and stores `results[fb.ID]` exactly once.

- [ ] **Step 1: Write the failing test (RED)**

Append to `internal/head/agent/fallback_activate_test.go`:

```go
func TestParallelExecutePlan_ActivatesFallbackOnFailure(t *testing.T) {
	loop, sid := fallbackLoop(t, true) // primary's receive fails
	primary := wsFlowCase("tc-primary")
	fallback := wsFlowCase("tc-fallback")
	fallback.FallbackFor = "tc-primary"
	fallback.Priority = -1

	pExec := NewParallelExecutor(loop, ParallelConfig{MaxWorkers: 2}, zap.NewNop())
	res, err := pExec.ExecutePlan(context.Background(), &TestPlan{
		Goal: "g", Cases: []TestCase{primary, fallback},
	}, sid)
	require.NoError(t, err)

	// collectResults returns results keyed by case ID in plan order; the lazy
	// fallback was activated by its primary's worker, so its result is present.
	byID := map[string]StepResult{}
	for _, r := range res {
		byID[r.TestCase.ID] = r
	}
	assert.Equal(t, StepFailed, byID["tc-primary"].Status, "primary failed")
	assert.Contains(t, byID, "tc-fallback", "fallback activated in parallel")
	assert.True(t, byID["tc-fallback"].Recovered, "fallback recovered")
}
```

- [ ] **Step 2: Run test to verify RED**

Run: `go test ./internal/head/agent/ -run 'TestParallelExecutePlan_ActivatesFallbackOnFailure' -v`
Expected: FAIL — `byID["tc-fallback"]` is absent (the lazy case is skipped, never activated).

- [ ] **Step 3: Pre-scan + skip in parallel `ExecutePlan`**

In `internal/head/agent/parallel_execute.go`, in `ExecutePlan`, add a pre-scan before the worker loop (after `state := initParallelExecState(...)`):

```go
	// Phase 2.5: A1 Phase 2 — index lazy fallback cases by primary ID. They are
	// skipped in the dispatch loop and activated inline by their primary's worker.
	fallbacksByPrimary := map[string][]*TestCase{}
	for i := range plan.Cases {
		if tc := &plan.Cases[i]; tc.FallbackFor != "" {
			fb := &plan.Cases[i]
			fallbacksByPrimary[fb.FallbackFor] = append(fallbacksByPrimary[fb.FallbackFor], fb)
		}
	}
```

In the dispatch loop, skip lazy cases (add at the top of the `for i := range plan.Cases` body, before the `isDeprioritized` check):

```go
	for i := range plan.Cases {
		tc := &plan.Cases[i]
		if tc.FallbackFor != "" {
			// Lazy fallback: activated only by its primary's worker below.
			continue
		}
		if isDeprioritized(tc) {
			p.skipAndStore(tc, state)
			continue
		}
```

Pass the index into `executeAndStore` by changing the call:

```go
			// Execute and store result
			p.executeAndStore(ctx, tc, sessionID, state, fallbacksByPrimary)
```

- [ ] **Step 4: Activate inline in `executeAndStore`**

In `internal/head/agent/parallel_execute_helpers.go`, change the signature and body of `executeAndStore`:

```go
// executeAndStore executes a single test case and stores the result. On a
// non-environmental failure it also runs the case's lazy fallbacks (A1 Phase 2)
// inline in this same worker and stores each by its own ID. A fallback is bound
// to exactly one primary and runs only here, so results[fb.ID] is written once.
func (p *ParallelExecutor) executeAndStore(ctx context.Context, tc *TestCase, sessionID string, state *parallelExecState, fallbacksByPrimary map[string][]*TestCase) {
	defer func() { <-state.sem }()

	result := p.loop.executeStep(ctx, tc, sessionID)

	store := func(r StepResult) {
		state.mu.Lock()
		state.results[r.TestCase.ID] = r
		if ch, ok := state.completed[r.TestCase.ID]; ok {
			close(ch)
		}
		state.mu.Unlock()
	}
	store(result)

	// A1 Phase 2: activate lazy fallback on non-environmental failure.
	if result.Status == StepFailed && !isEnvironmental(result) {
		for _, fb := range fallbacksByPrimary[tc.ID] {
			fbResult := p.loop.executeStep(ctx, fb, sessionID)
			fbResult.Recovered = fbResult.Status == StepPassed
			store(fbResult)
		}
	}

	p.logger.Info("parallel case completed",
		zap.String("case_id", tc.ID),
		zap.String("status", string(result.Status)),
	)
}
```

Note: `state.results` was previously keyed by `tc.ID`; it is now keyed by `r.TestCase.ID` so the fallback (a different ID) is stored under its own key. `collectResults` looks up `results[tc.ID]` for every case in plan order, which finds both the primary and the lazy fallback.

- [ ] **Step 5: Run the targeted test to verify GREEN**

Run: `go test ./internal/head/agent/ -run 'TestParallelExecutePlan_ActivatesFallbackOnFailure' -v`
Expected: PASS.

- [ ] **Step 6: Regression — full agent package + make check**

Run: `make check`
Expected: EXIT 0. In particular existing `parallel_test.go` cases still pass: their cases have empty `FallbackFor`, so the new guard is a no-op for them, and `store` keys by `r.TestCase.ID == tc.ID` exactly as before for non-fallback results.

- [ ] **Step 7: Commit + push**

```bash
git add internal/head/agent/parallel_execute.go internal/head/agent/parallel_execute_helpers.go internal/head/agent/fallback_activate_test.go
git -c user.name='binoctal' -c user.email='binoctal@gmail.com' commit -m "feat(agent): activate lazy WS fallback in parallel executor (A1 Phase 2)

Parallel ExecutePlan mirrors the serial path: it pre-scans FallbackFor cases
into fallbacksByPrimary, skips them in dispatch, and runs a primary's fallbacks
inline in the same worker on a non-environmental StepFailed. results is keyed by
case ID so the fallback is stored under its own ID and found by collectResults;
a fallback is bound to one primary, so its slot is written exactly once.

Spec: cerberus-docs/superpowers/specs/2026-07-28-ws-a1-runtime-fallback-design.md"
git push origin main
```

---

## Self-Review (completed)

- **Spec coverage:**
  - Data model (§1) → Task 1. ✓
  - `coveringCase` side table (§2) → Task 2 (produced + threaded). ✓
  - Plan-time lazy emission for covered receiver (§3) → Task 3. ✓
  - Pre-scan + serial activation + execution path via runSteps (§4) → Task 4 (`executeStep` Phase 0 path documented in the helper comment; activation gated on `StepFailed && !isEnvironmental`). ✓
  - Parallel activation, main-loop skip, single-write `results[fb.ID]` (§4 Parallel) → Task 5. ✓
  - Escalation interaction (§4) → Task 4 places activation after `consecutiveFailures`/budget checks; fallback results are never counted. ✓
  - Recover semantics (§5) → `fbResult.Recovered = fbResult.Status == StepPassed` in Tasks 4 & 5; primary fail unchanged. ✓
  - isEnvironmental reuses `types.IsEnvironmentalFailure` + Error string, no new StepResult field (§4) → Task 4 Step 3. ✓
  - Out of scope (loop, agent-authored synthesis, non-WS, examiner changes) → no task touches them. ✓
- **Placeholder scan:** No TBD/TODO; every code step has full code. The `WSCases` wrapper line number (~18) and struct field placements are the only approximations and are stable. ✓
- **Type consistency:** `coveringCase map[string]map[string]string` consistent across Tasks 2–3; `FallbackFor string` + `Recovered bool` consistent across Tasks 1/4/5; `fallbacksByPrimary map[string][]*TestCase` consistent across Tasks 4/5; `isEnvironmental(StepResult) bool` defined in Task 4 and reused in Task 5. ✓
- **Test design:** Each task's test fails for the documented reason before the implementation step and passes after. `fallbackFakeExec` relies on `types.ErrorResult` with empty `Err` = success (the existing contract in `internal/types/result_error.go`); if a step's action type name differs, the `default` branch surfaces it loudly rather than silently passing. ✓
