# WS Multi-Connection Orchestration (F1) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Prove, lock with tests, dogfood on real traffic, and document cerberus's ability to orchestrate multiple WebSocket connections (e.g. web↔bridge relay) within a single `Steps` case — with zero production-code change (the capability already exists mechanically).

**Architecture:** A `TestCase` whose `Steps` cite different `connection_id`s already lands on distinct connections (the executor keys its table by `<caseID>:<connectionID>`; `runSteps` has no single-connection assumption). F1 adds (1) a deterministic in-process relay test that proves multi-connection relay + optional-handshake-survival under `-race`, (2) a `//go:build integration` test that dogfoods cerberus's own `runSteps` against a live open-agents target, and (3) docs. No executor/runSteps/stepToAction/TestStep/protocol-schema change.

**Tech Stack:** Go 1.25, `coder/websocket v1.8.14`, `net/http/httptest`, `testify/require`.

## Global Constraints

- Go 1.25, pure-Go (no CGo); `coder/websocket v1.8.14` ONLY (forbidden: nhooyr, gorilla).
- No new dependencies; no expression evaluator; no protocol-schema change.
- Commit author `binoctal <binoctal@gmail.com>`; NO `Co-Authored-By`; English comments and commit messages.
- ALL docs in `cerberus-docs/` (never `docs/`).
- `make check` (fmt + lint + test `-race`) green; the integration test is `//go:build integration` and excluded from `make test`.
- Determinism: sort any map iteration used in error/reporting paths (not needed here — the relay hub forwards by lookup, not iteration-order-dependent output).
- Never `pkill -f <pattern>` from a bash whose argv contains it.

---

### Task 1: Multi-connection relay capability test

**Files:**
- Modify: `internal/head/agent/websocket_test.go` (add `newWSRelayServer` helper next to `newWSTestServer`)
- Modify: `internal/head/agent/execute_phases_steps_test.go` (add `TestRunStepsMultiConnection`)

**Interfaces:**
- Consumes: `newStepExecutionWithIdx(t, tc, wsIdx)` (existing, `execute_phases_steps_test.go:29`), `protocolIndexForURL(t, url, p)` (existing, `websocket_test.go:57`), `project.Protocol`/`ProtocolRole`/`RoleHandshake` (existing, `internal/project/protocol_schema.go`), `TestCase`/`TestStep`/`StepPassed` (existing, `internal/head/agent/types.go`).
- Produces: `newWSRelayServer(t *testing.T) (url string, accepts *atomic.Int32)` — a relay test server used by this task's test (and reusable by the integration test if desired).

**Reviewer note (controller):** this task is concurrency-heavy (a shared relay hub across goroutines + two read pumps under `-race`). Task review MUST be opus and MUST verify: hub mutex covers every hub read/write; no data race under `-race -count=3`; both connections genuinely distinct (accepts==2); the `web` connection survives its optional-handshake timeout (step 6 receive on `c-web` succeeds).

- [ ] **Step 1: Write the failing test**

Append to `internal/head/agent/execute_phases_steps_test.go`:

```go
// TestRunStepsMultiConnection proves a single Steps case can orchestrate TWO
// connections (web + bridge) and relay frames across them. The in-process relay
// server forwards web<->bridge frames (modeling a broker/DO). The web role
// carries an OPTIONAL handshake that times out but leaves the connection alive,
// so step 6 receiving on c-web also proves optional-handshake-survival across a
// multi-connection case. accepts==2 proves the two steps opened two DISTINCT
// connections in one case (not one shared connection).
func TestRunStepsMultiConnection(t *testing.T) {
	wsURL, accepts := newWSRelayServer(t)

	p := &project.Protocol{TypePath: "type",
		Roles: map[string]*project.ProtocolRole{
			"web": {
				Params:    map[string]string{"type": "web"},
				Handshake: &project.RoleHandshake{AwaitType: "devices:sync", Optional: true, Timeout: 1},
			},
			"bridge": {
				Params: map[string]string{"type": "bridge"},
			},
		}}
	wsIdx := protocolIndexForURL(t, wsURL, p)

	tc := &TestCase{
		ID:     "tc-multi-conn",
		Target: wsURL,
		Steps: []TestStep{
			{Action: "ws_connect", Role: "web", ConnectionID: "c-web"},
			{Action: "ws_connect", Role: "bridge", ConnectionID: "c-bridge"},
			{Action: "ws_send", ConnectionID: "c-web", Message: `{"type":"echo:web"}`},
			{Action: "ws_receive", ConnectionID: "c-bridge", Type: "echo:web", Timeout: 2},
			{Action: "ws_send", ConnectionID: "c-bridge", Message: `{"type":"echo:bridge"}`},
			{Action: "ws_receive", ConnectionID: "c-web", Type: "echo:bridge", Timeout: 2},
		},
	}

	se := newStepExecutionWithIdx(t, tc, wsIdx)
	result := se.runSteps()

	require.Equal(t, StepPassed, result.Status, "multi-connection relay case should pass")
	require.Len(t, result.Evidence, 6, "one evidence entry per step")
	require.Equal(t, int32(2), accepts.Load(),
		"the case must open two distinct connections (accepts=%d)", accepts.Load())
}
```

- [ ] **Step 2: Run test to verify it fails (compile error — helper undefined)**

Run: `go test -run TestRunStepsMultiConnection ./internal/head/agent/`
Expected: build failure — `undefined: newWSRelayServer`.

- [ ] **Step 3: Implement the relay server helper**

Add to `internal/head/agent/websocket_test.go` (next to `newWSTestServer`, ~line 48). All required imports (`sync`, `sync/atomic`, `context`, `net/http`, `net/http/httptest`, `strings`, `testing`, `time`, `websocket`) are already present in this file.

```go
// newWSRelayServer starts an httptest server whose /ws path accepts multiple
// connections, identifies each by its ?type= query param (web|bridge), and
// relays every frame from one connection to the other — modeling a broker / DO
// that forwards web<->bridge. It returns the ws:// URL and an accept counter.
// Race-clean: the hub map is guarded by mu; forwarding looks up the peer under
// the lock, then writes outside it. Used by the multi-connection Steps test.
func newWSRelayServer(t *testing.T) (string, *atomic.Int32) {
	t.Helper()
	var accepts atomic.Int32
	var mu sync.Mutex
	hub := map[string]*websocket.Conn{} // role -> conn

	// forward writes data to the connection whose role differs from fromRole
	// (the single peer in a two-role relay). No peer yet -> drop (the test only
	// sends after both connections are up).
	forward := func(fromRole string, mt websocket.MessageType, data []byte) {
		mu.Lock()
		var target *websocket.Conn
		for role, c := range hub {
			if role != fromRole {
				target = c
				break
			}
		}
		mu.Unlock()
		if target == nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = target.Write(ctx, mt, data)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		accepts.Add(1)
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("accept: %v", err)
			return
		}
		defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()
		role := r.URL.Query().Get("type")
		mu.Lock()
		hub[role] = conn
		mu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for {
			mt, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			forward(role, mt, data)
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws", &accepts
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -race -count=3 -run TestRunStepsMultiConnection ./internal/head/agent/`
Expected: PASS (within ~1 s per run for the optional-handshake timeout; `-race -count=3` stable). This is GREEN proving the multi-connection relay capability on a live in-process server.

- [ ] **Step 5: Commit**

```bash
git add internal/head/agent/websocket_test.go internal/head/agent/execute_phases_steps_test.go
git commit -m "test(ws): multi-connection relay capability (Steps case, 2 conns)"
```

---

### Task 2: open-agents integration dogfood test

**Files:**
- Create: `internal/head/agent/execute_phases_steps_integration_test.go` (`//go:build integration`)

**Interfaces:**
- Consumes: `newStepExecutionWithIdx` (existing), `TestCase`/`TestStep`/`StepPassed`/`StepFailed`/`Evidence` (existing), `project.Protocol`/`ProtocolAuth`/`ProtocolRole`/`RoleHandshake` (existing), `WSProtocolIndex{ByHost, ActorTokens}` (existing, `ws_protocol.go:98`).
- Produces: `TestRunStepsMultiConnectionOpenAgents` (build-tagged; not referenced by default build).

**Reviewer note (controller):** standard (non-concurrency) task review = sonnet. The dogfood RUN itself (bring up open-agents, execute with `-tags integration`, capture findings, tune provisional assertions) is performed by the CONTROLLER after the implementer finishes — not by the implementer subagent.

- [ ] **Step 1: Write the integration test**

Create `internal/head/agent/execute_phases_steps_integration_test.go`:

```go
//go:build integration

package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/project"
)

// TestRunStepsMultiConnectionOpenAgents dogfoods cerberus's multi-connection
// orchestration against a live open-agents target. Build-tagged out of `make
// test`. To run:
//
//	fnm use 22 && cd ../open-agents/apps/api && npm run dev   # serves :8989
//	go test -tags integration -run TestRunStepsMultiConnectionOpenAgents ./internal/head/agent/
//
// Hard asserts are capability-level: cerberus must establish two real sockets
// (web + bridge) to the SAME /ws/<userId> DO (both connects succeed). Exact
// protocol matching (devices:sync push, session:start->session:created relay)
// is BEST-EFFORT: open-agents' relay vocabulary is discovered at run time, so a
// mismatch is a dogfood finding, not a cerberus regression (the deterministic
// TestRunStepsMultiConnection is the mechanical proof).
func TestRunStepsMultiConnectionOpenAgents(t *testing.T) {
	const base = "http://localhost:8989"
	if !reachable(base) {
		t.Skipf("open-agents not reachable at %s; bring up `npm run dev` (apps/api)", base)
	}

	// Provision a user + bridge device. demo_token (dev backdoor) authenticates
	// the web socket for any userId; the device token authenticates bridge.
	// Both connect to /ws/<userId> so they share one UserRoom DO (the relay).
	userId, deviceToken, err := devSetup(base)
	require.NoError(t, err, "POST /api/dev/setup")
	t.Logf("provisioned userId=%s deviceToken=%s", userId, deviceToken)

	p := &project.Protocol{
		TypePath: "type",
		Auth:     &project.ProtocolAuth{Strategy: "query", Param: "token", CredentialRef: "web-actor"},
		Roles: map[string]*project.ProtocolRole{
			"web": {
				CredentialRef: "web-actor",
				Params:        map[string]string{"type": "web"},
				Handshake:     &project.RoleHandshake{AwaitType: "devices:sync", Optional: true, Timeout: 2},
			},
			"bridge": {
				CredentialRef: "bridge-actor",
				Params:        map[string]string{"type": "bridge"},
			},
		},
	}
	wsIdx := &WSProtocolIndex{
		ByHost: map[string]*project.Protocol{"localhost:8989": p},
		ActorTokens: map[string]string{
			"web-actor":    "demo_token",
			"bridge-actor": deviceToken,
		},
	}

	tc := &TestCase{
		ID:     "tc-openagents-relay",
		Target: "ws://localhost:8989/ws/" + userId,
		Steps: []TestStep{
			{Action: "ws_connect", Role: "web", ConnectionID: "c-web"},
			{Action: "ws_connect", Role: "bridge", ConnectionID: "c-bridge"},
			// Relay signal: the DO pushes devices:sync to web once bridge joins.
			{Action: "ws_receive", ConnectionID: "c-web", Type: "devices:sync", Timeout: 3},
			// Best-effort request/reply across the relay.
			{Action: "ws_send", ConnectionID: "c-web", Message: `{"type":"session:start"}`},
			{Action: "ws_receive", ConnectionID: "c-web", Type: "session:created", Timeout: 3},
		},
	}

	se := newStepExecutionWithIdx(t, tc, wsIdx)
	result := se.runSteps()
	for _, ev := range result.Evidence {
		t.Logf("step evidence: %s", ev.Content)
	}

	// HARD capability assertion: both connects succeeded. Evidence is appended
	// per step before the success check, and the case short-circuits on failure,
	// so reaching the 3rd step (index 2) means steps 0 and 1 (the two connects)
	// both succeeded => cerberus opened two real sockets to the same DO.
	require.GreaterOrEqual(t, len(result.Evidence), 3,
		"both connects must succeed (web + bridge); evidence=%d", len(result.Evidence))

	// BEST-EFFORT: the full relay. Log the outcome; do not fail the test on a
	// protocol mismatch (that is a dogfood finding about open-agents).
	if result.Status == StepPassed {
		t.Logf("relay case fully passed: status=%s", result.Status)
	} else {
		t.Logf("relay case did not fully pass (dogfood finding): status=%s", result.Status)
	}
}

// reachable reports whether base responds to a GET within 2 s.
func reachable(base string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base, nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return true
}

// devSetup POSTs /api/dev/setup and returns the provisioned userId + deviceToken
// (response.config.{userId,deviceToken}).
func devSetup(base string) (userId, deviceToken string, err error) {
	resp, err := http.Post(base+"/api/dev/setup", "application/json", strings.NewReader(`{}`))
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	var out struct {
		Config struct {
			UserId      string `json:"userId"`
			DeviceToken string `json:"deviceToken"`
		} `json:"config"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", "", err
	}
	return out.Config.UserId, out.Config.DeviceToken, nil
}
```

- [ ] **Step 2: Verify it compiles under the integration tag and is excluded by default**

Run: `go vet -tags integration ./internal/head/agent/`
Expected: no issues.

Run: `go test -run TestRunStepsMultiConnectionOpenAgents ./internal/head/agent/`
Expected: PASS with `--- SKIP` (no tests matched — the build tag excludes the file from the default build), confirming `make test` will not run it.

- [ ] **Step 3: Verify offline skip**

(With open-agents NOT running) Run: `go test -tags integration -run TestRunStepsMultiConnectionOpenAgents -v ./internal/head/agent/`
Expected: `--- SKIP: TestRunStepsMultiConnectionOpenAgents` with the "not reachable" message. No hard-assertion failure.

- [ ] **Step 4: Commit**

```bash
git add internal/head/agent/execute_phases_steps_integration_test.go
git commit -m "test(ws): open-agents multi-connection integration dogfood (build-tagged)"
```

- [ ] **Step 5: Controller dogfood run + findings (NOT a subagent step)**

The controller brings up open-agents and runs the integration test on real traffic, then records findings. Start open-agents in its OWN process group (via `setsid`) so the whole tree (npm + wrangler) can be killed together; NEVER `pkill -f <pattern>` from a bash whose argv contains that pattern (self-SIGTERM, exit 144):

```bash
# Start open-agents (sibling repo) in a new session/process group.
eval "$(fnm env --shell bash)" && fnm use 22
setsid bash -c 'cd /home/mason/Documents/code_projects/private/open-agents/apps/api && npm run dev' &
OA_PGID=$!            # setsid PID == new process-group ID
cd /home/mason/Documents/code_projects/private/cerberus
# Wait for :8989 to accept (poll), then run the integration test.
go test -tags integration -run TestRunStepsMultiConnectionOpenAgents -v ./internal/head/agent/
# Kill the whole process group (negative PID), not pkill -f.
kill -- -"$OA_PGID" 2>/dev/null || true
```

If the run reveals the provisional assertions (step types) need tuning, the controller adjusts the `Steps` in the test to match observed behavior and re-runs. Outcome + observed frames are summarized in `cerberus-docs/technical/dogfood/2026-07-23-ws-f1-multi-connection-dogfood.md` (findings doc). A non-passing relay (e.g. devices:sync not pushed) is recorded as a dogfood finding, NOT a test failure to force.

---

### Task 3: Documentation

**Files:**
- Modify: `cerberus-docs/executors/websocket.md` (add "Multi-connection orchestration" subsection after the "Deterministic multi-step cases (Steps)" subsection, before "### M0 fallback")
- Modify: `internal/head/agent/execute_phases_steps.go` (update the `runSteps` + `stepToAction` doc comments)

**Interfaces:** none (docs/comments only).

**Reviewer note (controller):** small doc/comment task = inline implementation by the controller, covered by the opus whole-branch final review.

- [ ] **Step 1: Add the docs subsection**

In `cerberus-docs/executors/websocket.md`, immediately AFTER the "Deterministic multi-step cases (Steps)" subsection and BEFORE "### M0 fallback", insert:

```markdown
### Multi-connection orchestration

A `Steps` case may cite more than one `connection_id` (and more than one `role`).
Each distinct id is a distinct connection: the executor's connection table is
keyed by `<caseID>:<connectionID>`, and `runSteps` runs every step under the same
case context, so steps that name different ids open and use separate sockets
within one case. This expresses cross-socket relay scenarios — e.g. connect a
`web` client and a `bridge` client to the same `/ws/{userId}` endpoint, send from
`web`, and receive the broker-relayed reply on `web` (or `bridge`).

The relay is transparent to the executor: `ws_receive` matches by `type` (+ field
asserts) exactly as on a single connection. cerberus does not need to know a
message was relayed. No executor, `runSteps`, `stepToAction`, `TestStep`, or
protocol-schema change is required to orchestrate multiple connections; an
optional `role` handshake (`optional: true`) keeps a connection usable across a
peer-gated welcome that never arrives. See the F1 design spec.
```

- [ ] **Step 2: Update the runSteps / stepToAction comments**

In `internal/head/agent/execute_phases_steps.go`, update the `runSteps` doc comment's first paragraph. Replace:

```go
// runSteps executes a deterministic multi-step WS case: each step runs via the
// shared executor under the case context (caseIDKey already set by executeStep),
// so steps citing the same connection_id share one connection. The first failed
// step short-circuits the case. The decisive verdict is the final ws_receive
// assert; a completed chain is a real upgraded exchange for the Examiner.
```

with:

```go
// runSteps executes a deterministic multi-step WS case: each step runs via the
// shared executor under the case context (caseIDKey already set by executeStep).
// Steps citing the SAME connection_id share one connection; steps citing
// DIFFERENT connection_ids open distinct connections in the same case (the table
// is keyed <caseID>:<connectionID>), enabling multi-connection / cross-socket
// relay orchestration. The first failed step short-circuits the case. The
// decisive verdict is the final ws_receive assert; a completed chain is a real
// upgraded exchange for the Examiner.
```

And in the `stepToAction` doc comment, replace `// shared executor already dispatches. The connect step dials tc.Target; role` with `// shared executor already dispatches. Every step carries its own connection_id, so a case may address several connections. The connect step dials tc.Target; role`.

- [ ] **Step 3: Verify make check is green**

Run: `make check`
Expected: fmt + lint + test `-race` all pass (the integration test is excluded by the build tag).

- [ ] **Step 4: Commit**

```bash
git add cerberus-docs/executors/websocket.md internal/head/agent/execute_phases_steps.go
git commit -m "docs(ws): document multi-connection Steps orchestration"
```

---

## Post-implementation (controller)

- [ ] **Whole-branch final review (opus):** review `main..feat/ws-f1-multi-connection`. Verify: zero production-logic change (only tests + comments + docs); the relay test is race-clean and genuinely proves 2 distinct connections + optional-handshake-survival; the integration test is correctly build-tagged out of `make test`; `make check` green; all constraints (author, no Co-Authored-By, English, cerberus-docs, coder/websocket v1.8.14, no new deps) met.
- [ ] **Finish the branch:** via superpowers:finishing-a-development-branch — local ff-merge to main + `make check` + delete branch. DO NOT push (user pushes explicitly).
- [ ] **Update memory + ledger:** refresh `ws-realtime-engine-roadmap` + `ws-dogfood-tier1-checkpoint` (F1 done: capability proven + dogfood findings), append the F1 entry to `.superpowers/sdd/progress.md`.
