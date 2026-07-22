# WS Deterministic Multi-Step Cases (TestCase.Steps) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let one TestCase carry ordered WS `Steps` (connect→send→receive→assert) that execute deterministically — no Steer LLM — sharing one connection.

**Architecture:** Add `TestCase.Steps []TestStep`. In `executeStep`, a new Phase 0 (`len(tc.Steps) > 0 ⇒ se.runSteps()`) runs before the rule engine / Steer. `runSteps` converts each `TestStep` to the existing `types.WS*Action`, executes it via the already-wired `r.executor.Execute` (same path Steer uses), and aggregates a `StepResult`, short-circuiting on the first failed step. Connection sharing is automatic: every step runs under the case's `caseIDKey` context, and the WS connection table keys on `<caseID>:<connectionID>`, so steps citing the same `connection_id` reuse one connection — **no connection-table or executor change**.

**Tech Stack:** Go 1.25, `github.com/coder/websocket` v1.8.14 (only), no new deps, no expression evaluator.

**Spec:** `cerberus-docs/superpowers/specs/2026-07-23-ws-deterministic-steps-design.md`

## Global Constraints

- Go 1.25, module `github.com/binoctal/cerberus`, pure Go (no CGo).
- WS library fixed at `github.com/coder/websocket` v1.8.14; `nhooyr.io/websocket` forbidden.
- **No runtime expression evaluation.** `Asserts` are constrained dotted-path → value exact match (numeric-normalized), reusing `doReceive`/`checkAsserts`.
- No new third-party dependencies (stdlib `slices`/`maps`/`encoding/json` only).
- Determinism: sorted iteration where maps are involved; no map-order nondeterminism in generated output.
- Commit author `binoctal <binoctal@gmail.com>`; no `Co-Authored-By`; comments + commit messages in English.
- `make check` (fmt + lint + test -race) green after every task.
- Tests table-driven where applicable; existing WS tests must stay green.

## File Structure

- **Modify** `internal/head/agent/types.go` — add `TestStep` struct + `TestCase.Steps` field.
- **Create** `internal/head/agent/execute_phases_steps.go` — `stepExecution.runSteps()` orchestrator + `TestStep`→`types.TypedAction` conversion.
- **Modify** `internal/head/agent/execute_phases.go` — add Phase 0 Steps branch before `tryRuleEngine`.
- **Create** `internal/head/agent/execute_phases_steps_test.go` — `runSteps` TDD against an in-process WS server (pass + short-circuits).
- **Modify** `internal/head/scout/ws_cases.go` — `WSCases` emits one `Steps` case for a goal-derived send/receive exchange (connect-only otherwise).
- **Modify** `internal/head/scout/ws_cases_test.go` — update/grow for `Steps` output.
- **Modify** `cerberus-docs/executors/websocket.md` — document `Steps`.

## TestStep schema (refines spec)

The spec's `Body string` is split into purpose-specific fields so each maps 1:1 to the existing `types.WS*Action` (no overload ambiguity):

```go
// TestStep is one declarative WS action inside a deterministic multi-step case.
type TestStep struct {
	Action       string         `json:"action"`                  // ws_connect|ws_send|ws_receive|ws_disconnect
	ConnectionID string         `json:"connection_id,omitempty"` // case-scoped; same id across steps ⇒ shared connection
	Role         string         `json:"role,omitempty"`          // ws_connect: protocol role (auth + handshake await_type)
	Message      string         `json:"message,omitempty"`       // ws_send: payload (raw JSON string)
	Type         string         `json:"type,omitempty"`          // ws_receive: awaited routing-type value
	Asserts      map[string]any `json:"asserts,omitempty"`       // ws_receive: dotted-path→value exact checks
	Timeout      int            `json:"timeout,omitempty"`       // ws_receive: seconds (0 ⇒ executor default)
}
```

---

### Task 1: `TestStep` type + `TestCase.Steps` field

**Files:**
- Modify: `internal/head/agent/types.go` (after the `TestCase` struct, ~line 39).
- Test: `internal/head/agent/types_test.go` (create if absent; else append).

**Interfaces:**
- Produces: `TestStep` struct, `TestCase.Steps []TestStep` (json `steps,omitempty`).

- [ ] **Step 1: Write the failing test** (round-trip + that `Steps` is optional)

```go
func TestTestCaseStepsRoundTrip(t *testing.T) {
	in := TestCase{
		ID: "ws-rt-web-flow", Target: "http://x", Action: "ws_flow",
		Steps: []TestStep{
			{Action: "ws_connect", ConnectionID: "c1", Role: "web"},
			{Action: "ws_send", ConnectionID: "c1", Message: `{"type":"device:command"}`},
			{Action: "ws_receive", ConnectionID: "c1", Type: "device:ack",
				Asserts: map[string]any{"payload.approved": true}},
		},
	}
	b, err := json.Marshal(in)
	require.NoError(t, err)
	var out TestCase
	require.NoError(t, json.Unmarshal(b, &out))
	assert.Equal(t, in.Steps, out.Steps)

	// Steps is optional: a case without Steps round-trips with no steps field.
	bare, err := json.Marshal(TestCase{ID: "x", Action: "api_request"})
	require.NoError(t, err)
	assert.NotContains(t, string(bare), `"steps"`)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/head/agent/ -run TestTestCaseStepsRoundTrip -v`
Expected: FAIL — `TestStep` undefined / unknown field `steps`.

- [ ] **Step 3: Add the type**

In `internal/head/agent/types.go`, add `Steps []TestStep \`json:"steps,omitempty"\`` to `TestCase`, and define `TestStep` (schema above) with a doc comment noting: non-empty `Steps` ⇒ the deterministic multi-step path runs the steps and `Action`/`Body` are ignored for execution.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/head/agent/ -run TestTestCaseStepsRoundTrip -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/head/agent/types.go internal/head/agent/types_test.go
git commit -m "feat(ws): add TestStep type and TestCase.Steps field"
```

---

### Task 2: `runSteps` orchestrator + Phase 0 hook (TDD: pass + short-circuits)

**Files:**
- Create: `internal/head/agent/execute_phases_steps.go`
- Create: `internal/head/agent/execute_phases_steps_test.go`
- Modify: `internal/head/agent/execute_phases.go:60` (insert Phase 0 before Phase 1).

**Interfaces:**
- Consumes: `stepExecution` (execute_phases_types.go), `r.executor.Execute(ctx, action)` (the MultiExecutor that already dispatches WS via the protocol-aware WebSocketExecutor), `r.recordEvidence(ctx, traceID, source, action, result)`, `StepResult`/`Evidence`/`StepPassed`/`StepFailed`, `types.WSConnectAction`/`WSSendAction`/`WSReceiveAction`/`WSDisconnectAction`.
- Produces: `func (se *stepExecution) runSteps() StepResult`.

**Conversion (TestStep → types.WS*Action):** the connect step's URL is the case's `Target` (Scout sets `Target = svc.URL`); `ConnectionID` flows through so steps share the connection.

```go
package agent

import (
	"fmt"
	"time"

	"github.com/binoctal/cerberus/internal/types"
)

// stepToAction converts a declarative TestStep into the typed WS action the
// shared executor already dispatches. The connect step dials tc.Target; role
// drives protocol auth + handshake exactly as a Steer-emitted ws_connect does.
func stepToAction(tc *TestCase, s TestStep) (types.TypedAction, error) {
	switch s.Action {
	case "ws_connect":
		return types.WSConnectAction{URL: tc.Target, Role: s.Role, ConnectionID: s.ConnectionID}, nil
	case "ws_send":
		return types.WSSendAction{ConnectionID: s.ConnectionID, Message: s.Message}, nil
	case "ws_receive":
		return types.WSReceiveAction{ConnectionID: s.ConnectionID, Type: s.Type, Assert: s.Asserts, Timeout: s.Timeout, Decisive: true}, nil
	case "ws_disconnect":
		return types.WSDisconnectAction{ConnectionID: s.ConnectionID}, nil
	default:
		return nil, fmt.Errorf("steps: unknown action %q", s.Action)
	}
}

// runSteps executes a deterministic multi-step WS case: each step runs via the
// shared executor under the case context (caseIDKey already set by executeStep),
// so steps citing the same connection_id share one connection. The first failed
// step short-circuits the case. The decisive verdict is the final ws_receive
// assert; a completed chain is a real upgraded exchange for the Examiner.
func (se *stepExecution) runSteps() StepResult {
	r := se.loop
	var evidence []Evidence
	var lastAction types.TypedAction
	var lastResult types.ExecutorResult
	for _, s := range se.tc.Steps {
		action, err := stepToAction(se.tc, s)
		if err != nil {
			return se.failureResult(err, 1)
		}
		result := r.executor.Execute(se.ctx, action)
		r.recordEvidence(se.ctx, se.traceID, "steps", action, result)
		evidence = append(evidence, Evidence{Type: evidenceType(result), Content: fmt.Sprintf("%s: %s", s.Action, result.Summary())})
		lastAction, lastResult = action, result
		if !result.Success() {
			return StepResult{TestCase: se.tc, Status: StepFailed, TraceID: se.traceID,
				Attempts: 1, Duration: time.Since(se.start), Action: action, Result: result, Evidence: evidence}
		}
	}
	return StepResult{TestCase: se.tc, Status: StepPassed, TraceID: se.traceID,
		Attempts: 1, Duration: time.Since(se.start), Action: lastAction, Result: lastResult, Evidence: evidence}
}
```

**Phase 0 hook** in `execute_phases.go`, immediately before the `// Phase 1: Try rule engine` block:

```go
	// Phase 0: Deterministic multi-step WS case (no Steer LLM).
	if len(se.tc.Steps) > 0 {
		return se.runSteps()
	}
```

- [ ] **Step 1: Write failing tests** against an in-process WS server (reuse the `httptest`+`coder/websocket` harness shape already used in `websocket_test.go` — a server that accepts, validates `?token=` is optional for the test, sends a handshake `devices:sync`, replies `device:ack {approved:true}` to `device:command`). Three sub-tests via `t.Run`:

  1. **pass** — `Steps: [connect web, send {device:command}, receive device:ack assert payload.approved=true]` ⇒ `StepPassed`, evidence has 3 entries.
  2. **connect-fail short-circuits** — point `Target` at a closed port ⇒ `StepFailed`, only the connect step in evidence (send/receive never run).
  3. **assert-mismatch short-circuits** — server replies `device:ack {approved:false}` ⇒ `StepFailed` at the receive step (connect + send succeeded first).

  Build the `ReActLoop`/`stepExecution` the same way `executeStep` does (set `caseIDKey` on ctx, create a trace via a test store, or factor a tiny helper). If wiring a full `ReActLoop` is heavy in a unit test, add a thin constructor used only by tests that builds a `stepExecution` with a real `r.executor` (MultiExecutor with a WebSocketExecutor carrying the protocol index) + a test store.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/head/agent/ -run TestRunSteps -v`
Expected: FAIL — `runSteps` undefined.

- [ ] **Step 3: Implement `execute_phases_steps.go`** (code above) + add the Phase 0 hook.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/head/agent/ -run TestRunSteps -v -race`
Expected: PASS (all three sub-tests).

- [ ] **Step 5: Confirm no regression**

Run: `go test ./internal/head/agent/ -race`
Expected: PASS (existing WS/recovery tests green).

- [ ] **Step 6: Commit**

```bash
git add internal/head/agent/execute_phases_steps.go internal/head/agent/execute_phases_steps_test.go internal/head/agent/execute_phases.go
git commit -m "feat(ws): deterministic multi-step case execution (runSteps + Phase 0)"
```

---

### Task 3: Scout `WSCases` emits `Steps` for goal-derived exchanges

**Files:**
- Modify: `internal/head/scout/ws_cases.go`
- Modify: `internal/head/scout/ws_cases_test.go`

**Interfaces:**
- Consumes: `agent.TestStep`, `agent.TestCase.Steps`, `project.Protocol`/`ProtocolRole`/`RoleHandshake`, `wsTypesNamedInGoal`.
- Produces: `WSCases` now emits, per role, one `Steps` case when the goal pairs a client-sent type (send-verb) with a following receive type; otherwise keeps today's connect + receive-case form for connect-only scenarios.

**Behavior:** Reuse the existing direction heuristic inversely — a send-verb token (the types #2 excludes from receive) becomes a `ws_send` step's message type; the following receive token becomes the `ws_receive` step's `Type`. Example goal `"send device:command, verify device:ack approved=true"` with role `web` (handshake `devices:sync`) ⇒ one case:

```
Steps:
  - ws_connect   ConnectionID=c1  Role=web
  - ws_send      ConnectionID=c1  Message={"type":"device:command"}
  - ws_receive   ConnectionID=c1  Type=device:ack  Asserts={payload.approved:true}
```

- [ ] **Step 1: Write failing tests** (update existing tests that pin the old separate-case shape, add new ones):
  - `TestWSCasesEmitsStepsForExchange` — the device-ack goal ⇒ exactly one case with 3 Steps (connect/send/receive), `Action: "ws_flow"` (or similar), `Target = svc.URL`; no separate connect/receive cases.
  - `TestWSCasesConnectOnlyWhenNoExchange` — a goal with no send/receive pair ⇒ still today's connect + handshake-receive case(s).
  - Update `TestWSCasesIDFormat`, `TestWSCasesEmitsConnectAndDecisiveReceives`, `TestWSCasesTargetSetAndGoalTemplateBraces`, `TestWSCasesSendVerbTokenNotReceive` to the new output shape where they now collide (keep their intent: deterministic order, Target set, brace handling, send-verb exclusion). If a test's scenario now naturally becomes a Steps case, assert the Steps contents instead of separate cases.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/head/scout/ -run TestWSCases -v`
Expected: FAIL — old shape.

- [ ] **Step 3: Implement** — extend `WSCases`: detect a send/receive exchange from the goal (pair each send-verb token with the next receive token); when present, emit a single `Steps` case; otherwise keep the connect + receive-case path. Keep `wsDecisiveTypes`/dedup + sorted-role determinism. Derive `Asserts` from the goal where it states an expected value (e.g. `approved=true`); otherwise empty `Asserts` (arrival-only).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/head/scout/ -run TestWSCases -v -race`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/head/scout/ws_cases.go internal/head/scout/ws_cases_test.go
git commit -m "feat(scout): emit deterministic Steps case for WS send/receive exchanges"
```

---

### Task 4: Document `Steps` in websocket.md

**Files:**
- Modify: `cerberus-docs/executors/websocket.md` — extend the "Scout-generated cases (M3-2)" subsection (and add a short "Deterministic multi-step cases" note).

- [ ] **Step 1: Write the doc** — describe: a `Steps` case runs connect→send→receive→assert deterministically (no Steer); steps share one connection by `connection_id` within the case; Scout emits a Steps case when the goal pairs a client-sent type with a receive type; coexistence (connect-only and non-WS cases unchanged). Cross-link the design spec. Keep the existing comment density.

- [ ] **Step 2: Verify** `make check` (fmt+lint+test -race) green.

- [ ] **Step 3: Commit**

```bash
git add cerberus-docs/executors/websocket.md
git commit -m "docs(ws): document deterministic multi-step Steps cases"
```

---

## Self-Review (run after writing — done inline)

- **Spec coverage:** TestStep+Steps (T1), deterministic execution + sharing + short-circuit (T2), Scout generation (T3), docs (T4). Aggregated-result/Examiner evidence is `StepResult.Evidence` per step (T2). Open Q1 (hook) ⇒ Phase 0 in executeStep (T2). Open Q2 (WSCases migration) ⇒ connect-only preserved, Steps for exchanges (T3). Open Q3 (handshake) ⇒ stays read-on-connect via role (T3 connect step). Open Q4 (aggregated shape) ⇒ per-step Evidence + final Result (T2). All covered.
- **Placeholders:** none; code blocks are complete for the core logic. Test setup reuses the existing `websocket_test.go` in-process server shape (named, not "TODO").
- **Type consistency:** `TestStep` fields (T1) match `stepToAction` (T2) match WSCases output (T3). `runSteps` returns `StepResult` consumed by `executeStep`. `r.executor.Execute` / `r.recordEvidence` / `evidenceType` / `StepPassed|StepFailed` / `Evidence{Type,Content}` all exist in the agent package today.

## Execution Handoff

Plan saved to `cerberus-docs/superpowers/plans/2026-07-23-ws-deterministic-steps.md`. Per the autonomous run directive, execute via **superpowers:subagent-driven-development** (fresh implementer per task + task review), then opus whole-branch final review, then local ff-merge + `make check` + memory/ledger update.
