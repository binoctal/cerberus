# Tool-Migration S2 — Scout 双层 Tool-Calling — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Migrate all seven Scout `Decide` call sites to `DecideWithTools` via a two-tier tool surface, deleting every drift-absorption patch (`flexibleStrings`, `relayIntent` body-JSON parsing, `Priorities` dual-shape, `PlanOutput` JSON) and the `verifyServiceAttribution` LLM round-trip.

**Architecture:** A new assembly layer (`assembly.go`) converts `[]llm.ToolCall` → `agent.TestPlan`. High-level tools map to no-Steps TestCases; `begin_case` + ws_* step tools map to Steps TestCases (`Action:"ws_flow"`). Each call site swaps `Decide` for `DecideWithTools` + assembly. `directPlan` returns `(TestPlan, covered)` so `augmentPlan` can suppress redundant WS connects. ToT `evaluate` becomes a deterministic multi-signal score.

**Tech Stack:** Go 1.25, `internal/head/scout`, `internal/ai`, `internal/llm`, `internal/head/contract`, testify.

## Global Constraints

- Go 1.25, pure-Go (no CGo); module `github.com/binoctal/cerberus`
- `coder/websocket` v1.8.14 only (untouched — no WS executor change)
- No expression/evaluator deps
- Commit author MUST be `binoctal <binoctal@gmail.com>`, NO Co-Authored-By trailer
- Code comments + commit messages in English
- Docs only in `cerberus-docs/`
- `make check` (fmt + lint + test -race) must be EXIT 0
- No provider implementation changes (parsing done in S1)
- `llm.Tool{Name, Description, InputSchema map[string]any}`; `llm.ToolCall{ID, Name, Input map[string]any}`
- `agent.TestCase` fields in use: `ID, Name, Target, Method, Action, Expectation, Severity, Service, Body, Steps`; `agent.TestStep`: `Action, ConnectionID, Role, Message, Type, Aliases, Asserts, Timeout`

---

### Task 1: Plan tool definitions + high-level assembly

**Files:**
- Create: `internal/head/scout/tools.go` — `planTools() []llm.Tool`
- Create: `internal/head/scout/assembly.go` — `assemblePlan(...)` + high-level assemblers + field helpers
- Test: `internal/head/scout/assembly_test.go` (new)

**Interfaces:**
- Consumes: `llm.Tool`, `llm.ToolCall`, `agent.TestCase`, `agent.TestPlan`, `project.Service`; existing `attributeService`, `fillBody` (`direct_planning.go`).
- Produces:
  - `func planTools() []llm.Tool`
  - `func assemblePlan(calls []llm.ToolCall, goal, baseURL string, services []project.Service) (*agent.TestPlan, map[string]map[string]bool)`
  - The `covered` return is `map[service]map[role]bool` (roles already connected by a `begin_case`+ws_* group), threaded to `WSCasesCovered` in Task 3.

- [ ] **Step 1: Write the failing tests (RED)**

Create `internal/head/scout/assembly_test.go`:

```go
package scout

import (
	"testing"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAssemblePlan_HighLevelHTTP(t *testing.T) {
	calls := []llm.ToolCall{{
		Name:  "test_http_endpoint",
		Input: map[string]any{"method": "GET", "path": "/api/users", "expect_status": float64(200)},
	}}
	plan, covered := assemblePlan(calls, "goal", "http://x", nil)
	require.Len(t, plan.Cases, 1)
	c := plan.Cases[0]
	assert.Equal(t, "tc-001", c.ID)
	assert.Equal(t, "GET", c.Method)
	assert.Equal(t, "/api/users", c.Target)
	assert.Contains(t, c.Expectation, "200")
	assert.Empty(t, covered)
}

// Service attribution: attributeService override replaces verifyServiceAttribution.
func TestAssemblePlan_ServiceOverride(t *testing.T) {
	svcs := []project.Service{{Name: "api", PathPrefix: []string{"/api"}}}
	calls := []llm.ToolCall{{
		Name:  "test_http_endpoint",
		Input: map[string]any{"method": "GET", "path": "/api/users", "service": "wrong"},
	}}
	plan, _ := assemblePlan(calls, "g", "", svcs)
	assert.Equal(t, "api", plan.Cases[0].Service, "attributeService must override wrong LLM tag")
}

func TestAssemblePlan_RunProcessBuildOnly(t *testing.T) {
	calls := []llm.ToolCall{{
		Name:  "run_process",
		Input: map[string]any{"action": "build"},
	}}
	plan, _ := assemblePlan(calls, "g", "", nil)
	require.Len(t, plan.Cases, 1)
	assert.Equal(t, "process_build", plan.Cases[0].Action)
}

func TestAssemblePlan_AnalyzeCodeAllThree(t *testing.T) {
	for _, a := range []string{"analyze", "lint", "symbols"} {
		calls := []llm.ToolCall{{Name: "analyze_code", Input: map[string]any{"action": a}}}
		plan, _ := assemblePlan(calls, "g", "", nil)
		require.Len(t, plan.Cases, 1, "action=%s", a)
		assert.Equal(t, "code_"+a, plan.Cases[0].Action)
	}
}

func TestAssemblePlan_IDsSequential(t *testing.T) {
	calls := []llm.ToolCall{
		{Name: "navigate", Input: map[string]any{"path": "/"}},
		{Name: "check_invariant", Input: map[string]any{"description": "x", "assertion": "y"}},
	}
	plan, _ := assemblePlan(calls, "g", "", nil)
	require.Len(t, plan.Cases, 2)
	assert.Equal(t, "tc-001", plan.Cases[0].ID)
	assert.Equal(t, "tc-002", plan.Cases[1].ID)
}
```

- [ ] **Step 2: Run tests to verify RED**

Run: `go test ./internal/head/scout/ -run 'TestAssemblePlan_' -v`
Expected: COMPILE ERROR — `undefined: planTools`, `undefined: assemblePlan`.

- [ ] **Step 3: Create `tools.go` with `planTools()`**

```go
package scout

import "github.com/binoctal/cerberus/internal/llm"

// planTools returns the two-tier tool surface for directPlan: high-level
// intent tools (one call = one TestCase) and low-level ws_* step tools gated by
// begin_case (multi-step choreography). Schemas are hard-enforced by the
// provider, replacing the old PlanOutput JSON.
func planTools() []llm.Tool {
	strs := func(items ...string) map[string]any {
		e := map[string]any{"type": "string"}
		if len(items) > 0 {
			cs := make([]any, len(items))
			for i, s := range items {
				cs[i] = s
			}
			e["enum"] = cs
		}
		return map[string]any{"type": "array", "items": e}
	}
	return []llm.Tool{
		{Name: "test_http_endpoint", Description: "Emit one HTTP test case.",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{
				"method": map[string]any{"type": "string", "enum": []any{"GET", "POST", "PUT", "PATCH", "DELETE"}},
				"path":   map[string]any{"type": "string"}, "body": map[string]any{"type": "string"},
				"service": map[string]any{"type": "string"}, "expect_status": map[string]any{"type": "number"},
				"expect_body": map[string]any{"type": "string"},
			}, "required": []any{"method", "path"}}},
		{Name: "check_invariant", Description: "Emit one invariant assertion case (executed via Steer).",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{
				"invariant_id": map[string]any{"type": "string"}, "description": map[string]any{"type": "string"},
				"assertion": map[string]any{"type": "string"}, "severity": map[string]any{"type": "string"},
			}}},
		{Name: "run_process", Description: "Emit one process test case. test/lint go through exec+cmd.",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{
				"action": map[string]any{"type": "string", "enum": []any{"build", "exec"}},
				"cmd": map[string]any{"type": "string"}, "expect": map[string]any{"type": "string"},
			}, "required": []any{"action"}}},
		{Name: "analyze_code", Description: "Emit one static-analysis case.",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{
				"action": map[string]any{"type": "string", "enum": []any{"analyze", "lint", "symbols"}},
				"target": map[string]any{"type": "string"},
			}, "required": []any{"action"}}},
		{Name: "check_file", Description: "Emit one file-system case.",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{
				"action": map[string]any{"type": "string", "enum": []any{"exists", "read", "glob"}},
				"path": map[string]any{"type": "string"}, "pattern": map[string]any{"type": "string"},
				"expect": map[string]any{"type": "string"},
			}, "required": []any{"action"}}},
		{Name: "navigate", Description: "Emit one browser navigation case.",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{
				"path": map[string]any{"type": "string"}, "expect": map[string]any{"type": "string"},
			}, "required": []any{"path"}}},
		{Name: "begin_case", Description: "Open a multi-step WS choreography case. Following ws_* calls belong to it until the next begin_case or high-level tool.",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{
				"name": map[string]any{"type": "string"}, "expectation": map[string]any{"type": "string"},
				"service": map[string]any{"type": "string"},
			}, "required": []any{"name", "expectation"}}},
		{Name: "ws_connect", Description: "WS step: open a connection as role.",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{
				"role": map[string]any{"type": "string"}, "url": map[string]any{"type": "string"},
			}, "required": []any{"role"}}},
		{Name: "ws_send", Description: "WS step: send a typed message on role's connection.",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{
				"role": map[string]any{"type": "string"}, "type": map[string]any{"type": "string"},
			}, "required": []any{"role", "type"}}},
		{Name: "ws_receive", Description: "WS step: await a typed message on role's connection.",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{
				"role": map[string]any{"type": "string"}, "type": map[string]any{"type": "string"},
				"aliases": strs(), "assert": map[string]any{"type": "object"}, "timeout": map[string]any{"type": "number"},
			}, "required": []any{"role"}}},
		{Name: "ws_disconnect", Description: "WS step: close role's connection.",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{
				"role": map[string]any{"type": "string"},
			}, "required": []any{"role"}}},
	}
}
```

- [ ] **Step 4: Create `assembly.go` — high-level assemblers + field helpers**

```go
package scout

import (
	"fmt"
	"strings"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
)

// assemblePlan converts directPlan tool calls into a TestPlan plus the
// per-service set of roles already connected by a begin_case+ws_* group
// (covered), so WSCasesCovered can suppress redundant deterministic connects.
// Unknown/invalid calls are dropped, never panic.
func assemblePlan(calls []llm.ToolCall, goal, baseURL string, services []project.Service) (*agent.TestPlan, map[string]map[string]bool) {
	var cases []agent.TestCase
	covered := map[string]map[string]bool{}
	var open *agent.TestCase
	id := 0
	nextID := func() string { id++; return fmt.Sprintf("tc-%03d", id) }
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

	for _, call := range calls {
		switch call.Name {
		case "test_http_endpoint":
			flush()
			cases = append(cases, assembleHTTP(call, nextID, services))
		case "check_invariant":
			flush()
			cases = append(cases, assembleInvariant(call, nextID))
		case "run_process":
			flush()
			cases = append(cases, assembleProcess(call, nextID))
		case "analyze_code":
			flush()
			cases = append(cases, assembleCode(call, nextID))
		case "check_file":
			flush()
			cases = append(cases, assembleFile(call, nextID))
		case "navigate":
			flush()
			cases = append(cases, assembleNavigate(call, nextID))
		case "begin_case":
			flush()
			open = &agent.TestCase{
				ID: nextID(), Name: strField(call, "name"),
				Expectation: strField(call, "expectation"), Action: "ws_flow",
				Service: strField(call, "service"),
			}
			// ws_* handled in Task 2; high-level-only tests never hit them.
		}
	}
	flush()
	cases = fillBody(cases, services) // retained: service.BodyTemplate fill
	return &agent.TestPlan{Goal: goal, Cases: cases, ProjectURL: baseURL}, covered
}

// --- field helpers (Input is map[string]any from provider JSON) ---

func strField(c llm.ToolCall, k string) string {
	if v, ok := c.Input[k].(string); ok {
		return v
	}
	return ""
}
func intField(c llm.ToolCall, k string) int {
	switch v := c.Input[k].(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return 0
}
func strSliceField(c llm.ToolCall, k string) []string {
	arr, ok := c.Input[k].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, v := range arr {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
func mapField(c llm.ToolCall, k string) map[string]any {
	if m, ok := c.Input[k].(map[string]any); ok {
		return m
	}
	return nil
}

// --- high-level assemblers ---

func assembleHTTP(c llm.ToolCall, nextID func() string, svcs []project.Service) agent.TestCase {
	method := strings.ToUpper(strField(c, "method"))
	path := strField(c, "path")
	tc := agent.TestCase{
		ID: nextID(), Name: fmt.Sprintf("%s %s", method, path), Target: path,
		Method: method, Body: strField(c, "body"),
		Expectation: formatHTTPExpectation(c), Service: strField(c, "service"),
	}
	if svc := attributeService(path, svcs); svc != "" {
		tc.Service = svc // deterministic override (replaces verifyServiceAttribution)
	}
	return tc
}

func formatHTTPExpectation(c llm.ToolCall) string {
	var parts []string
	if s := intField(c, "expect_status"); s != 0 {
		parts = append(parts, fmt.Sprintf("status %d", s))
	}
	if b := strField(c, "expect_body"); b != "" {
		parts = append(parts, fmt.Sprintf("body contains %q", b))
	}
	if len(parts) == 0 {
		return "Returns 2xx status code"
	}
	return strings.Join(parts, "; ")
}

func assembleInvariant(c llm.ToolCall, nextID func() string) agent.TestCase {
	desc := strField(c, "description")
	if id := strField(c, "invariant_id"); id != "" {
		desc = id
	}
	return agent.TestCase{
		ID: nextID(), Target: desc, Expectation: strField(c, "assertion"),
		Severity: strField(c, "severity"),
	}
}

func assembleProcess(c llm.ToolCall, nextID func() string) agent.TestCase {
	a := strField(c, "action") // build | exec (schema-enforced; test/lint via exec+cmd)
	return agent.TestCase{ID: nextID(), Action: "process_" + a, Target: strField(c, "cmd"), Expectation: strField(c, "expect")}
}

func assembleCode(c llm.ToolCall, nextID func() string) agent.TestCase {
	return agent.TestCase{ID: nextID(), Action: "code_" + strField(c, "action"), Target: strField(c, "target")}
}

func assembleFile(c llm.ToolCall, nextID func() string) agent.TestCase {
	return agent.TestCase{ID: nextID(), Action: "file_" + strField(c, "action"), Target: strField(c, "path"), Body: strField(c, "pattern"), Expectation: strField(c, "expect")}
}

func assembleNavigate(c llm.ToolCall, nextID func() string) agent.TestCase {
	return agent.TestCase{ID: nextID(), Action: "navigate", Target: strField(c, "path"), Expectation: strField(c, "expect")}
}
```

- [ ] **Step 5: Run tests to verify GREEN**

Run: `go test ./internal/head/scout/ -run 'TestAssemblePlan_' -v`
Expected: PASS (all five tests). `wsSendBody` already exists in `ws_relay.go` (used by `fillBody` path); if not, add it as `func wsSendBody(typ string) string { b,_ := json.Marshal(map[string]string{"type":typ}); return string(b) }`.

- [ ] **Step 6: Commit**

```bash
git add internal/head/scout/tools.go internal/head/scout/assembly.go internal/head/scout/assembly_test.go
git commit -m "feat(scout): plan tool definitions + high-level assembly

Add planTools() (two-tier surface: 6 high-level intent tools + begin_case +
4 ws_* step tools) and assemblePlan() converting []llm.ToolCall -> TestPlan.
High-level tools produce no-Steps TestCases; attributeService overrides wrong
LLM service tags deterministically (replacing verifyServiceAttribution).
run_process enum is build|exec (test/lint via exec+cmd); analyze_code keeps
all three (analyze|lint|symbols). IDs are code-generated tc-NNN."
```

---

### Task 2: Low-level WS step assembly (begin_case + ws_*)

**Files:**
- Modify: `internal/head/scout/assembly.go` — extend `assemblePlan` switch with ws_* cases + role validation
- Test: `internal/head/scout/assembly_test.go` (append)

**Interfaces:**
- Consumes: `agent.TestStep`, the `open *agent.TestCase` accumulator from Task 1, `wsSendBody` (existing in `ws_relay.go`).
- Produces: `assemblePlan` now handles `ws_connect/ws_send/ws_receive/ws_disconnect`, grouping them under the most recent `begin_case` into a Steps TestCase with `Action:"ws_flow"`.

- [ ] **Step 1: Write the failing tests (RED)**

Append to `assembly_test.go`:

```go
func TestAssemblePlan_WSRelaySequence(t *testing.T) {
	calls := []llm.ToolCall{
		{Name: "begin_case", Input: map[string]any{"name": "relay", "expectation": "bridge gets signal", "service": "ws"}},
		{Name: "ws_connect", Input: map[string]any{"role": "web"}},
		{Name: "ws_connect", Input: map[string]any{"role": "bridge"}},
		{Name: "ws_send", Input: map[string]any{"role": "web", "type": "ping"}},
		{Name: "ws_receive", Input: map[string]any{"role": "bridge", "type": "signal", "assert": map[string]any{"online": true}}},
	}
	plan, covered := assemblePlan(calls, "g", "", nil)
	require.Len(t, plan.Cases, 1)
	c := plan.Cases[0]
	assert.Equal(t, "ws_flow", c.Action)
	require.Len(t, c.Steps, 5)
	assert.Equal(t, "ws_connect", c.Steps[0].Action)
	assert.Equal(t, "web", c.Steps[0].ConnectionID)
	assert.Equal(t, `{"type":"ping"}`, c.Steps[1].Message)
	assert.Equal(t, "signal", c.Steps[3].Type)
	assert.Equal(t, map[string]any{"online": true}, c.Steps[3].Asserts)
	// covered records connected roles per service
	assert.True(t, covered["ws"]["web"])
	assert.True(t, covered["ws"]["bridge"])
}

func TestAssemblePlan_WSDroppedWithoutBeginCase(t *testing.T) {
	// ws_* before any begin_case: dropped (no open group).
	calls := []llm.ToolCall{{Name: "ws_connect", Input: map[string]any{"role": "web"}}}
	plan, _ := assemblePlan(calls, "g", "", nil)
	assert.Empty(t, plan.Cases)
}
```

- [ ] **Step 2: Run tests to verify RED**

Run: `go test ./internal/head/scout/ -run 'TestAssemblePlan_WS' -v`
Expected: FAIL — ws_* calls not yet handled (begin_case opens a group but steps aren't appended → `c.Steps` empty).

- [ ] **Step 3: Extend `assemblePlan` with ws_* cases**

In `assembly.go`, add these cases to the `switch call.Name` inside the loop (before the closing brace), reusing the `open` accumulator:

```go
		case "ws_connect":
			if open == nil {
				continue
			}
			open.Steps = append(open.Steps, agent.TestStep{
				Action: "ws_connect", ConnectionID: strField(call, "role"), Role: strField(call, "role"),
			})
		case "ws_send":
			if open == nil {
				continue
			}
			open.Steps = append(open.Steps, agent.TestStep{
				Action: "ws_send", ConnectionID: strField(call, "role"), Message: wsSendBody(strField(call, "type")),
			})
		case "ws_receive":
			if open == nil {
				continue
			}
			open.Steps = append(open.Steps, agent.TestStep{
				Action: "ws_receive", ConnectionID: strField(call, "role"),
				Type: strField(call, "type"), Aliases: strSliceField(call, "aliases"),
				Asserts: mapField(call, "assert"), Timeout: intField(call, "timeout"),
			})
		case "ws_disconnect":
			if open == nil {
				continue
			}
			open.Steps = append(open.Steps, agent.TestStep{
				Action: "ws_disconnect", ConnectionID: strField(call, "role"),
			})
```

- [ ] **Step 4: Run tests to verify GREEN**

Run: `go test ./internal/head/scout/ -run 'TestAssemblePlan_' -v`
Expected: PASS (all assembly tests).

- [ ] **Step 5: Commit**

```bash
git add internal/head/scout/assembly.go internal/head/scout/assembly_test.go
git commit -m "feat(scout): ws_* step assembly under begin_case

assemblePlan groups ws_connect/ws_send/ws_receive/ws_disconnect calls after a
begin_case into one Steps TestCase (Action ws_flow). ws_send wraps type in
{\"type\":...} via wsSendBody; covered records connected roles per service.
ws_* before any begin_case are dropped."
```

---

### Task 3: Wire directPlan to DecideWithTools; delete PlanOutput/expandWSRelayCases

**Files:**
- Modify: `internal/head/scout/direct_planning.go` — `runAIPlanning`, `convertPlanOutput` (delete), `directPlan` (return covered)
- Modify: `internal/head/scout/plan_phases.go` — `executeDirectPlanning`, `augmentPlan`, `Plan` (thread covered)
- Modify: `internal/head/scout/types.go` — delete `PlanOutput`, `CaseInfo`
- Modify: `internal/head/scout/ws_relay.go` — delete `expandWSRelayCases` + `expandOneRelayCase` + `expandedRelay` (validation absorbed into assembly)
- Modify: `internal/head/scout/prompts.go` — replace `promptPlanOutput` JSON section with tool-use guidance
- Test: update `direct_planning_test.go`, `scout_test.go`, `ws_relay_test.go`, `smoke/*`, `session/lifecycle_test.go` helpers (switch from PlanOutput JSON injection to tool-call fixtures)
- Live: `internal/head/scout/scout_live_test.go` — `TestScoutPlan_LiveGLM` asserts GLM emits `test_http_endpoint` + a `begin_case`/ws_* sequence

**Interfaces:**
- Consumes: `planTools()`, `assemblePlan()` (Tasks 1–2), `Driver.DecideWithTools`, `llm.NewMockClient` + `SetToolResponse`.
- Produces: `(*Scout) directPlan(...) (*agent.TestPlan, map[string]map[string]bool, error)`; `(*Scout) Plan` threads `covered` into `augmentPlan`.

- [ ] **Step 1: Write the failing directPlan mock test (RED)**

Append to `direct_planning_test.go` (use the S1 mock tool-response pattern):

```go
func TestDirectPlan_ToolCallingAssembly(t *testing.T) {
	mock := llm.NewMockClient(nil)
	mock.SetToolResponse("plan http + ws relay", []llm.ToolCall{
		{Name: "test_http_endpoint", Input: map[string]any{"method": "GET", "path": "/health"}},
		{Name: "begin_case", Input: map[string]any{"name": "r", "expectation": "ok", "service": "ws"}},
		{Name: "ws_connect", Input: map[string]any{"role": "a"}},
		{Name: "ws_connect", Input: map[string]any{"role": "b"}},
		{Name: "ws_send", Input: map[string]any{"role": "a", "type": "x"}},
		{Name: "ws_receive", Input: map[string]any{"role": "b", "type": "y"}},
	})
	s := newScoutWithMock(t, mock) // existing helper or build via NewScout
	plan, err := s.Plan(context.Background(), "plan http + ws relay", &project.ProjectModel{})
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(plan.Cases), 2) // http case + ws_flow case (+ executor appends)
}
```

- [ ] **Step 2: Run to verify RED**

Run: `go test ./internal/head/scout/ -run TestDirectPlan_ToolCallingAssembly -v`
Expected: FAIL — `directPlan` still calls `Decide`/`convertPlanOutput` returning no tool-assembled cases.

- [ ] **Step 3: Rewrite `runAIPlanning` + `directPlan`**

In `direct_planning.go`, replace `runAIPlanning` and `directPlan`:

```go
// runAIPlanning calls DecideWithTools and assembles tool calls into a plan.
// Zero tool calls (drift/quality) → error. Transient LLM call error → fallback.
func (s *Scout) runAIPlanning(ctx context.Context, prompt string, goal string, model *project.ProjectModel) (*agent.TestPlan, map[string]map[string]bool, error) {
	res, err := s.driver.DecideWithTools(ctx, prompt, planTools())
	if err != nil {
		s.logger.Warn("AI planning call failed, using deterministic fallback", zap.Error(err))
		fb := s.fallbackPlan(goal, model)
		return fb, map[string]map[string]bool{}, nil // transient-failure degradation path
	}
	if len(res.ToolCalls) == 0 {
		return nil, nil, fmt.Errorf("scout plan: zero tool calls (drift or quality)")
	}
	plan, covered := assemblePlan(res.ToolCalls, goal, s.resolveBaseURL(), s.config.Services)
	if len(plan.Cases) == 0 {
		return nil, nil, fmt.Errorf("scout plan: assembly produced zero cases")
	}
	return plan, covered, nil
}

func (s *Scout) directPlan(ctx context.Context, goal string, model *project.ProjectModel) (*agent.TestPlan, map[string]map[string]bool, error) {
	memory := s.buildEpisodicContext(ctx, goal, model)
	prompt := s.buildPlanningPrompt(ctx, goal, model, memory)
	return s.runAIPlanning(ctx, prompt, goal, model)
}
```

Delete `convertPlanOutput` entirely (its service-verification + fillBody duties now live in `assemblePlan`). Keep `fallbackPlan`, `resolveBaseURL`, `attributeService`, `fillBody`.

- [ ] **Step 4: Thread `covered` through `Plan`/`augmentPlan`**

In `plan_phases.go`:

```go
func (s *Scout) executeDirectPlanning(ctx context.Context, goal string, model *project.ProjectModel) (*agent.TestPlan, map[string]map[string]bool, error) {
	return s.directPlan(ctx, goal, model)
}
func (s *Scout) augmentPlan(plan *agent.TestPlan, goal string, covered map[string]map[string]bool) {
	s.appendExecutorCases(plan, goal, covered)
	filterWSEndpointDrift(plan, s.config)
}
func (s *Scout) Plan(ctx context.Context, goal string, model *project.ProjectModel) (*agent.TestPlan, error) {
	memory := s.buildMemoryContext(ctx, goal, model)
	var plan *agent.TestPlan
	var covered map[string]map[string]bool
	var err error
	if s.deepPlan {
		plan, err = s.executeDeepPlanning(ctx, goal, model, memory) // ToT path; returns (plan, nil) — covered empty
		covered = map[string]map[string]bool{}
	} else {
		plan, covered, err = s.executeDirectPlanning(ctx, goal, model)
	}
	if err != nil {
		return nil, err
	}
	s.augmentPlan(plan, goal, covered)
	return plan, nil
}
```

Update `executeDeepPlanning` to return `(*agent.TestPlan, error)` only (unchanged body) and have `Plan` synthesize `covered={}` for the ToT branch as above. Remove the `expandWSRelayCases` call from `augmentPlan` (its body→Steps job is gone; assembly authors Steps directly; `WSCasesCovered` consumes `covered`).

- [ ] **Step 5: Delete `PlanOutput`/`CaseInfo` and `expandWSRelayCases`**

- `types.go`: delete `PlanOutput` and `CaseInfo` structs.
- `ws_relay.go`: delete `expandWSRelayCases`, `expandOneRelayCase`, `expandedRelay`. Keep `relayIntent`/`relayStep` only if still referenced; otherwise delete. Keep `wsSendBody` (used by assembly) and `relayRecvTimeout` if referenced by ws_cases.

- [ ] **Step 6: Update prompt — `prompts.go`**

Replace the `promptPlanOutput` constant (JSON schema section) with tool-use guidance: "Emit one tool call per test case. Use high-level tools (test_http_endpoint, run_process, …) for single-step cases. For multi-step WebSocket choreography, emit begin_case then ws_connect/ws_send/ws_receive/ws_disconnect calls. Do not output JSON." Keep `promptPlanSystem`/`promptPlanSystemLocal`.

- [ ] **Step 7: Update affected tests**

- `ws_relay_test.go`: delete tests of `expandWSRelayCases` (the function is gone). The relay-coverage behavior is now covered by `TestAssemblePlan_WSRelaySequence`.
- `direct_planning_test.go` / `scout_test.go`: replace `PlanOutput` JSON mock responses with `SetToolResponse` tool-call fixtures.
- `session/lifecycle_test.go`, `smoke/*`, `session/contract_integration_test.go`: any helper injecting `PlanOutput` JSON switches to tool-call fixtures. (Grep: `grep -rl "PlanOutput\|\"cases\"" internal --include="*_test.go"`.)
- Keep `fallbackPlan` transient-path tests; delete drift-path tests that asserted fallback on parse failure (parse no longer exists).

- [ ] **Step 8: `make check` + commit**

Run: `make check` → EXIT 0.

```bash
git add -A
git commit -m "feat(scout): directPlan via DecideWithTools; delete PlanOutput/expandWSRelayCases

runAIPlanning calls DecideWithTools + assemblePlan. Zero tool calls (drift) →
error; LLM call error (transient) → fallbackPlan. directPlan returns
(TestPlan, covered); Plan threads covered into augmentPlan. Delete PlanOutput,
CaseInfo, expandWSRelayCases (assembly authors Steps directly). PlanOutput JSON
mock fixtures across tests switch to SetToolResponse tool calls."
```

- [ ] **Step 9: Live gate — `TestScoutPlan_LiveGLM`**

Add to `scout_live_test.go` (gated `//go:build live` like the existing `TestScoutRelayEmission_Live`):

```go
func TestScoutPlan_LiveGLM(t *testing.T) {
	s := newLiveScout(t) // existing helper using ANTHROPIC_API_KEY + GLM base URL
	plan, err := s.Plan(context.Background(), "test the /health endpoint and a web-bridge ws relay", liveModel(t))
	require.NoError(t, err)
	require.NotEmpty(t, plan.Cases, "GLM must emit ≥1 tool call via test_http_endpoint / begin_case")
}
```

Run: `go test -tags live ./internal/head/scout/ -run TestScoutPlan_LiveGLM -v`
Expected: PASS (GLM emits tool calls). If FAIL, inspect whether GLM ignores tool definitions — adjust prompt guidance, not the provider.

---

### Task 4: Analyze → report_endpoint / report_page / declare_tech; delete flexibleStrings

**Files:**
- Modify: `internal/head/scout/analyze_phases.go` — `runAIInference` uses `DecideWithTools` + analyze assembly
- Modify: `internal/head/scout/tools.go` — add `analyzeTools()`
- Modify: `internal/head/scout/assembly.go` — add `assembleAnalyze(calls, model) -> AnalyzeOutput` (or merge directly into model)
- Modify: `internal/head/scout/types.go` — delete `flexibleStrings`, `firstStringValue`; keep `AnalyzeOutput`/`EndpointInfo`/`PageInfo` as the assembly target (no longer LLM-emitted)
- Modify: `internal/head/scout/prompts.go` — replace `promptAnalyzeOutput` JSON with tool-use guidance
- Test: `analyze_techstack_test.go`, `scout_test.go` (Analyze tests switch to tool fixtures)

**Interfaces:**
- Consumes: `DecideWithTools`, `mergeAIInference`.
- Produces: `func analyzeTools() []llm.Tool`; `func assembleAnalyze(calls []llm.ToolCall) AnalyzeOutput`.

- [ ] **Step 1: Write failing test (RED)**

```go
func TestAssembleAnalyze_DeclareTechForcesStrings(t *testing.T) {
	calls := []llm.ToolCall{
		{Name: "report_endpoint", Input: map[string]any{"method": "GET", "path": "/api"}},
		{Name: "declare_tech", Input: map[string]any{"stack": []any{"go", "make"}}},
	}
	out := assembleAnalyze(calls)
	require.Len(t, out.Endpoints, 1)
	assert.Equal(t, "GET", out.Endpoints[0].Method)
	assert.Equal(t, []string{"go", "make"}, []string(out.TechStack))
}
```

- [ ] **Step 2: Verify RED** — `go test ./internal/head/scout/ -run TestAssembleAnalyze -v` → `undefined: analyzeTools/assembleAnalyze`.

- [ ] **Step 3: Add `analyzeTools()` + `assembleAnalyze()`**

```go
func analyzeTools() []llm.Tool {
	return []llm.Tool{
		{Name: "report_endpoint", Description: "Report one discovered API endpoint.",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{
				"method": map[string]any{"type": "string"}, "path": map[string]any{"type": "string"},
				"confidence": map[string]any{"type": "number"},
			}, "required": []any{"method", "path"}}},
		{Name: "report_page", Description: "Report one discovered page/route.",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{
				"path": map[string]any{"type": "string"}, "confidence": map[string]any{"type": "number"},
			}, "required": []any{"path"}}},
		{Name: "declare_tech", Description: "Declare detected tech stack (string array).",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{
				"stack": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			}, "required": []any{"stack"}}},
	}
}

func assembleAnalyze(calls []llm.ToolCall) AnalyzeOutput {
	var out AnalyzeOutput
	for _, c := range calls {
		switch c.Name {
		case "report_endpoint":
			out.Endpoints = append(out.Endpoints, EndpointInfo{
				Method: strField(c, "method"), Path: strField(c, "path"),
				Confidence: numField(c, "confidence"),
			})
		case "report_page":
			out.Pages = append(out.Pages, PageInfo{Path: strField(c, "path"), Confidence: numField(c, "confidence")})
		case "declare_tech":
			out.TechStack = strSliceField(c, "stack") // []string — flexibleStrings no longer needed
		}
	}
	return out
}
```

Add `numField` helper (`func numField(c llm.ToolCall, k string) float64 { if v,ok:=c.Input[k].(float64); ok { return v }; return 0 }`) to `assembly.go`.

- [ ] **Step 4: Rewrite `runAIInference`**

In `analyze_phases.go`:

```go
func (s *Scout) runAIInference(ctx context.Context, prompt string, configModel *project.ProjectModel) (*project.ProjectModel, error) {
	res, err := s.driver.DecideWithTools(ctx, prompt, analyzeTools())
	if err != nil || len(res.ToolCalls) == 0 {
		s.logger.Warn("AI analysis failed/empty, using config-only model", zap.Error(err))
		return configModel, nil // graceful degradation (unchanged behavior)
	}
	out := assembleAnalyze(res.ToolCalls)
	model := &project.ProjectModel{}
	*model = *configModel
	s.mergeAIInference(model, out)
	return model, nil
}
```

- [ ] **Step 5: Delete `flexibleStrings` + `firstStringValue`** from `types.go`. Change `AnalyzeOutput.TechStack` type from `flexibleStrings` to `[]string`.

- [ ] **Step 6: Update prompt** — `prompts.go`: replace `promptAnalyzeOutput` JSON with "Use report_endpoint/report_page/declare_tech to describe the surface."

- [ ] **Step 7: Update tests** — `analyze_techstack_test.go`/`scout_test.go`: the flexibleStrings object-array tests are deleted (schema now forces strings); Analyze mock fixtures switch to tool calls. `make check` → EXIT 0.

- [ ] **Step 8: Commit**

```bash
git add -A
git commit -m "feat(scout): Analyze via report_endpoint/report_page/declare_tech tools

runAIInference calls DecideWithTools + assembleAnalyze. declare_tech schema
forces []string, deleting the flexibleStrings/firstStringValue drift patch
(that absorbed LLM-emitted object-array tech stacks). AnalyzeOutput stays as
the assembly target, no longer LLM-emitted JSON."
```

---

### Task 5: contract.go → contract tools + report_contract_gap; delete Priorities patch

**Files:**
- Modify: `internal/head/scout/contract.go` — `BuildCoverageContract`/`SelfAssessContract` use `DecideWithTools` + contract assembly
- Modify: `internal/head/scout/tools.go` — add `contractTools()`, `selfAssessTools()`
- Modify: `internal/head/scout/assembly.go` — add `assembleContract(calls, depth, invariants) *contract.Contract`, `assembleGapNotes(calls) []string`
- Modify: `internal/head/contract/types.go` — delete `Priorities.UnmarshalJSON` (schema forces `map[string][]string`); keep `Priorities = map[string][]string`
- Test: `contract_test.go`, `session/contract_integration_test.go`
- Live: `TestBuildCoverageContract_LiveGLM`

**Interfaces:**
- Consumes: `DecideWithTools`, `contract.Contract`, `contract.InvariantRef`, `contract.ExpandDepth`.
- Produces: `func contractTools() []llm.Tool`; `func assembleContract(calls []llm.ToolCall, depth string, invs []contract.InvariantRef) *contract.Contract`.

- [ ] **Step 1: Write failing test (RED)**

```go
func TestAssembleContract_PrioritiesForcedStringSlice(t *testing.T) {
	calls := []llm.ToolCall{
		{Name: "set_priority", Input: map[string]any{"bucket": "high", "modules": []any{"go/build"}}},
		{Name: "set_coverage_gate", Input: map[string]any{"module": "go/build", "line_threshold": float64(0.8)}},
		{Name: "declare_scope", Input: map[string]any{"modules": []any{"a", "b"}}},
	}
	c := assembleContract(calls, "standard", nil)
	assert.Equal(t, []string{"go/build"}, c.Priorities["high"])
	assert.Equal(t, "go/build", c.CoverageGate.Module)
	assert.Equal(t, []string{"a", "b"}, c.Scope)
	assert.Equal(t, "standard", c.Depth)
}
```

- [ ] **Step 2: Verify RED** — `undefined: contractTools/assembleContract`.

- [ ] **Step 3: Add `contractTools()` + `selfAssessTools()` + `assembleContract()`**

```go
// Schema helpers (add to tools.go, reused by contractTools).
func objSchema(required []any, props map[string]any) map[string]any {
	return map[string]any{"type": "object", "properties": props, "required": required}
}
func strArrSchema() map[string]any {
	return map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
}
func enumArrSchema(vals ...string) map[string]any {
	cs := make([]any, len(vals))
	for i, v := range vals {
		cs[i] = v
	}
	return map[string]any{"type": "array", "items": map[string]any{"type": "string", "enum": cs}}
}

func contractTools() []llm.Tool {
	return []llm.Tool{
		{Name: "declare_scope", InputSchema: objSchema([]any{"modules"}, map[string]any{"modules": strArrSchema()})},
		{Name: "declare_path_types", InputSchema: objSchema([]any{"types"}, map[string]any{"types": enumArrSchema("happy", "alternative", "boundary", "edge")})},
		{Name: "declare_error_scope", InputSchema: objSchema([]any{"scopes"}, map[string]any{"scopes": enumArrSchema("4xx", "validation", "exception")})},
		{Name: "declare_boundaries", InputSchema: objSchema([]any{"boundaries"}, map[string]any{"boundaries": enumArrSchema("empty", "zero", "max", "invalid", "extreme")})},
		{Name: "set_priority", InputSchema: objSchema([]any{"bucket", "modules"}, map[string]any{
			"bucket": map[string]any{"type": "string"}, "modules": strArrSchema()})},
		{Name: "set_coverage_gate", InputSchema: objSchema([]any{"module"}, map[string]any{
			"module": map[string]any{"type": "string"}, "line_threshold": map[string]any{"type": "number"},
			"branch_threshold": map[string]any{"type": "number"}})},
	}
}
func selfAssessTools() []llm.Tool {
	return []llm.Tool{{Name: "report_contract_gap", Description: "Report one coverage gap.",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{"note": map[string]any{"type": "string"}}, "required": []any{"note"}}}}
}
// obj/enumArr are local schema helpers (add alongside strArr in tools.go).

func assembleContract(calls []llm.ToolCall, depth string, invs []contract.InvariantRef) *contract.Contract {
	c := &contract.Contract{Depth: depth, Priorities: contract.Priorities{}, Invariants: invs}
	for _, call := range calls {
		switch call.Name {
		case "declare_scope":
			c.Scope = strSliceField(call, "modules")
		case "declare_path_types":
			c.PathTypes = strSliceField(call, "types")
		case "declare_error_scope":
			c.ErrorScope = strSliceField(call, "scopes")
		case "declare_boundaries":
			c.Boundaries = strSliceField(call, "boundaries")
		case "set_priority":
			c.Priorities[strField(call, "bucket")] = strSliceField(call, "modules")
		case "set_coverage_gate":
			c.CoverageGate = contract.Gate{Module: strField(call, "module"),
				LineThreshold: numField(call, "line_threshold"), BranchThreshold: numField(call, "branch_threshold")}
		}
	}
	return c
}
```

- [ ] **Step 4: Rewrite `BuildCoverageContract` + `SelfAssessContract`**

```go
func (s *Scout) BuildCoverageContract(ctx context.Context, goal string, model *project.ProjectModel, depth string) (*contract.Contract, error) {
	contract.ExpandDepth(depth) // keep for any side effect / validation
	prompt := ai.NewPrompt().System(promptContractSystem).
		Context(s.buildAnalyzeContext(TargetInfo{Goal: goal})).
		Task(fmt.Sprintf("Goal: %s\nDepth: %s\nDefine the coverage contract via tools.", goal, depth)).Build()
	res, err := s.driver.DecideWithTools(ctx, prompt, contractTools())
	if err != nil || len(res.ToolCalls) == 0 {
		return nil, fmt.Errorf("build coverage contract: %w", err)
	}
	var invs []contract.InvariantRef
	for _, inv := range s.config.Invariants {
		invs = append(invs, contract.InvariantRef{ID: inv.ID, Description: inv.Description})
	}
	return assembleContract(res.ToolCalls, depth, invs), nil
}

func (s *Scout) SelfAssessContract(ctx context.Context, c *contract.Contract) ([]string, error) {
	prompt := ai.NewPrompt().System(`Critique the coverage contract for gaps via report_contract_gap.`).
		Task(fmt.Sprintf("Contract: %+v", c)).Build()
	res, err := s.driver.DecideWithTools(ctx, prompt, selfAssessTools())
	if err != nil {
		return nil, fmt.Errorf("self-assess contract: %w", err)
	}
	var notes []string
	for _, call := range res.ToolCalls {
		if call.Name == "report_contract_gap" {
			notes = append(notes, strField(call, "note"))
		}
	}
	return notes, nil
}
```

- [ ] **Step 5: Delete `Priorities.UnmarshalJSON`** in `contract/types.go` (schema forces `[]string`; keep `type Priorities map[string][]string`). Delete its tests.

- [ ] **Step 6: Update tests + live gate** — `contract_test.go` switches to tool fixtures. Add `TestBuildCoverageContract_LiveGLM` (gated `-tags live`) asserting GLM emits the six contract tools. `make check` → EXIT 0.

- [ ] **Step 7: Commit**

```bash
git add -A
git commit -m "feat(scout): contract.go via contract tools + report_contract_gap

BuildCoverageContract uses 6 contract tools (declare_scope/path_types/
error_scope/boundaries, set_priority, set_coverage_gate); SelfAssessContract
uses report_contract_gap. set_priority schema forces []string, deleting the
Priorities.UnmarshalJSON dual-shape drift patch."
```

---

### Task 6: Delete verifyServiceAttribution (service attribution is now in assembly)

**Files:**
- Modify: `internal/head/scout/direct_planning.go` — delete `verifyServiceAttribution`, `ServiceAttributionCorrection(s)`, `formatCasesForVerification`
- Test: delete `direct_planning_test.go` verify tests

**Interfaces:** None new. `attributeService` (used by `assembleHTTP`, Task 1) already performs deterministic override, so `verifyServiceAttribution` is dead after Task 3 removed `convertPlanOutput`.

- [ ] **Step 1: Confirm dead** — `grep -rn "verifyServiceAttribution\|ServiceAttributionCorrections\|formatCasesForVerification" internal --include="*.go"` → only definitions + their own tests remain (no live caller after Task 3).

- [ ] **Step 2: Delete the three symbols + their tests** from `direct_planning.go` and `direct_planning_test.go`.

- [ ] **Step 3: `make check` + commit**

```bash
git add -A
git commit -m "refactor(scout): delete verifyServiceAttribution

Service attribution moved into assemblePlan (attributeService overrides wrong
LLM tags deterministically). The post-hoc LLM correction round-trip and its
ServiceAttributionCorrections/formatCasesForVerification helpers are dead."
```

---

### Task 7: ToT evaluate deterministicization

**Files:**
- Modify: `internal/head/scout/tot_evaluators.go` — replace `aiScore`/`evaluateDriver.Decide` with multi-signal deterministic score + fail-safe guard
- Modify: `internal/head/scout/tot.go` — drop `evaluateDriver` field (or keep unused if ToT.propose still needs it in S3; **propose stays on Decide in S2**, so keep `evaluateDriver` for propose, remove only its evaluate use)
- Modify: `internal/head/scout/tot_helpers.go` — add signal helpers (invariantCoverage, pageCoverage, actionDiversity, goalOverlap)
- Test: `tot_test.go` — per-signal unit tests + ranking regression

**Interfaces:**
- Consumes: `PlanCandidate`, `project.ProjectModel`, `coverageScore` (existing).
- Produces: `func (t *ToTPlanner) deterministicScore(c *PlanCandidate, model *project.ProjectModel, goal string) float64` (0–1).

> Note: ToT.propose stays on `Decide` (deferred to S3). Only `evaluate` is deterministicized. Keep `proposeDriver`/`evaluateDriver` fields; `evaluate` no longer calls the LLM.

- [ ] **Step 1: Write failing test (RED)**

```go
func TestDeterministicScore_RanksCoveringCandidateHigher(t *testing.T) {
	model := &project.ProjectModel{API: project.APIModel{Endpoints: []project.EndpointDef{{Method: "GET", Path: "/users"}}}}
	model.InvariantHints = []project.InvariantHint{{ID: "inv1", Description: "users must be unique"}}
	high := &PlanCandidate{Cases: []string{"GET /users", "check inv1 users must be unique"}}
	low := &PlanCandidate{Cases: []string{"check something unrelated"}}
	planner := NewToTPlanner(nil, nil, DefaultToTConfig(), testLogger(t))
	hs := planner.deterministicScore(high, model, "test users")
	ls := planner.deterministicScore(low, model, "test users")
	assert.Greater(t, hs, ls, "candidate covering endpoints+invariants must score higher")
}

func TestDeterministicScore_FloorTriggersFailSafe(t *testing.T) {
	// "x" matches no endpoint/invariant/page/goal token → score ≈0.06 < floor 0.10.
	model := &project.ProjectModel{API: project.APIModel{Endpoints: []project.EndpointDef{{Method: "GET", Path: "/users"}}}}
	model.InvariantHints = []project.InvariantHint{{ID: "inv1", Description: "users must be unique"}}
	planner := NewToTPlanner(nil, nil, DefaultToTConfig(), testLogger(t))
	empty := []PlanCandidate{{Cases: []string{"x"}}}
	_, err := planner.evaluate(context.Background(), empty, model, "test users")
	assert.Error(t, err)
}
```

- [ ] **Step 2: Verify RED** — `undefined: deterministicScore`.

- [ ] **Step 3: Implement multi-signal score + helpers**

In `tot_evaluators.go`, replace `evaluate` and delete `aiScore`:

```go
// evaluate scores candidates deterministically (no LLM). Signals: endpoint
// coverage, invariant coverage, page coverage, action diversity, goal overlap.
// Fail-safe: if the top score is below floorScore, returns an error instead of
// silently ranking near-random candidates (the analogue of the old "all AI
// scores failed" systemic signal).
// evaluate now takes goal (for the goal-overlap signal). Update tot.go Plan's
// call site: `scored, err := t.evaluate(ctx, expanded, model, goal)` (goal is
// already in Plan's scope).
func (t *ToTPlanner) evaluate(ctx context.Context, candidates []PlanCandidate, model *project.ProjectModel, goal string) ([]PlanCandidate, error) {
	for i := range candidates {
		c := candidates[i]
		c.Score = t.deterministicScore(&c, model, goal)
		c.Coverage = t.coverageScore(&c, model)
		c.AIScore = 0 // legacy field; deterministic replacement
		candidates[i] = c
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Score > candidates[j].Score })
	if len(candidates) > 0 && candidates[0].Score < floorScore {
		return candidates, fmt.Errorf("tot evaluate: top score %.3f below floor %.3f; nothing actionable", candidates[0].Score, floorScore)
	}
	return candidates, nil
}

const floorScore = 0.10

func (t *ToTPlanner) deterministicScore(c *PlanCandidate, model *project.ProjectModel, goal string) float64 {
	ep := t.coverageScore(c, model)                       // 0.30
	inv := t.invariantCoverage(c, model)                  // 0.25
	pg := t.pageCoverage(c, model)                        // 0.12
	div := t.actionDiversity(c)                           // 0.20
	goalOL := t.goalOverlap(c, goal)                      // 0.13
	return 0.30*ep + 0.25*inv + 0.12*pg + 0.20*div + 0.13*goalOL
}
```

In `tot_helpers.go`, add (each returns 0–1):

```go
func (t *ToTPlanner) invariantCoverage(c *PlanCandidate, model *project.ProjectModel) float64 {
	if len(model.InvariantHints) == 0 {
		return 0.5
	}
	text := strings.ToLower(strings.Join(c.Cases, " "))
	matched := 0
	for _, inv := range model.InvariantHints {
		if strings.Contains(text, strings.ToLower(inv.ID)) || strings.Contains(text, strings.ToLower(inv.Description)) {
			matched++
		}
	}
	return float64(matched) / float64(len(model.InvariantHints))
}
func (t *ToTPlanner) pageCoverage(c *PlanCandidate, model *project.ProjectModel) float64 {
	if len(model.Navigation.Pages) == 0 {
		return 0.5
	}
	text := strings.ToLower(strings.Join(c.Cases, " "))
	matched := 0
	for _, pg := range model.Navigation.Pages {
		if strings.Contains(text, strings.ToLower(pg.Path)) {
			matched++
		}
	}
	return float64(matched) / float64(len(model.Navigation.Pages))
}
func (t *ToTPlanner) actionDiversity(c *PlanCandidate) float64 {
	set := map[string]bool{}
	for _, cs := range c.Cases {
		l := strings.ToLower(cs)
		for _, k := range []string{"get", "post", "error", "edge", "boundary", "invariant", "ws"} {
			if strings.Contains(l, k) {
				set[k] = true
			}
		}
	}
	return math.Min(float64(len(set))/4.0, 1.0) // saturate at 4 distinct angles
}
func (t *ToTPlanner) goalOverlap(c *PlanCandidate, goal string) float64 {
	if goal == "" {
		return 0.5
	}
	text := strings.ToLower(strings.Join(c.Cases, " "))
	toks := strings.Fields(strings.ToLower(goal))
	if len(toks) == 0 {
		return 0.5
	}
	matched := 0
	for _, tk := range toks {
		if len(tk) > 2 && strings.Contains(text, tk) {
			matched++
		}
	}
	return float64(matched) / float64(len(toks))
}
```

Add `"math"` and `"sort"` imports. Drop the `sync`/`atomic` concurrency (deterministic scoring is cheap; serial is fine) — or keep parallel; either passes tests.

- [ ] **Step 4: Run tests GREEN** — `go test ./internal/head/scout/ -run 'TestDeterministic|TestToT' -v`.

- [ ] **Step 5: Update existing ToT tests** — `tot_test.go`: any test asserting `AIScore>0` or mocking `evaluateDriver` is updated (AIScore is now 0; evaluate needs no driver). `reasoning` assertions removed.

- [ ] **Step 6: `make check` + commit**

```bash
git add -A
git commit -m "feat(scout): deterministic ToT evaluate (multi-signal score)

Replace the 70% LLM aiScore with a deterministic multi-signal score (endpoint
coverage 0.30, invariant coverage 0.25, page coverage 0.12, action diversity
0.20, goal overlap 0.13). Fail-safe guard: top score below floor -> error
(analogue of the old all-AI-failed systemic signal). ToT.propose stays on
Decide until S3."
```

---

## Post-implementation verification

After Task 7, run the full gate:
- `make check` → EXIT 0.
- `grep -rn "\.Decide(" internal/head/scout --include="*.go" | grep -v _test` → only `tot_generators.go` (ToT.propose, deferred to S3) remains. All other Scout call sites use `DecideWithTools`.
- Confirm deletions: `grep -rn "flexibleStrings\|expandWSRelayCases\|verifyServiceAttribution\|Priorities.UnmarshalJSON\|PlanOutput\|CaseInfo" internal --include="*.go"` → zero hits (except comments referencing the migration).

## Self-Review notes

- **Spec coverage:** all 7 call sites mapped (1 directPlan via Tasks 1–3; 2 Analyze via Task 4; 3 verify via Task 6; 4 ToT.evaluate via Task 7; 5–6 contract via Task 5; 7 ToT.propose explicitly deferred). Drift patches deleted: flexibleStrings (T4), relayIntent body-JSON (T3), Priorities (T5), PlanOutput (T3). Fallback split: drift→error (T3), transient→fallbackPlan (T3). Assembly details pinned: covered threading (T3), fillBody retained (T1), Action ws_flow (T2), service override (T1). Enum fixes: run_process build|exec (T1), analyze_code all three (T1). Test impact inventoried per task.
- **Type consistency:** `assemblePlan(calls, goal, baseURL, services) (*agent.TestPlan, map[string]map[string]bool)` consistent across T1/T2/T3. `planTools()/analyzeTools()/contractTools()/selfAssessTools()` consistent. `deterministicScore(c, model, goal) float64` consistent T7.
- **Live gates:** directPlan (T3) + BuildCoverageContract (T5).
