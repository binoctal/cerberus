# Relay Coverage Generator Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a deterministic generator that emits one 4-step relay case per declared `message_handled` vocab edge, so receive-driven path coverage can credit the ~31 edges the existing generators never enumerate.

**Architecture:** A new pure function `wsRelayCoverageCases(svc project.Service)` in `internal/head/scout/ws_cases.go` iterates the `requiredEdges` message_handled set (`Trigger=="message_handled" && !Unsupported && !Partial && FromRole!=ToRole`), dedups by `(From,To,Type)`, and emits a `ws_connect(From) → ws_connect(To) → ws_send(From, T) → ws_receive(To, T)` case per edge. This plan ships the generator + scout-package unit tests only. Wiring it into `WSCases` is gated on a later phase (it must run only after server-only types are marked, else their timeout-fails trigger `systemic_failure`/`target_unreachable` escalation in the autonomous executor). See the spec's 1a→2→1b ordering.

**Tech Stack:** Go 1.25, module `github.com/binoctal/cerberus`. Pure function, no LLM, no live server needed for unit tests.

**Spec:** `cerberus-docs/superpowers/specs/2026-08-13-relay-coverage-generator-design.md`

## Global Constraints

- Commit author: `binoctal <binoctal@gmail.com>`, no Co-Authored-By.
- No CGo. Comments and commit messages in English.
- Documentation only in `cerberus-docs/`, never `docs/`.
- Zero regression: `go test ./...` stays green; existing `ws_*Cases` generators and `TestPathCoverage_*` unchanged.
- Pure, deterministic, no LLM: the generator is unit-testable without a live server, exactly like the other `ws*Cases` generators.

---

### Task 1: `wsRelayCoverageCases` generator + unit tests (TDD)

**Files:**
- Modify: `internal/head/scout/ws_cases.go` — add `wsRelayCoverageCases` after `wsHTTPTriggerCases` (≈ line 700).
- Test: `internal/head/scout/ws_cases_test.go` (create if absent; else append).

**Interfaces:**
- Consumes: `project.Service` (`.Vocabulary.Edges`, `.Protocol.Roles`), `project.VocabEdge` (`FromRole`, `ToRole`, `Type`, `Trigger`, `Unsupported`, `Partial` fields), existing helpers `wsSendBody(typ string, payload map[string]string) string`, `wsCaseID(service, role, typ string) string`, `sanitizeTypeID(typ string) string`, and `agent.TestCase`/`agent.TestStep` from `internal/head/agent`.
- Produces: `func wsRelayCoverageCases(svc project.Service) []agent.TestCase` — unexported, same package as the other generators; returns one 4-step `ws_flow` case per qualifying edge.

- [ ] **Step 1: Write the failing tests**

Append to `internal/head/scout/ws_cases_test.go` (package `scout`). The test builds a synthetic service whose vocabulary has six message_handled edges exercising every filter branch, plus one non-message_handled edge and one Partial edge that must be excluded.

```go
package scout

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/project"
)

// msgEdge is a compact constructor for a message_handled vocab edge.
func msgEdge(from, to, typ string) project.VocabEdge {
	return project.VocabEdge{FromRole: from, ToRole: to, Type: typ, Trigger: "message_handled"}
}

func TestWSRelayCoverageCases_EmitsOneCasePerQualifyingEdge(t *testing.T) {
	svc := project.Service{
		Name: "realtime",
		Vocabulary: &project.Vocabulary{Edges: []project.VocabEdge{
			msgEdge("bridge", "web", "device:online"),        // qualifying
			msgEdge("web", "bridge", "session:send"),         // qualifying (reverse direction)
			msgEdge("bridge", "web", "device:online"),        // duplicate (From,To,Type) — collapse
			msgEdge("bridge", "web", "workflow:start"),       // qualifying
			msgEdge("web", "web", "self:loop"),               // self-relay — skip
			msgEdge("bridge", "web", "device:offline"),       // qualifying
			{FromRole: "bridge", ToRole: "web", Type: "device:restart", Trigger: "fetch_branch"}, // non-message_handled — skip
			{FromRole: "bridge", ToRole: "web", Type: "encrypted", Trigger: "message_handled", Partial: true}, // Partial — skip
		}},
		Protocol: &project.Protocol{Roles: map[string]*project.ProtocolRole{
			"web":    {},
			"bridge": {},
		}},
	}

	got := wsRelayCoverageCases(svc)

	// 4 unique qualifying edges: device:online (deduped), session:send, workflow:start, device:offline.
	require.Len(t, got, 4, "one case per unique qualifying message_handled edge, deduped by (From,To,Type)")

	byKey := map[string]agent.TestCase{}
	for _, c := range got {
		byKey[c.ID] = c
	}

	// Each case is a 4-step ws_flow: connect From, connect To, send T from From, receive T on To.
	for _, want := range []struct {
		from, to, typ string
	}{
		{"bridge", "web", "device:online"},
		{"web", "bridge", "session:send"},
		{"bridge", "web", "workflow:start"},
		{"bridge", "web", "device:offline"},
	} {
		id := wsCaseID("realtime", want.to+"-recv", want.typ)
		c, ok := byKey[id]
		require.Truef(t, ok, "missing case for %s→%s %s (id=%s)", want.from, want.to, want.typ, id)
		require.Lenf(t, c.Steps, 4, "case %s must have 4 steps", id)
		assert.Equal(t, "ws_connect", c.Steps[0].Action, "step 0 connects From")
		assert.Equal(t, want.from, c.Steps[0].Role, "step 0 role is From")
		assert.Equal(t, want.from, c.Steps[0].ConnectionID, "step 0 conn is From")
		assert.Equal(t, "ws_connect", c.Steps[1].Action, "step 1 connects To")
		assert.Equal(t, want.to, c.Steps[1].Role, "step 1 role is To")
		assert.Equal(t, want.to, c.Steps[1].ConnectionID, "step 1 conn is To")
		assert.Equal(t, "ws_send", c.Steps[2].Action, "step 2 sends T from From")
		assert.Equal(t, want.from, c.Steps[2].ConnectionID, "step 2 send on From conn")
		assert.Equal(t, "ws_receive", c.Steps[3].Action, "step 3 receives T on To")
		assert.Equal(t, want.to, c.Steps[3].ConnectionID, "step 3 receive on To conn")
		assert.Equal(t, want.typ, c.Steps[3].Type, "step 3 receives type T")
		assert.Equal(t, "ws_flow", c.Action, "case action is ws_flow")
		assert.Equal(t, "realtime", c.Service, "case carries service name")
	}
}

func TestWSRelayCoverageCases_EmptyWhenNoVocabulary(t *testing.T) {
	assert.Empty(t, wsRelayCoverageCases(project.Service{Name: "svc"}), "no vocabulary ⇒ no cases")
	assert.Empty(t, wsRelayCoverageCases(project.Service{
		Name:       "svc",
		Vocabulary: &project.Vocabulary{}, // no edges
	}), "empty vocabulary ⇒ no cases")
}

func TestWSRelayCoverageCases_PayloadFromRecipientRequestPayload(t *testing.T) {
	// The send payload uses the RECIPIENT role's RequestPayload[T], matching
	// wsRequestResponseCases (the receiver declares the payload it expects).
	svc := project.Service{
		Name: "realtime",
		Vocabulary: &project.Vocabulary{Edges: []project.VocabEdge{
			msgEdge("bridge", "web", "session:send"),
		}},
		Protocol: &project.Protocol{Roles: map[string]*project.ProtocolRole{
			"web": {RequestPayload: map[string]map[string]string{
				"session:send": {"content": "hello"},
			}},
			"bridge": {},
		}},
	}

	got := wsRelayCoverageCases(svc)
	require.Len(t, got, 1)
	// wsSendBody wraps {"type": T, "payload": {...}}; assert the payload field is present.
	assert.Contains(t, got[0].Steps[2].Message, `"content":"hello"`,
		"send body must carry the recipient's RequestPayload for T")
	assert.Contains(t, got[0].Steps[2].Message, `"type":"session:send"`)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/head/scout/ -run TestWSRelayCoverageCases -v`
Expected: FAIL / build error — `undefined: wsRelayCoverageCases`.

- [ ] **Step 3: Implement `wsRelayCoverageCases`**

Append after `wsHTTPTriggerCases` in `internal/head/scout/ws_cases.go`:

```go
// wsRelayCoverageCases emits one deterministic 4-step relay case per declared
// message_handled vocab edge that the other generators do not already cover:
// From connect → To connect → From send T → To receive T. The final ws_receive
// is what receive-driven path coverage credits (exercisedEdges keys by
// (ToRole, Type) from a matched receive in Evidence). This gives the ~31
// declared message_handled edges that wsRelayCases/wsRequestResponseCases/
// wsFlowConnectCase never enumerate a case that can credit them.
//
// Edge set mirrors requiredEdges exactly (Trigger=message_handled, not
// Unsupported, not Partial); FromRole==ToRole (self-relay) is skipped.
// Duplicate (From,To,Type) edges collapse to one case. The send payload uses
// the recipient role's RequestPayload[T] when declared (matching
// wsRequestResponseCases), else an empty payload. Pure; no LLM.
//
// NOTE: wiring into wsCasesForService is a LATER phase — these cases must not
// reach the autonomous executor until server-only types are marked Partial,
// because their timeout-fails would trigger systemic_failure/target_unreachable
// escalation. See the design spec's 1a→2→1b ordering.
func wsRelayCoverageCases(svc project.Service) []agent.TestCase {
	if svc.Vocabulary == nil {
		return nil
	}
	seen := make(map[string]bool)
	var cases []agent.TestCase
	for _, e := range svc.Vocabulary.Edges {
		if e.Trigger != "message_handled" || e.Unsupported || e.Partial {
			continue
		}
		if e.FromRole == "" || e.ToRole == "" || e.FromRole == e.ToRole {
			continue
		}
		key := e.FromRole + "|" + e.ToRole + "|" + e.Type
		if seen[key] {
			continue
		}
		seen[key] = true

		var payload map[string]string
		if svc.Protocol != nil {
			if role := svc.Protocol.Roles[e.ToRole]; role != nil {
				payload = role.RequestPayload[e.Type]
			}
		}
		cases = append(cases, agent.TestCase{
			ID:      wsCaseID(svc.Name, e.ToRole+"-recv", e.Type),
			Name:    fmt.Sprintf("%s %s relays %s to %s", svc.Name, e.FromRole, e.Type, e.ToRole),
			Service: svc.Name,
			Target:  svc.URL,
			Action:  "ws_flow",
			Expectation: fmt.Sprintf("%s: %s sends %s, %s receives it",
				svc.Name, e.FromRole, e.Type, e.ToRole),
			Priority: 0.6,
			Steps: []agent.TestStep{
				{Action: "ws_connect", ConnectionID: e.FromRole, Role: e.FromRole},
				{Action: "ws_connect", ConnectionID: e.ToRole, Role: e.ToRole},
				{Action: "ws_send", ConnectionID: e.FromRole, Message: wsSendBody(e.Type, payload)},
				{Action: "ws_receive", ConnectionID: e.ToRole, Type: e.Type, Timeout: 3},
			},
		})
	}
	return cases
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/head/scout/ -run TestWSRelayCoverageCases -v`
Expected: PASS (all three tests).

- [ ] **Step 5: Run full scout package + project suite for regression**

Run: `go test ./internal/head/scout/ ./internal/session/ ./internal/head/agent/`
Expected: all PASS — the new function is unreferenced elsewhere, so nothing else changes.

- [ ] **Step 6: Commit**

```bash
git add internal/head/scout/ws_cases.go internal/head/scout/ws_cases_test.go
git commit -m "feat(scout): add wsRelayCoverageCases generator for message_handled edges"
```

---

## Subsequent phases (out of this plan's scope — listed for handoff)

This plan ships the generator as an unexported, unit-tested pure function. The
spec's remaining phases depend on data this generator produces at run time, so
each gets its own plan once the prior phase's output is in hand:

1. **Live probe (spec Phase 1a).** Run the generator's cases against a live
   open-agents server to classify each type as client-triggerable (pass) vs
   server-only (timeout). Implementation note for that plan: the live
   step-execution test helpers (`setupOpenAgents`, `newStepExecutionWithIdx`)
   live in `internal/head/agent`'s test package and are unexported, while the
   generator lives in `internal/head/scout`; `scout` already imports `agent`, so
   an `agent`-package test cannot import `scout` (cycle). The probe plan must
   either (a) inline the 4-step case construction inside an `agent`-package
   integration test (mirroring `wsRelayCoverageCases`, whose correctness is
   locked by Task 1's unit tests), or (b) move/duplicate the case construction
   into an exported agent helper. Note the integration path runs each case via
   `newStepExecutionWithIdx`, which does NOT go through the `ExecutePlan` loop,
   so `systemic_failure`/`target_unreachable` escalation does NOT fire — the
   probe is safe to run all ~60 cases.

2. **Denominator honesty (spec Phase 2).** From the probe's stable timeout set,
   mark those vocab edges `Partial` (or a new flag) so `requiredEdges` and
   `wsRelayCoverageCases`'s filter exclude them. Data-gated on phase 1.

3. **Wire into `WSCases` (spec Phase 1b).** Call `wsRelayCoverageCases` from
   `wsCasesForService` with coexistence dedup against `wsRelayCases`/
   `wsRequestResponseCases`/`wsHTTPTriggerCases` (skip edges whose roles are
   already connected; record connected roles so the per-role
   `wsFlowConnectCase` loop skips them — same pattern as `rrConnected`). Safe
   only after phase 2 has excluded server-only types.
