# WebSocket Realtime Engine (M3-2) — Scout-Generated WS Cases Implementation Plan

> **STATUS: ACTIVE — greenlit by the 2026-07-21 WS Tier-1 dogfood.** The dogfood's Finding 3 is the trigger the M3 proposal required: against the same `/realtime` target in one session, the Steer LLM used `ws_*` for tc-001 but `api_request` (HTTP 426 death-loop) for tc-002/tc-004, and mis-sequenced the one WS case it started (connect×2, no send, receive instant-fail). Scout today emits no WS cases, so every run re-derives WS orchestration at runtime. The LLM-prompt content (Task 3) remains provisional — tune against a real target. Tasks 1–2 (the deterministic generator + wiring) are fully specified and testable without an LLM. (Skeleton scope, D2: this reduces but does not remove Steer-LLM orchestration — full determinism via `Steps`+`matchWSRules` remains deferred.)

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Scout generates WS test cases (one connect-setup + decisive-receive per role/verification-point) from declared `protocol:` blocks, appended to the plan; the agent Steer LLM orchestrates execution (skeleton + fill).

**Architecture:** A new deterministic `WSCases(cfg, goal)` reads `cfg.Services[].Protocol` (roles + handshake await_types + goal-named types) and emits `agent.TestCase` rows (connect setup + decisive receives linked by `DependsOn`). Wired into `appendExecutorCases` alongside the language-based cases. No `TestCase` struct change, no rule-engine/executor change (reuses M0–M2 Steer path). Planning-prompt gains a conditional WS-awareness sentence (provisional).

**Tech Stack:** Go 1.25 · stdlib · `internal/project` (Protocol/roles types already exist).

## Global Constraints

- Go 1.25, module `github.com/binoctal/cerberus`, pure Go. No new deps.
- Commit author `binoctal <binoctal@gmail.com>`, no Co-Authored-By. English comments/messages.
- Docs only in `cerberus-docs/`. `make check` (fmt+lint+test -race) green.
- `agent.TestCase` struct must NOT change (skeleton scope). `promptPlanSystem`/`promptPlanSystemLocal` are raw-string literals — inline edits only, no backticks.

**Spec:** `cerberus-docs/superpowers/specs/2026-07-21-ws-scout-cases-design.md`

---

## Task 1: WSCases generator + unit tests

**Files:**
- Create: `internal/head/scout/ws_cases.go`
- Test: `internal/head/scout/ws_cases_test.go`

**Interfaces:**
- Produces: `func WSCases(cfg *project.Config, goal string) []agent.TestCase`; package-private helpers `wsDecisiveTypes(role *project.ProtocolRole, goal string) []string`, `wsCaseID(...)`, `sanitizeTypeID(string) string`.

- [ ] **Step 1: Write the failing tests** (`internal/head/scout/ws_cases_test.go`):

```go
package scout

import (
	"encoding/json"
	"testing"

	"github.com/binoctal/cerberus/internal/project"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWSCasesNoneWhenNoProtocols(t *testing.T) {
	cfg := &project.Config{Services: []project.Service{{Name: "api", URL: "http://x"}}}
	assert.Nil(t, WSCases(cfg, "test it"))
}

func TestWSCasesEmitsConnectAndDecisiveReceives(t *testing.T) {
	cfg := &project.Config{Services: []project.Service{{
		Name: "rt", URL: "http://x",
		Protocol: &project.Protocol{
			TypePath: "type",
			Roles: map[string]*project.ProtocolRole{
				"bridge": {
					Params:    map[string]string{"type": "bridge"},
					Handshake: &project.RoleHandshake{AwaitType: "devices:sync", Timeout: 5},
				},
			},
		},
	}}}
	cases := WSCases(cfg, "bridge receives permission:response with approved=true")
	// One connect setup + one handshake-await decisive receive + one goal-named receive.
	connects := filterAction(cases, "ws_connect")
	require.Len(t, connects, 1)
	assert.Equal(t, "rt", connects[0].Service)
	assert.True(t, connects[0].Background)
	assertBodyRole(t, connects[0].Body, "bridge")

	receives := filterAction(cases, "ws_receive")
	// handshake await_type "devices:sync" + goal-named "permission:response"
	types := bodyTypes(receives)
	assert.ElementsMatch(t, []string{"devices:sync", "permission:response"}, types)
	// Every receive depends on the connect.
	for _, r := range receives {
		assert.Contains(t, []string(r.DependsOn), connects[0].ID)
	}
}

func TestWSCasesNoGoalMatchJustHandshake(t *testing.T) {
	cfg := &project.Config{Services: []project.Service{{
		Name: "rt", URL: "http://x",
		Protocol: &project.Protocol{Roles: map[string]*project.ProtocolRole{
			"web": {Handshake: &project.RoleHandshake{AwaitType: "ready", Timeout: 5}},
		}},
	}}}
	cases := WSCases(cfg, "unrelated goal")
	receives := filterAction(cases, "ws_receive")
	assert.Equal(t, []string{"ready"}, bodyTypes(receives))
}

func filterAction(cs []agent.TestCase, action string) []agent.TestCase {
	var out []agent.TestCase
	for _, c := range cs {
		if c.Action == action {
			out = append(out, c)
		}
	}
	return out
}

func bodyTypes(cs []agent.TestCase) []string {
	var out []string
	for _, c := range cs {
		var b map[string]string
		if json.Unmarshal([]byte(c.Body), &b) == nil {
			out = append(out, b["type"])
		}
	}
	return out
}

func assertBodyRole(t *testing.T, body, want string) {
	t.Helper()
	var b map[string]string
	require.NoError(t, json.Unmarshal([]byte(body), &b))
	assert.Equal(t, want, b["role"])
}
```

(`agent` is the sibling package `internal/head/agent`; `ws_cases_test.go` is `package scout` so it uses `agent.TestCase` qualified — add the import.)

- [ ] **Step 2: Run to verify fail** — `go test -run TestWSCases -v ./internal/head/scout/` → FAIL (WSCases undefined).

- [ ] **Step 3: Implement** (`internal/head/scout/ws_cases.go`):

```go
package scout

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/project"
)

// WSCases generates deterministic WS test cases from a project's declared
// protocols: for each role on each WS service, one ws_connect setup case plus
// one decisive ws_receive case per verification-point type (the role's
// handshake await_type, plus any routing type named in the goal). Returns nil
// when no service declares a protocol. The agent Steer LLM orchestrates the
// actual connect/send/receive; these cases seed the plan with WS intent.
func WSCases(cfg *project.Config, goal string) []agent.TestCase {
	if cfg == nil {
		return nil
	}
	var cases []agent.TestCase
	for _, svc := range cfg.Services {
		if svc.Protocol == nil || len(svc.Protocol.Roles) == 0 {
			continue
		}
		for roleName, role := range svc.Protocol.Roles {
			connectID := wsCaseID(svc.Name, roleName, "connect")
			cases = append(cases, agent.TestCase{
				ID:          connectID,
				Name:        fmt.Sprintf("%s %s connects", svc.Name, roleName),
				Service:     svc.Name,
				Action:      "ws_connect",
				Background:  true,
				Body:        wsBody(roleName, ""),
				Expectation: fmt.Sprintf("%s role %s establishes the connection", svc.Name, roleName),
				Priority:    0.5,
			})
			for _, typ := range wsDecisiveTypes(role, goal) {
				cases = append(cases, agent.TestCase{
					ID:          wsCaseID(svc.Name, roleName, typ),
					Name:        fmt.Sprintf("%s %s receives %s", svc.Name, roleName, typ),
					Service:     svc.Name,
					Action:      "ws_receive",
					Body:        wsBody(roleName, typ),
					Expectation: fmt.Sprintf("%s role %s receives a %s message", svc.Name, roleName, typ),
					DependsOn:   agent.Deps{connectID},
					Priority:    0.8,
				})
			}
		}
	}
	return cases
}

// wsDecisiveTypes returns the routing types to assert on for a role: the
// handshake await_type (if any) plus any type literally named in the goal that
// is not already included. Deterministic; no LLM.
func wsDecisiveTypes(role *project.ProtocolRole, goal string) []string {
	var types []string
	if role != nil && role.Handshake != nil && role.Handshake.AwaitType != "" {
		types = append(types, role.Handshake.AwaitType)
	}
	for _, t := range wsTypesNamedInGoal(goal) {
		if !contains(types, t) {
			types = append(types, t)
		}
	}
	return types
}

// wsTypesNamedInGoal finds candidate routing-type tokens in the goal text. A
// simple heuristic: colon-bearing tokens (e.g. "permission:response") are
// common WS routing keys. Provisional — tune via dogfooding.
func wsTypesNamedInGoal(goal string) []string {
	var out []string
	for _, field := range strings.Fields(goal) {
		f := strings.Trim(field, ".,;:\"'()")
		if strings.Contains(f, ":") && !contains(out, f) {
			out = append(out, f)
		}
	}
	return out
}

func wsBody(role, typ string) string {
	m := map[string]string{"role": role}
	if typ != "" {
		m["type"] = typ
	}
	b, _ := json.Marshal(m)
	return string(b)
}

func wsCaseID(service, role, typ string) string {
	return "ws-" + service + "-" + role + "-" + sanitizeTypeID(typ)
}

// sanitizeTypeID turns a routing type into an ID-safe token.
func sanitizeTypeID(typ string) string {
	r := strings.NewReplacer(":", "-", "/", "-", " ", "-")
	return r.Replace(typ)
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run to verify pass** — `go test -run TestWSCases -v ./internal/head/scout/` → PASS.

- [ ] **Step 5: Commit** — `git commit -m "feat(scout): generate WS cases from declared protocol roles"`.

---

## Task 2: Wire WSCases into appendExecutorCases + regression

**Files:**
- Modify: `internal/head/scout/plan_phases.go` (`appendExecutorCases`, ~line 86)
- Test: `internal/head/scout/plan_phases_test.go` (or existing executor-case test)

**Interfaces:** Consumes `WSCases` (Task 1).

- [ ] **Step 1: Write the failing test** — a test asserting that a `*agent.TestPlan` built via `appendExecutorCases` for a config with a WS protocol includes WS cases (e.g. an `Action:"ws_connect"` case), and that a config with NO protocol yields the same cases as before (regression: snapshot the non-WS case count/IDs).

- [ ] **Step 2: Run to verify fail.**

- [ ] **Step 3: Implement** — in `appendExecutorCases`, after the existing `GenerateExecutorCases(info, goal)` append, add:

```go
cases = append(cases, WSCases(s.config, goal)...)
```

(`s` is the `*Scout` receiver; `s.config` is the `*project.Config`.)

- [ ] **Step 4: Run to verify pass** (incl. the regression that non-WS configs are unchanged).

- [ ] **Step 5: Commit** — `git commit -m "feat(scout): append WS cases to the plan"`.

---

## Task 3: Planning-prompt WS awareness (PROVISIONAL) + doc

**Files:**
- Modify: `internal/head/scout/prompts.go` (`promptPlanSystem`, inline raw-string edit)
- Modify: `cerberus-docs/executors/websocket.md` (note Scout WS case generation)

**STATUS: PROVISIONAL.** The exact prompt sentence and its gating (always-on vs conditional on WS services) must be tuned against a real target. The implementer should add a minimal, clearly-marked sentence and flag it for dogfooding refinement; do NOT over-engineer the wording.

- [ ] **Step 1:** Add a one-sentence WS note to `promptPlanSystem` (inline, no backticks), e.g.: "If the project declares WebSocket protocols, WS connection/receive cases are generated automatically from the protocol roles — focus your cases on HTTP and other surfaces." (Marked provisional in a code comment.)
- [ ] **Step 2:** Add a short subsection to `cerberus-docs/executors/websocket.md` documenting that Scout auto-generates WS cases from declared protocol roles (cross-link the M3-2 spec).
- [ ] **Step 3:** `make check` green; `grep` audit for stale bullets.
- [ ] **Step 4:** Commit — `git commit -m "docs(ws): note Scout WS case generation (provisional prompt)"`.

---

## Self-Review Notes

- **Spec coverage:** D1 (service-level, not ProjectType) → WSCases reads cfg.Services ✓; D2 (skeleton, one case per verification point) → Task 1 ✓; D3 (connect setup + DependsOn) → Task 1 ✓; D4 (handshake await_type + goal-named types, deterministic) → Task 1 ✓; D5 (prompt awareness) → Task 3 (provisional) ✓.
- **No struct change:** `agent.TestCase` reused as-is (Action/Body/Service/DependsOn/Background).
- **Provisional flags:** Task 3 prompt content and the `wsTypesNamedInGoal` heuristic are explicitly provisional (🐶 dogfooding). Tasks 1–2 are deterministic and fully tested.
- **Deferred (documented in spec):** full determinism via `TestCase.Steps` + `matchWSRules` — NOT in this plan.
