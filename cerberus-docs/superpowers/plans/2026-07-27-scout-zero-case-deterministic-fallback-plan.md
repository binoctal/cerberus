# Scout Zero-Case → Deterministic Fallback Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stop `Scout.Plan` from aborting when the LLM produces zero cases, so protocol-derived deterministic cases (e.g. the WS relay case) still run. Abort only when neither the LLM nor deterministic augmentation produced any case.

**Architecture:** Two coupled edits. (1) In `runAIPlanning` the two zero exits (zero tool calls; zero assembled cases) return an empty plan + `nil` error + a debug log instead of a fatal error. (2) In `Scout.Plan` a post-augmentation guard returns an error only if the final plan is still empty. No signature changes; the transient-error→`fallbackPlan` path and ToT path are untouched.

**Tech Stack:** Go 1.25 · `github.com/binoctal/cerberus` · existing `llm.MockClient` + `zaptest/observer` test patterns.

**Spec:** `cerberus-docs/superpowers/specs/2026-07-27-scout-zero-case-deterministic-fallback-design.md`

## Global Constraints

- Go 1.25, module `github.com/binoctal/cerberus`, pure Go (no CGo), no new external dependency.
- `runAIPlanning` signature stays `(*agent.TestPlan, map[string]map[string]bool, error)`. `assemblePlan`, `WSCasesCovered`, `GenerateExecutorCases`, `DetectProjectType` are NOT modified.
- The transient-error → `fallbackPlan` path (direct_planning.go:64-67) is NOT changed; the ToT path is NOT changed; A1 coexistence suppression (ws_cases.go:57-61) is NOT changed.
- Commit author `binoctal <binoctal@gmail.com>`, NO Co-Authored-By, messages in English, conventional-commit style.
- `make check` (fmt + lint + test -race) EXIT 0.
- All docs in `cerberus-docs/` only.
- Behavior is additive for protocol/executor goals (more runs reach execution); the only new abort is "no cases generated" when nothing at all applies.

---

## File Structure

- **modify** `internal/head/scout/direct_planning.go:69-71` — zero-tool-calls exit → empty plan + nil.
- **modify** `internal/head/scout/direct_planning.go:85-87` — zero-assembled exit → empty plan + nil.
- **modify** `internal/head/scout/plan_phases.go:87-90` — add post-augment `len(plan.Cases)==0` guard.
- **modify** `internal/head/scout/direct_planning_test.go` — `wsRelayConfig()` helper + 3 tests.

---

## Task 1: Zero-case → deterministic fallback (behavior + guard + tests)

**Files:**
- Modify: `internal/head/scout/direct_planning.go:69-71` and `:85-87`
- Modify: `internal/head/scout/plan_phases.go:87-90`
- Test: `internal/head/scout/direct_planning_test.go` (append helper + 3 tests)

**Interfaces:**
- Consumes: `s.logger` (*zap.Logger on *Scout), `agent.TestPlan{}` (struct, types.go:78), `assemblePlan` (returns non-nil *TestPlan even on empty, assembly.go:114), `project.CodeConfig{Root}` (schema.go:65).
- Produces: no new symbols — `runAIPlanning` still returns the same signature; the two zero exits now succeed with an empty plan.

- [ ] **Step 1: Write the failing tests**

Append to `internal/head/scout/direct_planning_test.go`:

```go
// wsRelayConfig is a minimal config whose declared protocol makes
// WSCasesCovered emit the deterministic peer-join relay case (web has an
// OPTIONAL handshake awaiting a signal; bridge is the peer). Used by the
// zero-case fallback tests. The URL value is irrelevant — cases are not
// executed in these tests.
func wsRelayConfig() *project.Config {
	return &project.Config{
		Project: project.ProjectMeta{Name: "relay"},
		Services: []project.Service{{
			Name: "realtime",
			URL:  "http://localhost:8989/u",
			Protocol: &project.Protocol{
				TypePath: "type",
				Roles: map[string]*project.ProtocolRole{
					"web": {
						Params:    map[string]string{"type": "web"},
						Handshake: &project.RoleHandshake{AwaitType: "device:online", Optional: true, Timeout: 2},
					},
					"bridge": {Params: map[string]string{"type": "bridge"}},
				},
			},
		}},
	}
}

// TestPlan_ZeroToolCalls_ProceedsToDeterministic asserts that when the LLM
// returns zero tool calls, Scout.Plan no longer aborts: it proceeds to
// deterministic augmentation and the protocol-derived relay case is generated.
// (Reproduces the 2026-07-27 dogfood abort; FAILS today with "zero tool calls".)
func TestPlan_ZeroToolCalls_ProceedsToDeterministic(t *testing.T) {
	mock := llm.NewMockClient(nil)
	mock.SetToolResponse("zero tool calls relay goal", []llm.ToolCall{}) // zero tool calls
	driver := ai.NewDriver(mock, ai.NewTokenBudget(200000, 10000))
	sct := NewScout(driver, setupTestStore(t), wsRelayConfig(), zap.NewNop())

	plan, err := sct.Plan(context.Background(), "zero tool calls relay goal", &project.ProjectModel{})
	require.NoError(t, err, "zero LLM tool calls must not abort when deterministic cases apply")
	var hasRelay bool
	for _, c := range plan.Cases {
		if c.ID == "ws-realtime-relay-web-signal-device-online" {
			hasRelay = true
		}
	}
	require.True(t, hasRelay, "expected the deterministic relay case; got case IDs: %v", caseIDStrings(plan.Cases))
}

// TestPlan_ZeroAssembled_ProceedsToDeterministic: the LLM returns a bare
// begin_case with no ws_* follow-ups, which assembly drops (empty ws_flow) →
// zero assembled cases. The plan must still proceed to deterministic cases.
func TestPlan_ZeroAssembled_ProceedsToDeterministic(t *testing.T) {
	mock := llm.NewMockClient(nil)
	mock.SetToolResponse("zero assembled relay goal", []llm.ToolCall{
		{Name: "begin_case", Input: map[string]any{"name": "x", "expectation": "ok", "service": "realtime"}},
		// no ws_* follows → assembly drops the empty ws_flow case → 0 assembled cases
	})
	driver := ai.NewDriver(mock, ai.NewTokenBudget(200000, 10000))
	sct := NewScout(driver, setupTestStore(t), wsRelayConfig(), zap.NewNop())

	plan, err := sct.Plan(context.Background(), "zero assembled relay goal", &project.ProjectModel{})
	require.NoError(t, err)
	require.NotEmpty(t, plan.Cases, "deterministic relay case should survive a zero-assembled LLM round")
}

// TestPlan_NoCasesAtAll_Aborts: zero LLM tool calls + no protocol + a non-code
// root (so GenerateExecutorCases is also empty) → the augmented plan is empty
// → Scout.Plan returns an error naming the cause.
func TestPlan_NoCasesAtAll_Aborts(t *testing.T) {
	mock := llm.NewMockClient(nil)
	mock.SetToolResponse("nothing applies goal", []llm.ToolCall{})
	driver := ai.NewDriver(mock, ai.NewTokenBudget(200000, 10000))
	cfg := &project.Config{
		Project: project.ProjectMeta{Name: "empty"},
		Code:    project.CodeConfig{Root: t.TempDir()}, // non-code dir → no executor cases
	}
	sct := NewScout(driver, setupTestStore(t), cfg, zap.NewNop())

	_, err := sct.Plan(context.Background(), "nothing applies goal", &project.ProjectModel{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "no cases generated")
}

// caseIDStrings returns case IDs for assertion-failure messages.
func caseIDStrings(cases []agent.TestCase) []string {
	ids := make([]string, len(cases))
	for i, c := range cases {
		ids[i] = c.ID
	}
	return ids
}
```

Add `"github.com/binoctal/cerberus/internal/head/agent"` to the test file's imports if not already present (needed by `caseIDStrings`).

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/head/scout/ -run 'TestPlan_ZeroToolCalls_ProceedsToDeterministic|TestPlan_ZeroAssembled_ProceedsToDeterministic|TestPlan_NoCasesAtAll_Aborts' -v`
Expected: FAIL — the first two fail with `"zero tool calls"`/`"assembly produced zero cases"` errors (Plan aborts); the third FAILS THE OPPOSITE way (today Plan aborts with `"zero tool calls"`, not `"no cases generated"` — so `require.Contains(... "no cases generated")` fails). All three are RED for the intended post-change behavior.

- [ ] **Step 3: Change the zero-tool-calls exit (direct_planning.go:69-71)**

Replace:

```go
	if len(res.ToolCalls) == 0 {
		return nil, nil, fmt.Errorf("scout plan: zero tool calls (drift or quality)")
	}
```

with:

```go
	if len(res.ToolCalls) == 0 {
		// Zero tool calls is no longer fatal: proceed to deterministic
		// augmentation (WSCasesCovered + GenerateExecutorCases). Scout.Plan's
		// post-augment guard aborts only if the final plan is still empty.
		s.logger.Debug("scout planning proceeding to deterministic augmentation",
			zap.String("reason", "zero tool calls"))
		return &agent.TestPlan{}, map[string]map[string]bool{}, nil
	}
```

- [ ] **Step 4: Change the zero-assembled exit (direct_planning.go:85-87)**

Replace:

```go
	if len(plan.Cases) == 0 {
		s.logger.Debug("scout planning produced zero cases", zap.Int("tool_calls", len(res.ToolCalls)))
		return nil, nil, fmt.Errorf("scout plan: assembly produced zero cases")
	}
```

with:

```go
	if len(plan.Cases) == 0 {
		// Assembled zero cases: not fatal — proceed to deterministic augmentation.
		s.logger.Debug("scout planning proceeding to deterministic augmentation",
			zap.String("reason", "assembly produced zero cases"),
			zap.Int("tool_calls", len(res.ToolCalls)))
		return plan, covered, nil
	}
```

(`assemblePlan` always returns a non-nil `*agent.TestPlan` — assembly.go:114 — so `plan` is safe to return; its `Cases` is empty/nil.)

- [ ] **Step 5: Add the post-augment guard (plan_phases.go:87-90)**

Replace:

```go
	// Phase 3: Augment plan with executor cases
	s.augmentPlan(plan, goal, covered)

	return plan, nil
}
```

with:

```go
	// Phase 3: Augment plan with executor cases
	s.augmentPlan(plan, goal, covered)

	// A zero-case LLM round now reaches here (runAIPlanning returns an empty
	// plan instead of erroring). Abort only when neither the LLM nor
	// deterministic augmentation produced any runnable case.
	if len(plan.Cases) == 0 {
		return nil, fmt.Errorf("scout plan: no cases generated (LLM produced none; no deterministic cases apply to this goal/project)")
	}
	return plan, nil
}
```

`plan_phases.go` already imports `"fmt"` and `agent` — verify no new import is needed (if `fmt` is not imported, add it).

- [ ] **Step 6: Run the three tests to verify they pass**

Run: `go test ./internal/head/scout/ -run 'TestPlan_ZeroToolCalls_ProceedsToDeterministic|TestPlan_ZeroAssembled_ProceedsToDeterministic|TestPlan_NoCasesAtAll_Aborts' -v`
Expected: PASS — all three green.

- [ ] **Step 7: Run the full scout package with race (no regressions)**

Run: `go test -race ./internal/head/scout/`
Expected: PASS — the existing `TestDirectPlan_ToolCallingAssembly`, the fallback-on-error tests, and the Task-2 observer tests all still green.

- [ ] **Step 8: Commit**

```bash
git add internal/head/scout/direct_planning.go internal/head/scout/plan_phases.go internal/head/scout/direct_planning_test.go
git commit -m "fix(scout): proceed to deterministic cases on zero LLM output"
```

---

## Task 2: Verification gate — make check + live dogfood

**Files:** none (verification only).

- [ ] **Step 1: `make check`**

Run: `make check 2>&1 | tail -25; echo "EXIT=${PIPESTATUS[0]}"`
Expected: EXIT 0.

- [ ] **Step 2: Live dogfood — relay case survives a zero-case LLM round**

Bring up the open-agents dev server and run the relay dogfood with debug logging (per cccmemory `cerberus-logging-debug-howto`). The run log should now show, even on a GLM zero-case round, `"scout planning proceeding to deterministic augmentation"` and a plan that includes `ws-realtime-relay-web-signal-device-online`, so the relay verdict is reachable (no `assembly produced zero cases` abort). If a live server is not available, this step is deferred to manual verification — the unit tests in Task 1 are the deterministic proof.

```bash
cd ../open-agents/apps/api && (fnm use 22 2>/dev/null || true) && npm run dev &   # :8989
cd /home/mason/Documents/code_projects/private/cerberus && make build
# provision + write a tmpdir relay config (tokens in credentials.yaml), then:
CERBERUS_LOG_LEVEL=debug ./build/cerberus run --config <tmp>/project.yaml --dir <tmp> --db <tmp>.db \
  --goal "web and bridge connect to realtime; web receives relayed device:online." 2>&1 \
  | grep -E "proceeding to deterministic|relay-web-signal-device-online|no cases generated"
```
Expected: a `proceeding to deterministic augmentation` line and the relay case in the plan (the abort no longer fires on a zero-case round).

- [ ] **Step 3: No commit (verification-only task)**

If all green, the feature is complete. The implementation commit is Task 1.

---

## Self-Review (completed by plan author)

- **Spec coverage:** Change 1 (two zero exits → empty plan + nil) → Task 1 Steps 3-4. Change 2 (post-augment guard) → Task 1 Step 5. Testing § (3 tests) → Task 1 Steps 1-2. Verification § → Task 2. "What stays unchanged" (transient-error fallback, ToT, A1, signatures) → Global Constraints + the edits touch none of them. All covered.
- **Placeholder scan:** none — every code step contains real, runnable code with exact old→new blocks.
- **Type consistency:** `runAIPlanning` return `(emptyPlan, emptyMap, nil)` matches `(*agent.TestPlan, map[string]map[string]bool, error)`. `agent.TestPlan{}` is the zero-value struct (types.go:78). `assemblePlan` returns non-nil plan (assembly.go:114). `project.CodeConfig{Root}` matches schema.go:65. The relay case ID `ws-realtime-relay-web-signal-device-online` matches `wsRelayCases`'s `"ws-"+svc.Name+"-relay-"+aName+"-signal-"+sanitizeTypeID(signal)` (realtime/web/device:online → device-online).
- **Risk noted for the implementer:** if `GenerateExecutorCases(ProjectHTTP)` unexpectedly yields cases, `TestPlan_NoCasesAtAll_Aborts` would fail (plan non-empty) — confirm `DetectProjectType` on a `t.TempDir()` returns `ProjectHTTP` with empty executor cases; the `Code.Root=t.TempDir()` setup is what makes the abort path reachable.
