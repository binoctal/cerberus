# WS Scout Relay Generation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let Scout auto-generate a multi-connection relay `Steps` case from goal + protocol: the planning LLM emits a `ws_relay` case carrying a relay intent, and a deterministic expander assembles the canonical multi-connection `Steps` (connect each role → ordered send/receive), executed by the unchanged `runSteps` (F1 capability).

**Architecture:** A1 (plan-time LLM) + intent/deterministic-assembly. New scout expander `expandWSRelayCases` runs in `augmentPlan`, expands each `ws_relay` case into a `Steps` case, returns covered-roles so `WSCases` skips redundant connects. No `TestCase`/`TestStep`/executor/protocol-schema change; `ws_relay` is a new action value + an expander.

**Tech Stack:** Go 1.25, `encoding/json`, `slices`, `strings`; existing `agent.TestCase`/`TestStep`, `project.Service`/`Protocol`/`ProtocolRole`/`RoleHandshake`.

## Global Constraints

- Go 1.25, pure-Go (no CGo); `coder/websocket v1.8.14` ONLY (not touched here).
- No new dependencies; no expression evaluator; no `TestCase`/`TestStep`/executor/protocol-schema change.
- Production change confined to: new `internal/head/scout/ws_relay.go`, `augmentPlan`/`appendExecutorCases` wiring, `WSCasesCovered` wrapper + role-skip in `ws_cases.go`, one `promptPlanSystem` bullet, docs. Executor/`runSteps`/`stepToAction`/`WebSocketExecutor` unchanged.
- Commit author `binoctal <binoctal@gmail.com>`; NO `Co-Authored-By`; English comments/commit messages; docs only in `cerberus-docs/`.
- `make check` (fmt + lint + test `-race`) green.
- Determinism: any map-derived output sorted (`slices.Sorted`/`slices.Sort`); the assembled `Steps` order is the intent's order (not map-derived).

---

### Task 1: the relay expander (core, pure, TDD)

**Files:**
- Create: `internal/head/scout/ws_relay.go`
- Create: `internal/head/scout/ws_relay_test.go`

**Interfaces:**
- Consumes: `agent.TestCase`/`agent.TestStep` (`internal/head/agent/types.go`), `project.Config`/`Service`/`Protocol`/`ProtocolRole`/`RoleHandshake` (`internal/project/`), existing `wsSendBody(typ string) string` (`ws_cases.go:115`).
- Produces: `expandWSRelayCases(cfg *project.Config, plan *agent.TestPlan) map[string]map[string]bool` — mutates `plan.Cases` in place (replaces `ws_relay` cases with `Steps` cases, drops invalid ones), returns `service → set(role)` covered.

**Reviewer note (controller):** standard (non-concurrency) task review = sonnet. Verify the expander is pure (no I/O, no LLM, deterministic), validation drops every malformed intent without panic, and the assembled `Steps` match the F1 multi-connection contract (per-step `ConnectionID`/`Role`; `ws_connect` dials `Target`).

- [ ] **Step 1: Write the failing tests**

Create `internal/head/scout/ws_relay_test.go`:

```go
package scout

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/project"
)

func relayProtocol() *project.Protocol {
	return &project.Protocol{TypePath: "type",
		Roles: map[string]*project.ProtocolRole{
			"web":    {Params: map[string]string{"type": "web"}, Handshake: &project.RoleHandshake{AwaitType: "device:online", Optional: true, Timeout: 2}},
			"bridge": {Params: map[string]string{"type": "bridge"}},
		}}
}

// TestExpandWSRelayCases_HappyPath: a well-formed intent expands to a Steps case
// that connects each role (in intent order) then runs the ordered send/receive,
// and reports both roles covered for the service.
func TestExpandWSRelayCases_HappyPath(t *testing.T) {
	cfg := &project.Config{Services: []project.Service{{Name: "rt", URL: "ws://h/ws", Protocol: relayProtocol()}}}
	body, _ := json.Marshal(map[string]any{
		"roles": []string{"web", "bridge"},
		"steps": []map[string]any{
			{"do": "receive", "role": "web", "type": "device:online"},
			{"do": "send", "role": "web", "type": "session:start"},
			{"do": "receive", "role": "web", "type": "session:created", "assert": map[string]any{"payload.ready": true}},
		},
	})
	plan := &agent.TestPlan{Cases: []agent.TestCase{
		{ID: "x", Action: "ws_relay", Service: "rt", Body: string(body), Name: "relay", Expectation: "relay works"},
		{ID: "y", Action: "api_request"}, // non-relay case is untouched
	}}

	covered := expandWSRelayCases(cfg, plan)

	require.Len(t, plan.Cases, 2, "ws_relay replaced in place, api_request kept")
	require.Equal(t, "api_request", plan.Cases[1].Action, "non-relay case untouched")
	got := plan.Cases[0]
	require.Equal(t, "ws-rt-relay-bridge-web", got.ID, "deterministic ID with sorted roles")
	require.Equal(t, "ws://h/ws", got.Target, "target from service URL")
	require.Equal(t, "rt", got.Service)
	require.Equal(t, "relay", got.Name, "LLM name preserved")
	require.NotEmpty(t, got.Steps)
	// connect order == intent roles order (web first).
	require.Equal(t, "ws_connect", got.Steps[0].Action)
	require.Equal(t, "web", got.Steps[0].ConnectionID)
	require.Equal(t, "web", got.Steps[0].Role)
	require.Equal(t, "ws_connect", got.Steps[1].Action)
	require.Equal(t, "bridge", got.Steps[1].ConnectionID)
	// ordered send/receive with role/type/assert.
	require.Equal(t, "ws_receive", got.Steps[2].Action)
	require.Equal(t, "device:online", got.Steps[2].Type)
	require.Equal(t, 2, got.Steps[2].Timeout, "receive timeout from web role handshake")
	require.Equal(t, "ws_send", got.Steps[3].Action)
	require.Equal(t, `{"type":"session:start"}`, got.Steps[3].Message)
	require.Equal(t, "ws_receive", got.Steps[4].Action)
	require.Equal(t, map[string]any{"payload.ready": true}, got.Steps[4].Asserts)
	// covered reported for both roles.
	require.True(t, covered["rt"]["web"])
	require.True(t, covered["rt"]["bridge"])
}

// TestExpandWSRelayCases_DropsInvalid: every malformed intent is dropped (the
// ws_relay case removed) and other cases survive; covered is empty.
func TestExpandWSRelayCases_DropsInvalid(t *testing.T) {
	cfg := &project.Config{Services: []project.Service{{Name: "rt", URL: "ws://h/ws", Protocol: relayProtocol()}}}
	cases := []struct {
		name string
		body string
	}{
		{"bad json", `not json`},
		{"fewer than 2 roles", `{"roles":["web"],"steps":[]}`},
		{"unknown role", `{"roles":["web","ghost"],"steps":[]}`},
		{"unknown service", `{"roles":["web","bridge"],"steps":[]}`}, // service mismatch handled below
		{"empty type", `{"roles":["web","bridge"],"steps":[{"do":"send","role":"web","type":""}]}`},
		{"step role not in roles", `{"roles":["web","bridge"],"steps":[{"do":"send","role":"ghost","type":"x"}]}`},
		{"bad do", `{"roles":["web","bridge"],"steps":[{"do":"push","role":"web","type":"x"}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := "rt"
			if tc.name == "unknown service" {
				svc = "nope"
			}
			plan := &agent.TestPlan{Cases: []agent.TestCase{
				{ID: "r", Action: "ws_relay", Service: svc, Body: tc.body},
				{ID: "keep", Action: "api_request"},
			}}
			covered := expandWSRelayCases(cfg, plan)
			require.Len(t, plan.Cases, 1, "%s: invalid ws_relay dropped", tc.name)
			require.Equal(t, "keep", plan.Cases[0].ID)
			require.Empty(t, covered, "%s: nothing covered", tc.name)
		})
	}
}

// TestExpandWSRelayCases_NilSafe: nil cfg/plan is a no-op returning empty covered.
func TestExpandWSRelayCases_NilSafe(t *testing.T) {
	require.Empty(t, expandWSRelayCases(nil, &agent.TestPlan{}))
	require.Empty(t, expandWSRelayCases(&project.Config{}, nil))
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -run TestExpandWSRelayCases ./internal/head/scout/`
Expected: build failure — `undefined: expandWSRelayCases`.

- [ ] **Step 3: Implement the expander**

Create `internal/head/scout/ws_relay.go`:

```go
package scout

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/project"
)

// relayIntent is the LLM-authored relay description carried in a ws_relay case's
// body. Roles is the ordered set of connections to open (the peer-join signal
// receiver first, so the DO pushes to an already-connected client); Steps is the
// ordered send/receive sequence across them. Assert (receive only) is a
// dotted-path -> value map passed through to the step.
type relayIntent struct {
	Roles []string    `json:"roles"`
	Steps []relayStep `json:"steps"`
}

type relayStep struct {
	Do     string         `json:"do"`   // "send" | "receive"
	Role   string         `json:"role"` // a connection named in Roles
	Type   string         `json:"type"` // message routing type
	Assert map[string]any `json:"assert"`
}

// expandWSRelayCases expands every ws_relay case in plan.Cases into a
// deterministic multi-connection Steps case (connect each role in Roles order,
// then the ordered send/receive) and replaces it in place. It returns the roles
// covered per service so WSCases can skip redundant connect cases for them.
// Invalid intents (unknown service/protocol/role, fewer than 2 roles, malformed
// body, bad step) are dropped — the case is removed; the run never fails. Pure:
// no LLM, no I/O, deterministic.
func expandWSRelayCases(cfg *project.Config, plan *agent.TestPlan) map[string]map[string]bool {
	covered := map[string]map[string]bool{}
	if cfg == nil || plan == nil {
		return covered
	}
	svcByName := map[string]*project.Service{}
	for i := range cfg.Services {
		svcByName[cfg.Services[i].Name] = &cfg.Services[i]
	}
	out := make([]agent.TestCase, 0, len(plan.Cases))
	for _, c := range plan.Cases {
		if c.Action != "ws_relay" {
			out = append(out, c)
			continue
		}
		exp, ok := expandOneRelayCase(svcByName, c)
		if !ok {
			continue // dropped: malformed/unresolvable ws_relay intent
		}
		out = append(out, exp.tc)
		if covered[exp.service] == nil {
			covered[exp.service] = map[string]bool{}
		}
		for _, r := range exp.roles {
			covered[exp.service][r] = true
		}
	}
	plan.Cases = out
	return covered
}

type expandedRelay struct {
	tc      agent.TestCase
	service string
	roles   []string
}

// expandOneRelayCase resolves the case's service + protocol, validates the
// intent, and assembles the Steps case. ok=false when the case should be dropped.
func expandOneRelayCase(svcByName map[string]*project.Service, c agent.TestCase) (expandedRelay, bool) {
	svc := svcByName[c.Service]
	if svc == nil || svc.Protocol == nil || len(svc.Protocol.Roles) == 0 {
		return expandedRelay{}, false
	}
	var intent relayIntent
	if err := json.Unmarshal([]byte(c.Body), &intent); err != nil {
		return expandedRelay{}, false
	}
	if len(intent.Roles) < 2 {
		return expandedRelay{}, false
	}
	// Every named role must be declared by this ONE protocol; collect the set so
	// step roles can be checked against it.
	declared := map[string]*project.ProtocolRole{}
	for _, r := range intent.Roles {
		role := svc.Protocol.Roles[r]
		if role == nil {
			return expandedRelay{}, false
		}
		declared[r] = role
	}
	for _, st := range intent.Steps {
		if (st.Do != "send" && st.Do != "receive") || st.Type == "" || declared[st.Role] == nil {
			return expandedRelay{}, false
		}
	}
	// Assemble: one connect per role (intent order), then the ordered steps.
	steps := make([]agent.TestStep, 0, len(intent.Roles)+len(intent.Steps))
	for _, r := range intent.Roles {
		steps = append(steps, agent.TestStep{Action: "ws_connect", ConnectionID: r, Role: r})
	}
	for _, st := range intent.Steps {
		switch st.Do {
		case "send":
			steps = append(steps, agent.TestStep{Action: "ws_send", ConnectionID: st.Role, Message: wsSendBody(st.Type)})
		case "receive":
			steps = append(steps, agent.TestStep{
				Action: "ws_receive", ConnectionID: st.Role, Type: st.Type,
				Asserts: st.Assert, Timeout: relayRecvTimeout(declared[st.Role]),
			})
		}
	}
	// Deterministic ID: sorted roles (connect order stays the intent's order).
	sortedRoles := append([]string(nil), intent.Roles...)
	slices.Sort(sortedRoles)
	return expandedRelay{
		tc: agent.TestCase{
			ID:          "ws-" + c.Service + "-relay-" + strings.Join(sortedRoles, "-"),
			Name:        c.Name,
			Service:     c.Service,
			Target:      svc.URL,
			Action:      "ws_flow", // informational; runSteps routes on len(Steps) > 0
			Expectation: c.Expectation,
			Steps:       steps,
		},
		service: c.Service,
		roles:   intent.Roles,
	}, true
}

// relayRecvTimeout returns the receive-await budget for a step on role r: the
// role's declared handshake timeout (seconds) if any, else 0 (executor default).
func relayRecvTimeout(r *project.ProtocolRole) int {
	if r != nil && r.Handshake != nil && r.Handshake.Timeout > 0 {
		return r.Handshake.Timeout
	}
	return 0
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -race -count=3 -run TestExpandWSRelayCases ./internal/head/scout/`
Expected: PASS, stable.

- [ ] **Step 5: Commit**

```bash
git add internal/head/scout/ws_relay.go internal/head/scout/ws_relay_test.go
git commit -m "feat(ws): deterministic ws_relay intent expander (Scout)"
```

---

### Task 2: wire the expander + WSCases dedupe

**Files:**
- Modify: `internal/head/scout/plan_phases.go` (`augmentPlan`, `appendExecutorCases`)
- Modify: `internal/head/scout/ws_cases.go` (rename body to `WSCasesCovered`; `WSCases` delegates; role-skip)
- Modify: any caller of `appendExecutorCases` outside `augmentPlan` (grep first)

**Interfaces:**
- Consumes: `expandWSRelayCases` (Task 1).
- Produces: `WSCasesCovered(cfg, goal, covered)`; `augmentPlan` expands then appends with covered roles.

**Reviewer note (controller):** sonnet. Verify the non-`ws_relay` plan is byte-identical to before (regression), `WSCases(cfg, goal)` still equals the old output (covered nil), and `appendExecutorCases` caller list is complete.

- [ ] **Step 1: Add the WSCasesCovered wrapper + role-skip**

In `internal/head/scout/ws_cases.go`, rename the existing `WSCases` func body to `WSCasesCovered(cfg *project.Config, goal string, covered map[string]map[string]bool) []agent.TestCase`, and at the top of the per-role loop add the skip:

```go
// WSCases generates deterministic WS test cases. It is WSCasesCovered with no
// roles covered, kept for compatibility (existing callers/tests).
func WSCases(cfg *project.Config, goal string) []agent.TestCase {
	return WSCasesCovered(cfg, goal, nil)
}

func WSCasesCovered(cfg *project.Config, goal string, covered map[string]map[string]bool) []agent.TestCase {
	if cfg == nil {
		return nil
	}
	var cases []agent.TestCase
	for _, svc := range cfg.Services {
		if svc.Protocol == nil || len(svc.Protocol.Roles) == 0 {
			continue
		}
		for _, roleName := range slices.Sorted(maps.Keys(svc.Protocol.Roles)) {
			if covered[svc.Name][roleName] {
				continue // role is covered by an expanded ws_relay; skip its cases
			}
			role := svc.Protocol.Roles[roleName]
			// ... (existing exchange / connect+receive logic unchanged) ...
		}
	}
	return cases
}
```

(Keep the existing body verbatim under the new name; only add the `covered` skip and the `WSCases` delegating wrapper.)

- [ ] **Step 2: Wire augmentPlan + appendExecutorCases**

In `internal/head/scout/plan_phases.go`:

```go
// augmentPlan expands LLM-authored ws_relay intents into multi-connection Steps
// cases, then appends executor-specific cases (process, code, WS). Covered roles
// (already connected by a relay) are passed so WSCases does not redundantly
// re-connect them.
func (s *Scout) augmentPlan(plan *agent.TestPlan, goal string) {
	covered := expandWSRelayCases(s.config, plan)
	if len(covered) > 0 {
		s.logger.Info("expanded ws_relay cases", zap.Int("covered_services", len(covered)))
	}
	s.appendExecutorCases(plan, goal, covered)
}

func (s *Scout) appendExecutorCases(plan *agent.TestPlan, goal string, covered map[string]map[string]bool) {
	rootDir := s.config.Code.Root
	if rootDir == "" {
		rootDir = "."
	}
	info := DetectProjectType(rootDir)
	cases := GenerateExecutorCases(info, goal)
	cases = append(cases, WSCasesCovered(s.config, goal, covered)...)
	if len(cases) > 0 {
		s.logger.Info("appended executor cases",
			zap.String("project_type", string(info.Type)),
			zap.Int("cases", len(cases)),
		)
		plan.Cases = append(plan.Cases, cases...)
	}
}
```

First run `grep -rn "appendExecutorCases" internal/` and update any caller other than `augmentPlan` to pass `nil` (or thread covered). (`appendExecutorCases` is currently called only from `augmentPlan`; confirm and note in the report.)

- [ ] **Step 3: Regression + wiring tests**

Add to `internal/head/scout/ws_relay_test.go` (or `plan_phases_test.go`):

```go
// TestWSCasesCovered_NilEqualsWSCases: covered=nil reproduces the old WSCases
// output exactly (backwards compatibility).
func TestWSCasesCovered_NilEqualsWSCases(t *testing.T) {
	cfg := &project.Config{Services: []project.Service{{Name: "rt", URL: "ws://h/ws", Protocol: relayProtocol()}}}
	require.Equal(t, WSCases(cfg, "send device:command receive device:ack"),
		WSCasesCovered(cfg, "send device:command receive device:ack", nil))

	// Covered role is skipped.
	got := WSCasesCovered(cfg, "send device:command receive device:ack",
		map[string]map[string]bool{"rt": {"web": true}})
	for _, c := range got {
		require.NotContains(t, c.ID, "-web-", "web role covered -> no web cases emitted")
	}
}
```

Run: `go test -race -count=1 ./internal/head/scout/`
Expected: PASS (all existing scout tests + new expander + regression). If an existing test calls `appendExecutorCases(plan, goal)` directly, update it to `appendExecutorCases(plan, goal, nil)`.

- [ ] **Step 4: Commit**

```bash
git add internal/head/scout/plan_phases.go internal/head/scout/ws_cases.go internal/head/scout/ws_relay_test.go
git commit -m "feat(ws): wire ws_relay expander into Scout augmentPlan + WSCases dedupe"
```

---

### Task 3: prompt + docs

**Files:**
- Modify: `internal/head/scout/prompts.go` (`promptPlanSystem`: add one WS relay bullet)
- Modify: `cerberus-docs/executors/websocket.md` (add a "Scout-generated relay cases" note under the M3-2 / multi-connection sections)

**Reviewer note (controller):** small doc/prompt = inline + opus final. Raw-string integrity (single literal, inline edit, no backticks/`${}`).

- [ ] **Step 1: Add the prompt bullet**

In `internal/head/scout/prompts.go`, append one bullet to the WS-awareness text in `promptPlanSystem` (the existing M3-2 bullet ends with "...focus your cases on HTTP and other surfaces."). Add:

```
- WebSocket relay: if a goal describes a multi-party relay (two or more protocol roles exchanging messages through a broker, e.g. "web sends X and receives the relayed Y while bridge is connected"), emit ONE ws_relay case per exchange with the service, an ordered roles list (the peer-join signal receiver first), and an ordered steps list of {do: send|receive, role, type, assert?}. Do not also emit single-role ws_connect/ws_receive cases for roles the relay covers.
```

(Edit inline within the existing raw string; no backticks or `${}` in the inserted text.)

- [ ] **Step 2: Doc note**

In `cerberus-docs/executors/websocket.md`, under the "Scout-generated cases (M3-2)" subsection (or the new "Multi-connection orchestration" subsection), add a short note: Scout may emit a `ws_relay` case (action `ws_relay`, body = `{roles, steps}`); `augmentPlan` deterministically expands it into the multi-connection `Steps` case described above. Cross-link the spec.

- [ ] **Step 3: make check + commit**

Run: `make check` (fmt + lint + test `-race`).
Expected: green. Commit:

```bash
git add internal/head/scout/prompts.go cerberus-docs/executors/websocket.md
git commit -m "docs(ws): Scout ws_relay generation prompt + doc"
```

---

### Task 4: dogfood validation (controller-run, best-effort)

**Files:** none committed unless findings warrant a doc update.

- [ ] **Controller step:** confirm the LLM emits a well-formed `ws_relay` intent for the open-agents relay. Options (cheapest first): (a) a Scout-level unit/integration test that feeds a relay goal + the open-agents protocol through `Scout.Plan` with a mock driver returning a `ws_relay` case, asserting `expandWSRelayCases` produces the expected `Steps`; OR (b) a live `cerberus run` against open-agents (provisioned credentials) inspecting whether the LLM emits `ws_relay`. The deterministic expander (Task 1) is the testable guarantee; the LLM output quality is the A1 risk (R8). Record findings; a malformed intent is dropped gracefully (the expander logs nothing fatal, the hand-authored F1 path still works). Add a short findings note to `cerberus-docs/technical/dogfood/` if a live run reveals prompt-tuning needs.

---

## Post-implementation (controller)

- [ ] **Whole-branch final review (opus):** `c08dffb..HEAD`. Verify: production change confined to scout (expander + wiring + prompt); executor/runSteps/stepToAction/TestStep/protocol-schema unchanged; expander pure + deterministic + drops malformed intents without panic; WSCases backward-compat (covered nil == old output); connect order == intent roles order; constraints (author/no-Co-Authored-By/English/cerberus-docs/coder/websocket v1.8.14/no new deps); `make check` green.
- [ ] **Finish:** superpowers:finishing-a-development-branch — local ff-merge to main + `make check` + delete branch. DO NOT push.
- [ ] **Memory + ledger:** refresh `ws-realtime-engine-roadmap` + `ws-dogfood-tier1-checkpoint` (Scout relay generation done; remaining F3/F4), append `.superpowers/sdd/progress.md`.
