# Finding-2: Suppress Redundant Single-Connection WS Connect Cases — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stop `WSCasesCovered` from emitting redundant single-connection WS connect cases for roles already connected by the 3C deterministic relay case.

**Architecture:** `wsRelayCases` gains a `connectedRoles` return (receiver + every peer); `WSCasesCovered`'s role loop skips the single-conn connect+receive form for those roles (goal-exchange `wsStepsCase`, which precedes the check, is preserved). Scout-only; no executor/Steer change.

**Tech Stack:** Go 1.25, `internal/head/scout` (`ws_cases.go`), testify.

## Global Constraints

- Go 1.25, pure-Go (no CGo); module `github.com/binoctal/cerberus`
- `coder/websocket` v1.8.14 only (unchanged — scout-only change adds no WS deps)
- No expression/evaluator deps
- Commit author: `binoctal <binoctal@gmail.com>`, **NO** Co-Authored-By
- Code comments + commit messages in English
- Docs only in `cerberus-docs/` (never `docs/`)
- `make check` (fmt + lint + test -race) must be green
- Map iteration stays sorted (`connectedRoles` is membership-only; the role loop already iterates sorted keys)
- Follow existing comment density/naming idiom

---

### Task 1: `connectedRoles` mechanism — report + suppress redundant single-conn connect

The signature change, the caller, and the role-loop consumption are one compile
unit (a Go local must be used), so they ship together. TDD: tests first drive
the new `connectedRoles` return, then the connect-suppression behavior.

**Files:**
- Modify: `internal/head/scout/ws_cases.go` — `wsRelayCases` (signature + body, ~line 128-167), `WSCasesCovered` caller (~line 55) and role loop (~line 82-86)
- Test: `internal/head/scout/ws_cases_test.go` — `TestWSRelayCases` (~line 455), `TestWSRelayCases_NoTrigger` (~line 481), new `TestWSCasesCovered_RelaySuppressesConnect`

**Interfaces:**
- Consumes: none new.
- Produces: `wsRelayCases` new signature `(cases []agent.TestCase, covered map[string]map[string]bool, connectedRoles map[string]bool)` — `connectedRoles` names every role a relay case opens a socket for (receiver `Steps[0].Role` + all peers).

- [ ] **Step 1: Write the failing tests (RED)**

In `TestWSRelayCases` (~line 462), change the call and append two assertions after the existing `require.True(t, covered["web"]["device:online"])`:

```go
	cases, covered, connected := wsRelayCases(svc)
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
	// Finding-2: the relay case connects the receiver (web) AND its peer (bridge).
	require.True(t, connected["web"], "receiver is connected by the relay")
	require.True(t, connected["bridge"], "peer is connected by the relay")
```

In `TestWSRelayCases_NoTrigger` (~line 493), update the call and add an assertion:

```go
				cases, covered, connected := wsRelayCases(project.Service{Name: "rt", Protocol: p})
				require.Empty(t, cases)
				require.Empty(t, covered)
				require.Empty(t, connected)
```

Add a ≥3-role test (place it right after `TestWSRelayCases`) — covers receiver + multiple peers:

```go
// ≥3 roles: the receiver and ALL peers land in connectedRoles.
func TestWSRelayCases_MultiPeer(t *testing.T) {
	p := &project.Protocol{TypePath: "type", Roles: map[string]*project.ProtocolRole{
		"web":    {Handshake: &project.RoleHandshake{AwaitType: "device:online", Optional: true, Timeout: 2}},
		"bridge": {},
		"app":    {},
	}}
	_, _, connected := wsRelayCases(project.Service{Name: "rt", Protocol: p})
	for _, role := range []string{"web", "bridge", "app"} {
		require.True(t, connected[role], "%s connected by the relay", role)
	}
}
```

Add a behavior test (place it right after `TestWSCasesCovered_RelaySuppressesReceive`):

```go
// Finding-2: when the deterministic relay case connects a role (receiver or
// peer), WSCasesCovered suppresses the redundant single-conn connect case for
// it — the connect runs inside the relay case's Steps, and the lone single-conn
// form would route through Steer and fail. goal-exchange wsStepsCase is still
// emitted (it precedes the connectedRoles check).
func TestWSCasesCovered_RelaySuppressesConnect(t *testing.T) {
	p := &project.Protocol{TypePath: "type", Roles: map[string]*project.ProtocolRole{
		"web":    {Handshake: &project.RoleHandshake{AwaitType: "device:online", Optional: true, Timeout: 2}},
		"bridge": {},
	}}
	cfg := &project.Config{Services: []project.Service{{Name: "rt", URL: "ws://h/ws", Protocol: p}}}
	cases := WSCases(cfg, "verify web receives device:online")

	// The relay case is present.
	var hasRelay bool
	for _, c := range cases {
		if c.Action == "ws_flow" && len(c.Steps) > 1 {
			hasRelay = true
		}
	}
	require.True(t, hasRelay, "deterministic relay case emitted")

	// No single-conn connect case for the roles the relay connects (web+bridge).
	connects := filterAction(cases, "ws_connect")
	require.Empty(t, connects, "relay covers web+bridge; no redundant single-conn connect")
}
```

- [ ] **Step 2: Run the tests to verify RED**

Run: `go test ./internal/head/scout/ -run 'TestWSRelayCases|TestWSCasesCovered_RelaySuppressesConnect' -v`
Expected: COMPILE ERROR — `assignment mismatch: 2 variables but wsRelayCases returns 3 values` (and the new test cannot compile yet either).

- [ ] **Step 3: Update `wsRelayCases` signature + fill `connectedRoles`**

In `internal/head/scout/ws_cases.go`, change the signature, init `connectedRoles`, return it from both exit points, and populate it. The signature + guard return (~line 128):

```go
// wsRelayCases emits deterministic multi-connection relay Steps cases for the
// peer-join signals in a service's protocol: each role with an OPTIONAL handshake
// (await_type T) receives T when a peer connects. Returns the cases, the
// (role → set(signalType)) pairs they cover (so the per-role loop can skip the
// redundant single-connection receive), and connectedRoles — every role a relay
// case opens a socket for (receiver + peers), so the per-role loop can skip the
// redundant single-connection connect form. Pure; no LLM. Deterministic (sorted).
func wsRelayCases(svc project.Service) ([]agent.TestCase, map[string]map[string]bool, map[string]bool) {
	var cases []agent.TestCase
	covered := map[string]map[string]bool{}
	connectedRoles := map[string]bool{}
	if svc.Protocol == nil || len(svc.Protocol.Roles) < 2 {
		return cases, covered, connectedRoles
	}
```

After the existing `covered[aName][signal] = true` block (still inside the per-receiver loop), add the connectedRoles population:

```go
		// Finding-2: this relay case opens a socket for the receiver and every
		// peer, so WSCasesCovered can skip their redundant single-conn
		// connect+receive form (the connect runs in this relay case's Steps).
		connectedRoles[aName] = true
		for _, p := range peers {
			connectedRoles[p] = true
		}
```

Change the final `return` at the end of the function:

```go
	return cases, covered, connectedRoles
}
```

- [ ] **Step 4: Update the prod caller (~line 55)**

```go
		relayCases, relayCovered, relayConnected := wsRelayCases(svc)
```

(`relayConnected` is consumed in Step 6; until then this file will not compile,
which is expected mid-task.)

- [ ] **Step 5: Run the relay tests to verify the signature half is GREEN**

Run: `go test ./internal/head/scout/ -run TestWSRelayCases -v`
Expected: still COMPILE ERROR on the file (relayConnected unused) — proceed to Step 6, which consumes it. (Do NOT add `_ = relayConnected`.)

- [ ] **Step 6: Consume `relayConnected` in the role loop (GREEN)**

In `WSCasesCovered`'s role loop, add the skip right after the goal-exchange branch and before `connectID`:

```go
			if ex, ok := wsExchangeFromGoal(goal); ok {
				cases = append(cases, wsStepsCase(svc, roleName, role, ex))
				continue
			}
			// Finding-2: the deterministic relay case already connects this role
			// (receiver or peer). Its single-conn connect+receive form is
			// redundant (the connect runs in the relay Steps) and, routed through
			// Steer, unreliable. Skip the whole form — a dependent receive would
			// otherwise dangle without its connect. goal-exchange wsStepsCase
			// above is preserved.
			if relayConnected[roleName] {
				continue
			}
			connectID := wsCaseID(svc.Name, roleName, "connect")
```

- [ ] **Step 7: Run the targeted tests to verify GREEN**

Run: `go test ./internal/head/scout/ -run 'TestWSRelayCases|TestWSCasesCovered_RelaySuppresses' -v`
Expected: PASS (TestWSRelayCases, TestWSRelayCases_NoTrigger, TestWSCasesCovered_RelaySuppressesReceive, TestWSCasesCovered_RelaySuppressesConnect).

- [ ] **Step 8: Regression — full scout package (existing WSCases* tests must be unchanged)**

Run: `go test ./internal/head/scout/`
Expected: PASS. All existing `WSCases*` tests are single-role or non-optional multi-role, so none trigger the new suppression.

- [ ] **Step 9: make check (fmt + lint + test -race)**

Run: `make check`
Expected: EXIT 0.

- [ ] **Step 10: Commit**

```bash
git add internal/head/scout/ws_cases.go internal/head/scout/ws_cases_test.go
git commit -m "feat(ws): suppress redundant single-conn connect for relay roles

wsRelayCases reports connectedRoles (receiver + peers); WSCasesCovered skips
the single-conn connect+receive form for those roles (the deterministic relay
case already connects them; the lone single-conn form routed through Steer and
failed). goal-exchange wsStepsCase is preserved. Scout-only."
```

---

### Task 2: Document the suppression + optional live dogfood re-run

**Files:**
- Modify: `cerberus-docs/executors/websocket.md` — the deterministic-relay note (in the "Multi-connection orchestration" subsection, the note added by the 3C detector)
- Manual (optional): a live `cerberus run` against open-agents — not part of `make check`

**Interfaces:** none.

- [ ] **Step 1: Append the suppression note to the deterministic-relay paragraph**

Find the deterministic-relay note in the "Multi-connection orchestration" subsection of `cerberus-docs/executors/websocket.md` (the paragraph that describes the 3C detector emitting a relay Steps case when a role has an optional handshake). Append one sentence to that paragraph:

```
Roles a relay case connects (receiver + peers) also have their redundant single-connection connect case suppressed, since the connect already runs inside the relay Steps and the lone single-conn form would route through the Steer planner unreliably; a goal send→receive exchange (wsStepsCase) is still emitted for any role.
```

- [ ] **Step 2: (Optional) Live dogfood re-run — confirm the redundant connect cases disappear**

With open-agents on `:8989` and a built binary:

```bash
# provision (reuse the dogfood config from 2026-07-24, or re-provision)
curl -sS -X POST -H "Content-Type: application/json" -H "Origin: http://localhost:8989" \
  -d '{}' http://localhost:8989/api/dev/setup
# run cerberus with the dogfood project.yaml (web+bridge optional-handshake relay)
./build/cerberus run --config <dogfood project.yaml> --dir <tmp> --db <tmp.db> \
  --goal "The web client and the bridge client connect to the realtime service on the same user. When the bridge client joins, the web client receives the relayed device:online signal."
```

Expected: the generated case set no longer contains `ws-realtime-web-connect` / `ws-realtime-bridge-connect`; `ws-realtime-relay-web-signal-device-online` is still present and PASSES. (This is a manual confirmation, not a `make check` step — skip if open-agents is not running.)

- [ ] **Step 3: Commit the doc**

```bash
git add cerberus-docs/executors/websocket.md
git commit -m "docs(ws): note relay-role single-conn connect suppression"
```
