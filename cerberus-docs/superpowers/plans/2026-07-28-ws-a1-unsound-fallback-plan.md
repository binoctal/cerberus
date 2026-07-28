# A1 unsound-WS-flow fallback (Phase 1) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stop an LLM `ws_flow` with an invented (ungrounded) receive from suppressing a role's deterministic fallback, so a broken LLM case can no longer strand a covered role.

**Architecture:** Add pure grounding helpers to `ws_cases.go` (`wsTypeGrounded`, `llmWSFlowSound`), then gate the `covered`-marking in `assembly.go flush()` on `llmWSFlowSound`. The unsound LLM case stays in the plan; its roles simply are not marked covered, so `WSCasesCovered` (unchanged) still emits the deterministic fallback for them.

**Tech Stack:** Go 1.25, `internal/head/scout` (`ws_cases.go`, `assembly.go`), testify, `internal/llm` (`ToolCall`).

## Global Constraints

- Go 1.25, pure-Go (no CGo); module `github.com/binoctal/cerberus`
- Commit author MUST be `binoctal <binoctal@gmail.com>`, NO Co-Authored-By trailer
- Code comments + commit messages in English
- Docs only in `cerberus-docs/`
- `make check` (fmt + lint + test -race) must be EXIT 0
- Follow existing comment density/naming idiom
- Spec: `cerberus-docs/superpowers/specs/2026-07-28-ws-a1-unsound-fallback-design.md`

## File Structure

- `internal/head/scout/ws_cases.go` — add `wsTypeGrounded` + `llmWSFlowSound` immediately after `wsDecisiveTypes` (the grounding-heuristic neighbors: `wsTypesNamedInGoal`, `sanitizeTypeID` are reused).
- `internal/head/scout/ws_grounding_test.go` — NEW. Table-driven unit tests for the two helpers.
- `internal/head/scout/assembly.go` — `flush()` gates `covered`-marking on `llmWSFlowSound`; add a service→protocol index built once in `assemblePlan`.
- `internal/head/scout/ws_relay_test.go` — add the assembly-level + residual-risk tests (alongside the existing `assemblePlan`+`covered` tests).

---

### Task 1: Grounding helpers `wsTypeGrounded` + `llmWSFlowSound`

**Files:**
- Modify: `internal/head/scout/ws_cases.go` (insert after the `wsDecisiveTypes` function)
- Test (new): `internal/head/scout/ws_grounding_test.go`

**Interfaces:**
- Consumes: `sanitizeTypeID` (existing, `ws_cases.go`), `wsTypesNamedInGoal` (existing, `ws_cases.go`), `*project.Protocol` / `*project.ProtocolRole` / `*project.RoleHandshake` (`internal/project/protocol_schema.go`), `*agent.TestCase` / `agent.TestStep` (`internal/head/agent`).
- Produces:
  - `func wsTypeGrounded(typ string, aliases []string, proto *project.Protocol, goal string) bool`
  - `func llmWSFlowSound(tc *agent.TestCase, proto *project.Protocol, goal string) bool`

- [ ] **Step 1: Write the failing tests (RED)**

Create `internal/head/scout/ws_grounding_test.go`:

```go
package scout

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/project"
)

func TestWsTypeGrounded(t *testing.T) {
	// web awaits device:online; bridge awaits devices:sync.
	proto := &project.Protocol{Roles: map[string]*project.ProtocolRole{
		"web":    {Handshake: &project.RoleHandshake{AwaitType: "device:online"}},
		"bridge": {Handshake: &project.RoleHandshake{AwaitType: "devices:sync"}},
	}}
	// Goal names permission:response (a receive type, no send verb before it).
	goal := "verify permission:response approved=true"

	tests := []struct {
		name    string
		typ     string
		aliases []string
		want    bool
	}{
		{"handshake await_type grounded", "device:online", nil, true},
		{"handshake grounded via dash-colon sanitize", "devices-sync", nil, true}, // == devices:sync
		{"goal-named type grounded", "permission:response", nil, true},
		{"invented type ungrounded", "message", nil, false},
		{"empty type no aliases ungrounded", "", nil, false},
		{"ungrounded type rescued by grounded alias", "bogus", []string{"device:online"}, true},
		{"ungrounded type with ungrounded alias", "bogus", []string{"other"}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, wsTypeGrounded(tc.typ, tc.aliases, proto, goal))
		})
	}

	// nil proto: only goal-named types ground.
	assert.True(t, wsTypeGrounded("permission:response", nil, nil, goal),
		"goal type grounds without a proto")
	assert.False(t, wsTypeGrounded("device:online", nil, nil, goal),
		"handshake type does NOT ground without a proto")
}

func TestLLMWSFlowSound(t *testing.T) {
	proto := &project.Protocol{Roles: map[string]*project.ProtocolRole{
		"web": {Handshake: &project.RoleHandshake{AwaitType: "device:online"}},
	}}
	goal := "verify device:online"

	t.Run("connect-only is sound", func(t *testing.T) {
		tc := &agent.TestCase{Action: "ws_flow", Steps: []agent.TestStep{
			{Action: "ws_connect", Role: "web"},
		}}
		assert.True(t, llmWSFlowSound(tc, proto, goal))
	})

	t.Run("send-only is sound", func(t *testing.T) {
		tc := &agent.TestCase{Action: "ws_flow", Steps: []agent.TestStep{
			{Action: "ws_connect", Role: "web"},
			{Action: "ws_send", Message: `{"type":"device:command"}`},
		}}
		assert.True(t, llmWSFlowSound(tc, proto, goal))
	})

	t.Run("grounded receive is sound", func(t *testing.T) {
		tc := &agent.TestCase{Action: "ws_flow", Steps: []agent.TestStep{
			{Action: "ws_connect", Role: "web"},
			{Action: "ws_receive", Type: "device:online"},
		}}
		assert.True(t, llmWSFlowSound(tc, proto, goal))
	})

	t.Run("invented receive is unsound", func(t *testing.T) {
		tc := &agent.TestCase{Action: "ws_flow", Steps: []agent.TestStep{
			{Action: "ws_connect", Role: "web"},
			{Action: "ws_receive", Type: "message"},
		}}
		assert.False(t, llmWSFlowSound(tc, proto, goal))
	})

	t.Run("mixed grounded and invented is unsound", func(t *testing.T) {
		tc := &agent.TestCase{Action: "ws_flow", Steps: []agent.TestStep{
			{Action: "ws_receive", Type: "device:online"},
			{Action: "ws_receive", Type: "message"},
		}}
		assert.False(t, llmWSFlowSound(tc, proto, goal))
	})

	t.Run("ungrounded type grounded alias is sound", func(t *testing.T) {
		tc := &agent.TestCase{Action: "ws_flow", Steps: []agent.TestStep{
			{Action: "ws_receive", Type: "bogus", Aliases: []string{"device:online"}},
		}}
		assert.True(t, llmWSFlowSound(tc, proto, goal))
	})
}
```

- [ ] **Step 2: Run tests to verify RED**

Run: `go test ./internal/head/scout/ -run 'TestWsTypeGrounded|TestLLMWSFlowSound' -v`
Expected: COMPILE ERROR — `undefined: wsTypeGrounded` / `llmWSFlowSound`.

- [ ] **Step 3: Add the helpers**

Insert immediately after the `wsDecisiveTypes` function in `internal/head/scout/ws_cases.go`:

```go
// wsTypeGrounded reports whether a ws_receive type (or any of its aliases) is
// grounded — i.e. the server is known to send it. A type is grounded when it
// equals (by sanitizeTypeID, so "devices:sync" and "devices-sync" match) any
// role's handshake await_type in proto OR a type named in the goal. The goal is
// receive-directional (wsTypesNamedInGoal already excludes send-verb types).
// Aliases are matched because the executor matches a frame whose type_path is
// Type OR any Aliases (websocket.go want = Type + Aliases).
func wsTypeGrounded(typ string, aliases []string, proto *project.Protocol, goal string) bool {
	grounded := wsGroundedTypeSet(proto, goal)
	if grounded[sanitizeTypeID(typ)] {
		return true
	}
	for _, a := range aliases {
		if grounded[sanitizeTypeID(a)] {
			return true
		}
	}
	return false
}

// wsGroundedTypeSet returns the sanitizeTypeID-normalized set of receive types
// the server is known to send: every role's handshake await_type in proto plus
// the goal-named types. Used to judge whether an LLM-emitted ws_receive can
// plausibly match a real frame.
func wsGroundedTypeSet(proto *project.Protocol, goal string) map[string]bool {
	out := map[string]bool{}
	if proto != nil {
		for _, r := range proto.Roles {
			if r != nil && r.Handshake != nil && r.Handshake.AwaitType != "" {
				out[sanitizeTypeID(r.Handshake.AwaitType)] = true
			}
		}
	}
	for _, t := range wsTypesNamedInGoal(goal) {
		out[sanitizeTypeID(t)] = true
	}
	return out
}

// llmWSFlowSound reports whether an LLM-authored ws_flow case is structurally
// sound: every ws_receive step has a grounded type or alias. A case with no
// ws_receive (connect-only, send-only) is trivially sound. An unsound case (a
// receive of an invented type the server never sends) must not by itself mark a
// role covered, or the role is stranded when the receive times out. Asserts are
// intentionally not considered — malformed asserts are tolerated at execution by
// the D4 defense (commit cf638a0).
func llmWSFlowSound(tc *agent.TestCase, proto *project.Protocol, goal string) bool {
	for _, s := range tc.Steps {
		if s.Action == "ws_receive" && !wsTypeGrounded(s.Type, s.Aliases, proto, goal) {
			return false
		}
	}
	return true
}
```

- [ ] **Step 4: Run the targeted tests to verify GREEN**

Run: `go test ./internal/head/scout/ -run 'TestWsTypeGrounded|TestLLMWSFlowSound' -v`
Expected: PASS (all subtests).

- [ ] **Step 5: Regression — full scout package**

Run: `go test ./internal/head/scout/ -count=1`
Expected: PASS. The helpers are not yet wired into assembly, so all existing behavior is unchanged.

- [ ] **Step 6: Commit**

```bash
git add internal/head/scout/ws_cases.go internal/head/scout/ws_grounding_test.go
git -c user.name='binoctal' -c user.email='binoctal@gmail.com' commit -m "feat(scout): ws_flow soundness + receive-type grounding helpers

wsTypeGrounded reports whether a ws_receive type/alias is grounded (a declared
handshake await_type or a goal-named type, compared by sanitizeTypeID).
llmWSFlowSound reports whether an LLM ws_flow case is structurally sound (every
ws_receive grounded). Pure helpers; not yet wired. Grounds the A1 unsound-WS-flow
fallback (Phase 1)."
```

---

### Task 2: Gate `covered` on case soundness in `assembly.go`

**Files:**
- Modify: `internal/head/scout/assembly.go` — build a service→protocol index in `assemblePlan`; gate the `covered`-marking block in `flush()` on `llmWSFlowSound`.
- Test: `internal/head/scout/ws_relay_test.go` — add assembly-level + residual-risk tests.

**Interfaces:**
- Consumes (from Task 1): `llmWSFlowSound(tc *agent.TestCase, proto *project.Protocol, goal string) bool`.
- Produces: tightened `covered` semantics — `covered[svc][role]` is true only when a SOUND LLM ws_flow covers the role. `WSCasesCovered` and the `covered` map type are unchanged.

- [ ] **Step 1: Write the failing tests (RED)**

Append to `internal/head/scout/ws_relay_test.go` (the file already imports `testing`, `require`, `llm`, `project`; add `"github.com/stretchr/testify/assert"` and `"strings"` to the import block):

```go
// TestAssemblePlan_UnsoundWSFlowDoesNotCover is the A1 residual-risk fix: an
// LLM ws_flow that connects a role but receives an INVENTED (ungrounded) type is
// unsound, so the role is NOT marked covered — WSCasesCovered still emits the
// deterministic fallback for it. A sound case (grounded receive) still covers.
func TestAssemblePlan_UnsoundWSFlowDoesNotCover(t *testing.T) {
	cfg := &project.Config{Services: []project.Service{{Name: "rt", URL: "ws://h/ws", Protocol: relayProtocol()}}}

	sound := []llm.ToolCall{
		{Name: "begin_case", Input: map[string]any{"name": "relay", "expectation": "ok", "service": "rt"}},
		{Name: "ws_connect", Input: map[string]any{"role": "web"}},
		{Name: "ws_receive", Input: map[string]any{"role": "web", "type": "device:online"}}, // grounded (web await_type)
	}
	_, coveredSound := assemblePlan(sound, "goal", "ws://h/ws", cfg.Services)
	assert.True(t, coveredSound["rt"]["web"], "grounded receive -> web covered")

	unsound := []llm.ToolCall{
		{Name: "begin_case", Input: map[string]any{"name": "relay", "expectation": "ok", "service": "rt"}},
		{Name: "ws_connect", Input: map[string]any{"role": "web"}},
		{Name: "ws_receive", Input: map[string]any{"role": "web", "type": "message"}}, // invented
	}
	planUnsound, coveredUnsound := assemblePlan(unsound, "goal", "ws://h/ws", cfg.Services)
	assert.False(t, coveredUnsound["rt"]["web"], "invented receive -> web NOT covered (unsound)")
	// Policy: the unsound LLM case itself stays in the plan.
	assert.Len(t, planUnsound.Cases, 1, "unsound LLM case is kept, not dropped")

	// Residual-risk proof: unsound coverage keeps web's deterministic fallback.
	// (web has an optional handshake in relayProtocol, so the fallback is the
	// deterministic relay case, which connects web.)
	connectsWeb := func(covered map[string]map[string]bool) bool {
		for _, c := range WSCasesCovered(cfg, "receive devices:sync", covered) {
			for _, st := range c.Steps {
				if st.Action == "ws_connect" && st.Role == "web" {
					return true
				}
			}
		}
		return false
	}
	assert.False(t, connectsWeb(coveredSound), "sound coverage suppresses web fallback")
	assert.True(t, connectsWeb(coveredUnsound), "unsound coverage keeps web fallback (not stranded)")
}
```

- [ ] **Step 2: Run tests to verify RED**

Run: `go test ./internal/head/scout/ -run 'TestAssemblePlan_UnsoundWSFlowDoesNotCover' -v`
Expected: FAIL — `coveredUnsound["rt"]["web"]` is `true` (today every ws_connect-with-Role marks covered regardless of soundness), so the `assert.False` fails; and `connectsWeb(coveredUnsound)` is `false` (web suppressed), so the final `assert.True` fails.

- [ ] **Step 3: Build the service→protocol index in `assemblePlan`**

In `internal/head/scout/assembly.go`, inside `assemblePlan`, immediately after the line `covered := map[string]map[string]bool{}` (the first line of the function body), add:

```go
	// service -> declared protocol, so flush can judge ws_flow soundness without
	// re-scanning services per case.
	svcProtos := map[string]*project.Protocol{}
	for _, s := range services {
		if s.Protocol != nil {
			svcProtos[s.Name] = s.Protocol
		}
	}
```

- [ ] **Step 4: Gate the `covered`-marking in `flush()` on soundness**

In `internal/head/scout/assembly.go`, the `flush` closure currently marks every `ws_connect`-with-`Role` as covered unconditionally:

```go
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
```

Replace it with a soundness-gated version:

```go
			if open.Service != "" {
				// A1 unsound-fallback (Phase 1): only a SOUND ws_flow suppresses
				// the deterministic fallback for the roles it connects. An unsound
				// case (a ws_receive of an invented type) stays in the plan but
				// does not mark its roles covered, so WSCasesCovered still emits
				// the deterministic fallback for them.
				if llmWSFlowSound(open, svcProtos[open.Service], goal) {
					for _, st := range open.Steps {
						if st.Action == "ws_connect" && st.Role != "" {
							if covered[open.Service] == nil {
								covered[open.Service] = map[string]bool{}
							}
							covered[open.Service][st.Role] = true
						}
					}
				}
			}
```

`goal` and `svcProtos` are captured by the closure (both are in `assemblePlan`'s scope).

- [ ] **Step 5: Run the targeted test to verify GREEN**

Run: `go test ./internal/head/scout/ -run 'TestAssemblePlan_UnsoundWSFlowDoesNotCover' -v`
Expected: PASS.

- [ ] **Step 6: Regression — full scout package**

Run: `go test ./internal/head/scout/ -count=1`
Expected: PASS. In particular:
- `TestAugmentPlanComposition_AssembledRelay` still passes — its relay receive `device:online` equals web's declared `AwaitType`, so the case is sound and `covered["rt"]["web"]`/`["bridge"]` remain true.
- `TestWSCasesCovered_*` tests that hand-build `covered` are unaffected (the `covered` contract is unchanged).

If any existing assembly/relay test flips, confirm its LLM case receive is actually grounded; if a test deliberately used an ungrounded receive to exercise coverage, update that test to a grounded receive (the contract is now "sound coverage").

- [ ] **Step 7: make check**

Run: `make check`
Expected: EXIT 0 (fmt + lint + test -race).

- [ ] **Step 8: Commit + push**

```bash
git add internal/head/scout/assembly.go internal/head/scout/ws_relay_test.go
git -c user.name='binoctal' -c user.email='binoctal@gmail.com' commit -m "fix(scout): gate WS covered on case soundness (A1 unsound fallback)

assemblePlan marks a role covered only when a SOUND LLM ws_flow covers it: a
ws_flow is sound iff every ws_receive type/alias is grounded (a declared
handshake await_type or a goal-named type). An unsound case (an invented receive
that the server never sends) stays in the plan but no longer suppresses the
deterministic fallback for its roles, so a broken LLM case can no longer strand
a covered role. WSCasesCovered and the covered map contract are unchanged.

Spec: cerberus-docs/superpowers/specs/2026-07-28-ws-a1-unsound-fallback-design.md"
git push origin main
```

---

## Self-Review (completed)

- **Spec coverage:** Soundness rule (§Design) → Task 1 helpers + Task 2 gate. "Keep the LLM case" (policy a) → Task 2 Step 4 appends `*open` unconditionally + Task 2 test asserts `Len(planUnsound.Cases)==1`. "WSCasesCovered unchanged" → no task touches it. Phase 2 items explicitly out of scope. ✓
- **Placeholder scan:** No TBD/TODO; every code step has full code. ✓
- **Type consistency:** `wsTypeGrounded(typ string, aliases []string, proto *project.Protocol, goal string) bool` and `llmWSFlowSound(tc *agent.TestCase, proto *project.Protocol, goal string) bool` — signatures match between Task 1 (definition), Task 1 tests, and Task 2 (call `llmWSFlowSound(open, svcProtos[open.Service], goal)`). ✓
