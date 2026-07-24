# WS Deterministic Relay Detector Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development. Checkbox tracking.

**Goal:** Deterministically emit a multi-connection relay `Steps` case when a ≥2-role protocol has an optional-handshake role (a peer-join signal), bypassing the GLM LLM-emission gap the live dogfood found.

**Architecture:** New `wsRelayCases(svc)` in `ws_cases.go` detects optional-handshake roles → emits connect-A + connect-peers + receive-signal `Steps` cases; `WSCasesCovered` calls it + suppresses the redundant single-conn receive of the covered signal. Executor/runSteps/stepToAction/TestStep/protocol-schema unchanged.

**Tech Stack:** Go 1.25; existing `slices`/`maps`, `project.Service`/`ProtocolRole`/`RoleHandshake`, `agent.TestStep`/`TestCase`, `sanitizeTypeID`.

## Global Constraints
- Go 1.25, pure-Go; `coder/websocket v1.8.14` ONLY; no new deps; no expression evaluator; no protocol-schema change.
- Production change confined to `internal/head/scout/ws_cases.go` (+ docs). Author `binoctal <binoctal@gmail.com>`; NO Co-Authored-By; English; docs only in `cerberus-docs/`; `make check` green.
- Determinism: roles/signals sorted; `Steps` order fixed (A, peers sorted, receive).
- Backwards-compat: no optional-handshake role / <2 roles ⇒ byte-identical WSCases output.

---

### Task 1: wsRelayCases + WSCasesCovered integration

**Files:**
- Modify: `internal/head/scout/ws_cases.go` (add `wsRelayCases`; wire into `WSCasesCovered`)
- Modify: `internal/head/scout/ws_cases_test.go` (or `ws_relay_test.go`) — add tests

**Interfaces:**
- Produces: `wsRelayCases(svc project.Service) ([]agent.TestCase, map[string]map[string]bool)`. Pure, deterministic.

**Reviewer note (controller):** sonnet. Verify: trigger is exactly optional-handshake (mandatory/no-handshake do not trigger); ≥2 roles required; connect order A-first then peers; the covered map drives suppression of the redundant single-conn receive; backwards-compat (no optional handshake ⇒ identical WSCases); determinism.

- [ ] **Step 1: Write failing tests**

Add to `internal/head/scout/ws_cases_test.go` (reuse `relayProtocol()` from `ws_relay_test.go` if helpful, or define a local one with an optional-handshake role):

```go
// wsRelayCases emits a relay case for an optional-handshake role in a ≥2-role
// protocol; connect the receiver first, then peers, then receive the signal.
func TestWSRelayCases(t *testing.T) {
	p := &project.Protocol{TypePath: "type", Roles: map[string]*project.ProtocolRole{
		"web":    {Handshake: &project.RoleHandshake{AwaitType: "device:online", Optional: true, Timeout: 2}},
		"bridge": {},
	}}
	svc := project.Service{Name: "rt", URL: "ws://h/ws", Protocol: p}

	cases, covered := wsRelayCases(svc)
	require.Len(t, cases, 1, "one relay case for web's optional handshake")
	c := cases[0]
	require.Equal(t, "ws-rt-relay-web-signal-device-online", c.ID)
	require.Equal(t, "ws://h/ws", c.Target)
	require.Equal(t, "ws_flow", c.Action)
	require.Len(t, c.Steps, 3)
	require.Equal(t, "ws_connect", c.Steps[0].Action) // web (receiver) first
	require.Equal(t, "web", c.Steps[0].ConnectionID)
	require.Equal(t, "ws_connect", c.Steps[1].Action) // bridge (peer)
	require.Equal(t, "bridge", c.Steps[1].ConnectionID)
	require.Equal(t, "ws_receive", c.Steps[2].Action)
	require.Equal(t, "device:online", c.Steps[2].Type)
	require.Equal(t, "web", c.Steps[2].ConnectionID)
	require.Equal(t, 2, c.Steps[2].Timeout)
	require.True(t, covered["web"]["device:online"])
}

// No emission when the trigger is absent.
func TestWSRelayCases_NoTrigger(t *testing.T) {
	for _, name := range []string{"single role", "mandatory handshake", "no handshake"} {
		t.Run(name, func(t *testing.T) {
			var p *project.Protocol
			switch name {
			case "single role":
				p = &project.Protocol{Roles: map[string]*project.ProtocolRole{"web": {Handshake: &project.RoleHandshake{AwaitType: "x", Optional: true}}}}
			case "mandatory handshake":
				p = &project.Protocol{Roles: map[string]*project.ProtocolRole{"web": {Handshake: &project.RoleHandshake{AwaitType: "x"}}, "bridge": {}}}
			case "no handshake":
				p = &project.Protocol{Roles: map[string]*project.ProtocolRole{"web": {}, "bridge": {}}}
			}
			cases, covered := wsRelayCases(project.Service{Name: "rt", Protocol: p})
			require.Empty(t, cases)
			require.Empty(t, covered)
		})
	}
}

// WSCasesCovered suppresses the redundant single-conn receive of a covered signal.
func TestWSCasesCovered_RelaySuppressesReceive(t *testing.T) {
	p := &project.Protocol{TypePath: "type", Roles: map[string]*project.ProtocolRole{
		"web":    {Handshake: &project.RoleHandshake{AwaitType: "device:online", Optional: true, Timeout: 2}},
		"bridge": {},
	}}
	cfg := &project.Config{Services: []project.Service{{Name: "rt", URL: "ws://h/ws", Protocol: p}}}
	got := WSCases(cfg, "verify web receives device:online")
	// A relay case is present.
	var relay *agent.TestCase
	for i := range got {
		if got[i].Action == "ws_flow" && len(got[i].Steps) > 1 {
			relay = &got[i]
		}
	}
	require.NotNil(t, relay, "deterministic relay case emitted")
	// No separate single-conn ws_receive device:online case (it is covered by the relay).
	for _, c := range got {
		if c.Action == "ws_receive" {
			require.NotContains(t, c.ID, "device-online", "redundant single-conn signal receive suppressed")
		}
	}
}
```

- [ ] **Step 2: Run tests → FAIL (`undefined: wsRelayCases`)**

`go test -run 'TestWSRelayCases|TestWSCasesCovered_RelaySuppressesReceive' ./internal/head/scout/`

- [ ] **Step 3: Implement**

Add `wsRelayCases` (verbatim from the spec) + wire into `WSCasesCovered` (call it at the top of the per-service loop; thread `relayCovered` into the per-role receive loop's skip). Exact code in the spec `cerberus-docs/superpowers/specs/2026-07-24-ws-deterministic-relay-detector-design.md`. Read the current `WSCasesCovered` before editing to place the integration correctly.

- [ ] **Step 4: Run tests → PASS**

`go test -race -count=1 ./internal/head/scout/` (new tests + existing scout tests green — backwards-compat).

- [ ] **Step 5: Commit**

```bash
git add internal/head/scout/ws_cases.go internal/head/scout/ws_cases_test.go
git commit -m "feat(ws): deterministic peer-join relay detector (bypass LLM emission gap)"
```

---

### Task 2: docs + live re-validation

**Files:**
- Modify: `cerberus-docs/executors/websocket.md` (note deterministic relay emission for optional-handshake roles)
- Modify: the live-result section of `cerberus-docs/technical/dogfood/2026-07-24-ws-scout-relay-llm-dogfood-procedure.md` (append the 3C resolution)

- [ ] **Step 1: websocket.md note** — under the Multi-connection / Scout-cases material: when a ≥2-role protocol has an optional-handshake role, Scout deterministically emits a multi-connection relay `Steps` case (connect the receiver + peers, receive the peer-join signal) — no LLM needed.
- [ ] **Step 2: live re-validation (controller, NOT a subagent)** — re-run `go test -tags live -run TestScoutRelayEmission_Live -v ./internal/head/scout/` with the device:online relay goal. Expected: a `ws_flow` multi-step case now appears (connect web + bridge + receive device:online) — the GLM gap is closed deterministically. Append the result to the procedure doc's live-result section.
- [ ] **Step 3: make check + commit**

`make check`; commit `docs(ws): deterministic relay detector doc + live re-validation`.

---

## Post-implementation (controller)
- [ ] **Whole-branch review (opus):** `326e0bd..HEAD`. Verify: trigger exactly optional-handshake; ≥2 roles; A-first connect order; suppression correct; backwards-compat; determinism; constraints; `make check` green; live probe now emits the relay case.
- [ ] **Finish:** ff-merge main + delete branch (NO push unless asked).
- [ ] **Memory + ledger:** 3C done — Scout relay generation now works deterministically for peer-join signals (GLM gap closed); type-transform exchanges still A1/future.
