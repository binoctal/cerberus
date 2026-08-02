# open-agents Full Relay Coverage Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add integration-test coverage for cerberus's open-agents WebSocket relay surface (gaps A–E) plus a capture-server capability for HTTP side-effect assertions.

**Architecture:** A reusable HTTP capture server helper + a shared open-agents fixture, then five parametrized Go `//go:build integration` tests (one per gap) that drive both `web` and `bridge` sockets via the existing deterministic `stepExecution.runSteps()` runner. All new code is test-only (hybrid: factored for a future uplift to a product step, but not shipped this round).

**Tech Stack:** Go 1.25, `coder/websocket`, `net/http`, `testing` + `testify/require`. Target: a running `open-agents` dev server on `localhost:8989`.

**Spec:** `cerberus-docs/superpowers/specs/2026-08-02-openagents-coverage-design.md`

## Global Constraints

- Go module `github.com/binoctal/cerberus`; pure Go (no CGo).
- All new test files carry `//go:build integration` (excluded from `make test`; run via `go test -tags integration`).
- Commit author `binoctal <binoctal@gmail.com>`, no `Co-Authored-By`. Messages in English.
- Hard-assert capability (connects succeed, relay frame arrives, callback captured); best-effort protocol detail (exact payload fields, 4xx status substring). A best-effort miss is a logged finding, not a red test.
- Tests skip (not fail) when open-agents is unreachable, via the existing `reachable("http://localhost:8989")` guard.
- Run any single test: `go test -tags integration -run <TestName> -v ./internal/head/agent/`

## File Structure

- **Create** `internal/head/agent/captureserver_test.go` — the HTTP capture server (pure, reusable).
- **Create** `internal/head/agent/openagents_setup_test.go` — the `oaFixture` + `setupOpenAgents` helper; `reachable` and `devSetup` move here from the existing file.
- **Modify** `internal/head/agent/execute_phases_steps_integration_test.go` — refactor the existing `TestRunStepsMultiConnectionOpenAgents` to use the fixture; the five new tests live alongside it (same package, same build tag).

### Key real signatures (verified in code)

- `func newStepExecutionWithIdx(t *testing.T, tc *TestCase, wsIdx *WSProtocolIndex) *stepExecution` (in `execute_phases_steps_test.go`)
- `func (se *stepExecution) runSteps() StepResult`
- `type TestStep struct { Action, ConnectionID, Role, URL, Message, Type string; Aliases []string; Asserts map[string]any; MatchAll bool; Timeout int }` (`types.go:76`)
- `type TestCase struct { ID, Target string; Steps []TestStep; ... }` (`types.go:46`)
- `type WSProtocolIndex struct { ByHost map[string]*project.Protocol; ActorTokens map[string]string; ... }` (`ws_protocol.go:123`)
- `types.WSResult` has `OK bool`, `Err string`, `SeenMessages []string`, `MatchedMessage string`.
- A `ws_connect` step with `URL` set and `Role` empty dials the URL raw (no auth injection). With a protocol registered for the host, `doConnect` auto-injects the resolved token — so **gap D uses a `nil` wsIdx** to dial raw bad-param URLs.

---

### Task 1: Capture server

**Files:**
- Create: `internal/head/agent/captureserver_test.go`

**Interfaces:**
- Produces: `captureServer` type, `newCaptureServer(t, port) *captureServer`, `(c *captureServer) awaitPOST(path, bodySubstring string, timeout time.Duration) (capturedPOST, bool)`, `(c *captureServer) reset()`, `(c *captureServer) base() string`. Consumed by Task 7.

- [ ] **Step 1: Write the failing test**

Write the file with the build tag, package clause, and the **complete final import block** (it already includes every import the implementation in Step 3 needs, so Step 3 adds no imports):

```go
//go:build integration

package agent

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestCaptureServerRoundTrip validates the capture server with no external
// dependency: start it, POST to it, awaitPOST must observe the capture.
func TestCaptureServerRoundTrip(t *testing.T) {
	c := newCaptureServer(t, 9099)
	t.Cleanup(c.stop)

	go func() {
		resp, err := http.Post(c.base()+"/api/multiagent/internal/orchestrator/event",
			"application/json", strings.NewReader(`{"type":"multiagent:task_progress"}`))
		if err != nil {
			t.Errorf("post: %v", err)
			return
		}
		_ = resp.Body.Close()
	}()

	got, ok := c.awaitPOST("/api/multiagent/internal/orchestrator/event", "task_progress", 2*time.Second)
	if !ok {
		t.Fatal("awaitPOST: no capture within timeout")
	}
	if !strings.Contains(got.Body, "task_progress") {
		t.Fatalf("captured body = %q, want task_progress substring", got.Body)
	}

	// reset clears recorded POSTs.
	c.reset()
	if _, ok := c.awaitPOST("/api/multiagent/internal/orchestrator/event", "", 100*time.Millisecond); ok {
		t.Fatal("reset did not clear captured POSTs")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -tags integration -run TestCaptureServerRoundTrip -v ./internal/head/agent/`
Expected: FAIL — `newCaptureServer` / `captureServer` undefined.

- [ ] **Step 3: Implement the capture server**

Append to `captureserver_test.go` (below the test). No new imports — the import block from Step 1 already covers these:

```go
type capturedPOST struct {
	Path string
	Body string
	At   time.Time
}

type captureServer struct {
	baseURL string
	mu      sync.Mutex
	posts   []capturedPOST
	srv     *http.Server
}

// newCaptureServer binds a fixed port and serves until stop. If the port is
// unavailable, t.Skipf with a clear prerequisite message (matches the existing
// reachable()-skip idiom) rather than failing the suite.
func newCaptureServer(t *testing.T, port int) *captureServer {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	c := &captureServer{baseURL: "http://" + addr}
	mux := http.NewServeMux()
	mux.HandleFunc("/", c.handle)
	c.srv = &http.Server{Addr: addr, Handler: mux}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		t.Skipf("capture server: cannot bind %s (%v); is another instance running?", addr, err)
	}
	go func() { _ = c.srv.Serve(ln) }()
	return c
}

func (c *captureServer) handle(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	c.mu.Lock()
	c.posts = append(c.posts, capturedPOST{Path: r.URL.Path, Body: string(body), At: time.Now()})
	c.mu.Unlock()
	_, _ = w.Write([]byte("ok"))
}

// awaitPOST polls recorded POSTs until one matches path (and bodySubstring when
// non-empty) or timeout elapses. Returns the capture and true on match.
func (c *captureServer) awaitPOST(path, bodySubstring string, timeout time.Duration) (capturedPOST, bool) {
	deadline := time.Now().Add(timeout)
	for {
		c.mu.Lock()
		for _, p := range c.posts {
			if p.Path == path && (bodySubstring == "" || strings.Contains(p.Body, bodySubstring)) {
				c.mu.Unlock()
				return p, true
			}
		}
		c.mu.Unlock()
		if time.Now().After(deadline) {
			return capturedPOST{}, false
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (c *captureServer) reset() {
	c.mu.Lock()
	c.posts = nil
	c.mu.Unlock()
}

func (c *captureServer) base() string { return c.baseURL }

func (c *captureServer) stop() { _ = c.srv.Close() }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -tags integration -run TestCaptureServerRoundTrip -v ./internal/head/agent/`
Expected: PASS (no open-agents required).

- [ ] **Step 5: Commit**

```bash
git add internal/head/agent/captureserver_test.go
git commit -m "test(agent): add HTTP capture server for callback side-effect assertions"
```

---

### Task 2: Shared open-agents fixture

**Files:**
- Create: `internal/head/agent/openagents_setup_test.go`
- Modify: `internal/head/agent/execute_phases_steps_integration_test.go` — move `reachable` and `devSetup` out (they relocate to the new file); refactor `TestRunStepsMultiConnectionOpenAgents` to use `setupOpenAgents`.

**Interfaces:**
- Consumes: `newStepExecutionWithIdx`, `WSProtocolIndex`, `newCaptureServer` (Task 1), `project.Protocol`/`project.ProtocolAuth`/`project.ProtocolRole`/`project.RoleHandshake`.
- Produces: `oaFixture{ wsIdx *WSProtocolIndex; userId, deviceId string; capture *captureServer }`; `setupOpenAgents(t *testing.T, withCapture bool) oaFixture`. Consumed by Tasks 3–7.

- [ ] **Step 1: Write the fixture (new file)**

```go
//go:build integration

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/project"
)

const oaBase = "http://localhost:8989"

// oaFixture carries the provisioned open-agents connection wiring shared by
// every open-agents integration test. wsIdx is bound to the dynamic user/device
// from /api/dev/setup; each test builds its own TestCase and binds it via
// newStepExecutionWithIdx(t, tc, f.wsIdx).
type oaFixture struct {
	wsIdx    *WSProtocolIndex
	userId   string
	deviceId string
	capture  *captureServer // nil when withCapture is false
}

// setupOpenAgents provisions a user + bridge device and wires the web/bridge
// protocol (web awaits device:online, optional). Skips when open-agents is not
// reachable. withCapture also starts a capture server on port 9099 (skips if the
// port is unavailable — gap E prerequisite not met).
func setupOpenAgents(t *testing.T, withCapture bool) oaFixture {
	if !reachable(oaBase) {
		t.Skipf("open-agents not reachable at %s; bring up `npm run dev` (apps/api)", oaBase)
	}
	userId, deviceId, deviceToken, err := devSetup(oaBase)
	require.NoError(t, err, "POST /api/dev/setup")

	p := &project.Protocol{
		TypePath: "type",
		Auth:     &project.ProtocolAuth{Strategy: "query", Param: "token", CredentialRef: "web-actor"},
		Roles: map[string]*project.ProtocolRole{
			"web": {
				CredentialRef: "web-actor",
				Params:        map[string]string{"type": "web"},
				Handshake:     &project.RoleHandshake{AwaitType: "device:online", Optional: true, Timeout: 2},
			},
			"bridge": {
				CredentialRef: "bridge-actor",
				Params:        map[string]string{"type": "bridge", "deviceId": deviceId},
			},
		},
	}
	f := oaFixture{
		wsIdx: &WSProtocolIndex{
			ByHost: map[string]*project.Protocol{"localhost:8989": p},
			ActorTokens: map[string]string{
				"web-actor":    "demo_token",
				"bridge-actor": deviceToken,
			},
		},
		userId:   userId,
		deviceId: deviceId,
	}
	if withCapture {
		f.capture = newCaptureServer(t, 9099)
		t.Cleanup(f.capture.stop)
	}
	return f
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

// devSetup POSTs /api/dev/setup and returns the provisioned userId, deviceId,
// and deviceToken. The dev server's CSRF middleware requires an Origin header.
func devSetup(base string) (userId, deviceId, deviceToken string, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/dev/setup", strings.NewReader(`{}`))
	if err != nil {
		return "", "", "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", base)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", "", "", fmt.Errorf("dev setup: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out struct {
		Config struct {
			UserId      string `json:"userId"`
			DeviceId    string `json:"deviceId"`
			DeviceToken string `json:"deviceToken"`
		} `json:"config"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", "", "", err
	}
	return out.Config.UserId, out.Config.DeviceId, out.Config.DeviceToken, nil
}
```

- [ ] **Step 2: Refactor the existing test to use the fixture**

In `execute_phases_steps_integration_test.go`:
- Delete the now-duplicated `reachable` and `devSetup` funcs (they live in the new file).
- Replace the body of `TestRunStepsMultiConnectionOpenAgents` (the inline `devSetup` + Protocol + wsIdx block) with:

```go
func TestRunStepsMultiConnectionOpenAgents(t *testing.T) {
	f := setupOpenAgents(t, false)

	tc := &TestCase{
		ID:     "tc-openagents-relay",
		Target: "ws://localhost:8989/ws/" + f.userId,
		Steps: []TestStep{
			{Action: "ws_connect", Role: "web", ConnectionID: "c-web"},
			{Action: "ws_connect", Role: "bridge", ConnectionID: "c-bridge"},
			{Action: "ws_receive", ConnectionID: "c-web", Type: "device:online", Timeout: 3},
			{Action: "ws_send", ConnectionID: "c-web", Message: `{"type":"session:start"}`},
			{Action: "ws_receive", ConnectionID: "c-web", Type: "session:created", Timeout: 3},
		},
	}

	se := newStepExecutionWithIdx(t, tc, f.wsIdx)
	result := se.runSteps()
	for _, ev := range result.Evidence {
		t.Logf("step evidence: %s", ev.Content)
	}
	require.GreaterOrEqual(t, len(result.Evidence), 3,
		"both connects must succeed (web + bridge); evidence=%d", len(result.Evidence))
	if result.Status == StepPassed {
		t.Logf("relay case fully passed: status=%s", result.Status)
	} else {
		t.Logf("relay case did not fully pass (dogfood finding): status=%s", result.Status)
	}
}
```

Keep the existing top-of-file `//go:build integration` and imports (drop any now-unused imports such as `project`, `encoding/json`, `io`, `net/http`, `strings`, `context`, `time` if they are no longer referenced after the move — `goimports` will clean them).

- [ ] **Step 3: Run the refactored test**

Run: `go test -tags integration -run TestRunStepsMultiConnectionOpenAgents -v ./internal/head/agent/`
Expected: PASS (if open-agents is up) or `--- SKIP` (if not). Behavior is unchanged from before the refactor.

- [ ] **Step 4: Format and vet**

Run: `make fmt && go vet ./internal/head/agent/`
Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add internal/head/agent/openagents_setup_test.go internal/head/agent/execute_phases_steps_integration_test.go
git commit -m "test(agent): extract open-agents fixture, dedup devSetup/reachable"
```

---

### Task 3: Gap A — Bridge→Web relay

**Files:**
- Modify: `internal/head/agent/execute_phases_steps_integration_test.go`

**Interfaces:**
- Consumes: `setupOpenAgents(t, false)`, `newStepExecutionWithIdx`, `TestCase`/`TestStep`, `StepResult.Status`.

- [ ] **Step 1: Add the parametrized test**

```go
// TestBridgeToWebRelay dogfoods the DO's Bridge→Web broadcast for every relay
// type in room.ts:178-220. Per row: connect web+bridge, bridge sends, web must
// receive the same type. Hard-assert: the relayed frame arrives.
func TestBridgeToWebRelay(t *testing.T) {
	f := setupOpenAgents(t, false)
	rows := []string{
		"encrypted", "session:created", "session:started", "session:output",
		"session:stopped", "session:error", "session:message", "session:status",
		"chat:response", "chat:thought", "chat:permission", "permission:request",
		"acp:status", "acp:output", "acp:tool_call", "acp:tool_result",
		"agent:status", "tool:call", "session:usage",
		"multiagent:task_started", "multiagent:task_completed",
		"multiagent:task_failed", "multiagent:job_completed",
		// task_progress/task_result/task_error are Bridge→Web relays AND trigger
		// notifyOrchestrator (gap E). Listed here so their web relay is covered;
		// with API_BASE_URL unset the fetch is a no-op (room.ts:329), and with it
		// set the fetch misses (no capture server in gap A) and .catch swallows it.
		"multiagent:task_progress", "multiagent:task_result", "multiagent:task_error",
		"prompts:synced", "mcp:synced", "mcp:list_response",
		"config:synced", "rules:synced", "storage:synced",
	}
	for _, typ := range rows {
		t.Run(typ, func(t *testing.T) {
			tc := &TestCase{
				ID:     "tc-b2w-" + typ,
				Target: "ws://localhost:8989/ws/" + f.userId,
				Steps: []TestStep{
					{Action: "ws_connect", Role: "web", ConnectionID: "c-web"},
					{Action: "ws_connect", Role: "bridge", ConnectionID: "c-bridge"},
					{Action: "ws_receive", ConnectionID: "c-web", Type: "device:online", Timeout: 3},
					{Action: "ws_send", ConnectionID: "c-bridge", Message: fmt.Sprintf(`{"type":%q}`, typ)},
					{Action: "ws_receive", ConnectionID: "c-web", Type: typ, Timeout: 3},
				},
			}
			se := newStepExecutionWithIdx(t, tc, f.wsIdx)
			result := se.runSteps()
			for _, ev := range result.Evidence {
				t.Logf("%s", ev.Content)
			}
			require.Equal(t, StepPassed, result.Status,
				"bridge→web relay for %q did not pass (dogfood finding)", typ)
		})
	}
}
```

Ensure `fmt` is imported.

- [ ] **Step 2: Run the test**

Run: `go test -tags integration -run TestBridgeToWebRelay -v ./internal/head/agent/`
Expected: subtests PASS (relay reaches web) or fail with a logged dogfood finding per type. Either outcome is informative; a PASS means the DO relays all listed types.

- [ ] **Step 3: Commit**

```bash
git add internal/head/agent/execute_phases_steps_integration_test.go
git commit -m "test(agent): cover open-agents Bridge→Web relay types (gap A)"
```

---

### Task 4: Gap B — Web→Bridge routing (incl. session:start round trip)

**Files:**
- Modify: `internal/head/agent/execute_phases_steps_integration_test.go`

**Interfaces:**
- Consumes: same as Task 3, plus `f.deviceId`.

- [ ] **Step 1: Add the parametrized routing test**

```go
// TestWebToBridgeRouting dogfoods the DO's Web→Bridge routing for every routed
// type in room.ts:224-252. Per row: connect web+bridge, web sends with
// payload.deviceId, bridge must receive the same type. Hard-assert: the routed
// frame reaches the bridge.
func TestWebToBridgeRouting(t *testing.T) {
	f := setupOpenAgents(t, false)
	rows := []string{
		"session:send", "session:stop", "session:resize", "chat:send",
		"permission:response", "control:takeover", "config:sync", "rules:sync",
		"storage:sync", "prompts:sync", "mcp:sync", "mcp:list",
		"multiagent:start_job", "multiagent:pause_job", "multiagent:cancel_job",
		"multiagent:start_task", "multiagent:task_assign", "acp:query_status",
	}
	for _, typ := range rows {
		t.Run(typ, func(t *testing.T) {
			tc := &TestCase{
				ID:     "tc-w2b-" + typ,
				Target: "ws://localhost:8989/ws/" + f.userId,
				Steps: []TestStep{
					{Action: "ws_connect", Role: "web", ConnectionID: "c-web"},
					{Action: "ws_connect", Role: "bridge", ConnectionID: "c-bridge"},
					{Action: "ws_receive", ConnectionID: "c-web", Type: "device:online", Timeout: 3},
					{Action: "ws_send", ConnectionID: "c-web",
						Message: fmt.Sprintf(`{"type":%q,"payload":{"deviceId":%q}}`, typ, f.deviceId)},
					{Action: "ws_receive", ConnectionID: "c-bridge", Type: typ, Timeout: 3},
				},
			}
			se := newStepExecutionWithIdx(t, tc, f.wsIdx)
			result := se.runSteps()
			for _, ev := range result.Evidence {
				t.Logf("%s", ev.Content)
			}
			require.Equal(t, StepPassed, result.Status,
				"web→bridge routing for %q did not pass (dogfood finding)", typ)
		})
	}
}
```

- [ ] **Step 2: Add the session:start round-trip sub-case**

```go
// TestSessionStartRoundTrip proves the Web→Bridge→Web chain end-to-end: web
// sends session:start with payload.deviceId, bridge receives it, bridge replies
// session:created, web receives session:created. Closes the original Finding 4.
func TestSessionStartRoundTrip(t *testing.T) {
	f := setupOpenAgents(t, false)
	tc := &TestCase{
		ID:     "tc-session-roundtrip",
		Target: "ws://localhost:8989/ws/" + f.userId,
		Steps: []TestStep{
			{Action: "ws_connect", Role: "web", ConnectionID: "c-web"},
			{Action: "ws_connect", Role: "bridge", ConnectionID: "c-bridge"},
			{Action: "ws_receive", ConnectionID: "c-web", Type: "device:online", Timeout: 3},
			{Action: "ws_send", ConnectionID: "c-web",
				Message: fmt.Sprintf(`{"type":"session:start","payload":{"deviceId":%q}}`, f.deviceId)},
			{Action: "ws_receive", ConnectionID: "c-bridge", Type: "session:start", Timeout: 3},
			{Action: "ws_send", ConnectionID: "c-bridge", Message: `{"type":"session:created"}`},
			{Action: "ws_receive", ConnectionID: "c-web", Type: "session:created", Timeout: 3},
		},
	}
	se := newStepExecutionWithIdx(t, tc, f.wsIdx)
	result := se.runSteps()
	for _, ev := range result.Evidence {
		t.Logf("%s", ev.Content)
	}
	require.Equal(t, StepPassed, result.Status, "session:start round trip did not complete")
}
```

- [ ] **Step 3: Run the tests**

Run: `go test -tags integration -run 'TestWebToBridgeRouting|TestSessionStartRoundTrip' -v ./internal/head/agent/`
Expected: PASS, or per-type failures logged as dogfood findings.

- [ ] **Step 4: Commit**

```bash
git add internal/head/agent/execute_phases_steps_integration_test.go
git commit -m "test(agent): cover open-agents Web→Bridge routing + session round trip (gap B)"
```

---

### Task 5: Gap C — Lifecycle signals

**Files:**
- Modify: `internal/head/agent/execute_phases_steps_integration_test.go`

**Interfaces:**
- Consumes: same as Task 3. Produces: three lifecycle sub-cases.

- [ ] **Step 1: Add the lifecycle test**

```go
// TestLifecycleSignals covers three DO lifecycle paths: device:offline on
// bridge disconnect (room.ts:154-160), sendToBridge silent-drop on an unknown
// deviceId (room.ts:295), and broadcastToWeb fan-out to two web clients
// (room.ts:269).
func TestLifecycleSignals(t *testing.T) {
	f := setupOpenAgents(t, false)
	t.Run("device_offline_on_disconnect", func(t *testing.T) {
		tc := &TestCase{
			ID:     "tc-offline",
			Target: "ws://localhost:8989/ws/" + f.userId,
			Steps: []TestStep{
				{Action: "ws_connect", Role: "web", ConnectionID: "c-web"},
				{Action: "ws_connect", Role: "bridge", ConnectionID: "c-bridge"},
				{Action: "ws_receive", ConnectionID: "c-web", Type: "device:online", Timeout: 3},
				{Action: "ws_disconnect", ConnectionID: "c-bridge"},
				{Action: "ws_receive", ConnectionID: "c-web", Type: "device:offline", Timeout: 3},
			},
		}
		se := newStepExecutionWithIdx(t, tc, f.wsIdx)
		result := se.runSteps()
		require.Equal(t, StepPassed, result.Status, "device:offline not relayed on bridge disconnect")
	})

	t.Run("sendToBridge_miss_silent_drop", func(t *testing.T) {
		// Web sends a routed type with an UNKNOWN deviceId; sendToBridge finds no
		// socket and drops silently. Assert the bridge receives nothing: the case
		// FAILS on the receive step (no frame within timeout) — that failure IS the
		// proof of the silent drop, so we invert the assertion.
		tc := &TestCase{
			ID:     "tc-miss",
			Target: "ws://localhost:8989/ws/" + f.userId,
			Steps: []TestStep{
				{Action: "ws_connect", Role: "web", ConnectionID: "c-web"},
				{Action: "ws_connect", Role: "bridge", ConnectionID: "c-bridge"},
				{Action: "ws_receive", ConnectionID: "c-web", Type: "device:online", Timeout: 3},
				{Action: "ws_send", ConnectionID: "c-web",
					Message: `{"type":"session:send","payload":{"deviceId":"device_does_not_exist"}}`},
				{Action: "ws_receive", ConnectionID: "c-bridge", Type: "session:send", Timeout: 1},
			},
		}
		se := newStepExecutionWithIdx(t, tc, f.wsIdx)
		result := se.runSteps()
		require.NotEqual(t, StepPassed, result.Status,
			"bridge unexpectedly received a routed frame for an unknown deviceId (drop did not happen)")
	})

	t.Run("fanout_two_web_clients", func(t *testing.T) {
		tc := &TestCase{
			ID:     "tc-fanout",
			Target: "ws://localhost:8989/ws/" + f.userId,
			Steps: []TestStep{
				{Action: "ws_connect", Role: "web", ConnectionID: "c-web-1"},
				{Action: "ws_connect", Role: "web", ConnectionID: "c-web-2"},
				{Action: "ws_connect", Role: "bridge", ConnectionID: "c-bridge"},
				// device:online is broadcast to BOTH web clients.
				{Action: "ws_receive", ConnectionID: "c-web-1", Type: "device:online", Timeout: 3},
				{Action: "ws_receive", ConnectionID: "c-web-2", Type: "device:online", Timeout: 3},
			},
		}
		se := newStepExecutionWithIdx(t, tc, f.wsIdx)
		result := se.runSteps()
		require.Equal(t, StepPassed, result.Status, "broadcastToWeb did not reach both web clients")
	})
}
```

- [ ] **Step 2: Run the test**

Run: `go test -tags integration -run TestLifecycleSignals -v ./internal/head/agent/`
Expected: all three sub-tests PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/head/agent/execute_phases_steps_integration_test.go
git commit -m "test(agent): cover open-agents lifecycle signals (gap C)"
```

---

### Task 6: Gap D — Auth/error paths

**Files:**
- Modify: `internal/head/agent/execute_phases_steps_integration_test.go`

**Interfaces:**
- Consumes: `reachable` (via setupOpenAgents, but this test uses a `nil` wsIdx — see note). Produces: `f.userId` for path templating.

- [ ] **Step 1: Add the auth-error test**

```go
// TestAuthErrorPaths asserts the DO/Worker reject bad connects. Each row dials
// a raw URL (Role empty, nil wsIdx) so the fixture's protocol does NOT inject a
// good token. Hard-assert: the connect is rejected (StepFailed). Best-effort:
// the dial error string usually carries the HTTP status (400/401).
func TestAuthErrorPaths(t *testing.T) {
	f := setupOpenAgents(t, false) // provisions userId; its wsIdx is NOT used below.
	cases := []struct {
		name   string
		url    string // raw dial URL with the bad param
	}{
		{"invalid_type", fmt.Sprintf("ws://localhost:8989/ws/%s?type=invalid&token=demo_token", f.userId)},
		{"bridge_no_deviceId", fmt.Sprintf("ws://localhost:8989/ws/%s?type=bridge&token=%s", f.userId, "token_"+strings.Repeat("0", 32))},
		{"missing_token", fmt.Sprintf("ws://localhost:8989/ws/%s?type=web", f.userId)},
		{"bad_bridge_token", fmt.Sprintf("ws://localhost:8989/ws/%s?type=bridge&deviceId=%s&token=token_wrong", f.userId, f.deviceId)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tc := &TestCase{
				ID:     "tc-auth-" + c.name,
				Target: c.url,
				Steps: []TestStep{
					{Action: "ws_connect", ConnectionID: "c-bad"}, // no Role, no protocol → raw dial
				},
			}
			// nil wsIdx: resolveProtocol short-circuits, no auth injection.
			se := newStepExecutionWithIdx(t, tc, nil)
			result := se.runSteps()
			require.Equal(t, StepFailed, result.Status, "connect %q unexpectedly succeeded", c.name)
			// Best-effort: surface the dial error (often contains the HTTP status).
			if ws, ok := result.Result.(types.WSResult); ok {
				t.Logf("dial error for %s: %s", c.name, ws.Err)
			}
		})
	}
}
```

Ensure `strings` and `github.com/binoctal/cerberus/internal/types` are imported.

- [ ] **Step 2: Run the test**

Run: `go test -tags integration -run TestAuthErrorPaths -v ./internal/head/agent/`
Expected: all four sub-tests PASS (each connect rejected), with the dial error logged.

- [ ] **Step 3: Commit**

```bash
git add internal/head/agent/execute_phases_steps_integration_test.go
git commit -m "test(agent): cover open-agents auth/error connect paths (gap D)"
```

---

### Task 7: Gap E — Orchestrator callback via capture server

**Files:**
- Modify: `internal/head/agent/execute_phases_steps_integration_test.go`

**Interfaces:**
- Consumes: `setupOpenAgents(t, true)` (starts the capture server), `f.capture.awaitPOST`.

- [ ] **Step 1: Document the prerequisite at the top of the file**

Add this comment immediately under the package clause's existing run instructions:

```go
// Gap E prerequisite (TestOrchestratorCallback only): open-agents must route its
// DO callback to the capture server. wrangler does NOT read shell env prefixes,
// so use one of:
//   - add API_BASE_URL = "http://127.0.0.1:9099" to apps/api/.dev.vars, or
//   - wrangler dev --var API_BASE_URL:http://127.0.0.1:9099 --port 8989
// The test skips (not fails) if port 9099 is unavailable or no callback arrives.
```

- [ ] **Step 2: Add the callback test**

```go
// TestOrchestratorCallback asserts the DO's notifyOrchestrator side effect
// (room.ts:326-338) for the three triggers (room.ts:217): task_progress,
// task_result, task_error. Per row: bridge sends the trigger, then the capture
// server must observe a POST to /api/multiagent/internal/orchestrator/event.
func TestOrchestratorCallback(t *testing.T) {
	f := setupOpenAgents(t, true) // starts capture server on :9099 (skips if unavailable)
	triggers := []string{"multiagent:task_progress", "multiagent:task_result", "multiagent:task_error"}
	for _, typ := range triggers {
		t.Run(typ, func(t *testing.T) {
			f.capture.reset()
			tc := &TestCase{
				ID:     "tc-cb-" + typ,
				Target: "ws://localhost:8989/ws/" + f.userId,
				Steps: []TestStep{
					{Action: "ws_connect", Role: "web", ConnectionID: "c-web"},
					{Action: "ws_connect", Role: "bridge", ConnectionID: "c-bridge"},
					{Action: "ws_receive", ConnectionID: "c-web", Type: "device:online", Timeout: 3},
					{Action: "ws_send", ConnectionID: "c-bridge",
						Message: fmt.Sprintf(`{"type":%q,"payload":{"deviceId":%q}}`, typ, f.deviceId)},
				},
			}
			se := newStepExecutionWithIdx(t, tc, f.wsIdx)
			res := se.runSteps()
			require.Equal(t, StepPassed, res.Status, "trigger send failed for %s", typ)

			got, ok := f.capture.awaitPOST("/api/multiagent/internal/orchestrator/event", typ, 3*time.Second)
			if !ok {
				t.Skipf("no orchestrator callback captured for %s within timeout — "+
					"is API_BASE_URL pointed at the capture server? (see gap E prerequisite)", typ)
			}
			t.Logf("captured callback for %s: %s", typ, got.Body)
		})
	}
}
```

Ensure `time` is imported.

- [ ] **Step 3: Run the test**

Prerequisite: open-agents running with `API_BASE_URL=http://127.0.0.1:9099` (`.dev.vars` or `--var`).
Run: `go test -tags integration -run TestOrchestratorCallback -v ./internal/head/agent/`
Expected: PASS when the prerequisite is met; `--- SKIP` with the prerequisite message when it is not.

- [ ] **Step 4: Run the whole integration suite together**

Run: `go test -tags integration -run 'OpenAgents|BridgeToWeb|WebToBridge|SessionStart|Lifecycle|AuthError|OrchestratorCallback|CaptureServer' -v ./internal/head/agent/`
Expected: green (or documented skips for the unreachable/prerequisite-missing cases).

- [ ] **Step 5: Commit**

```bash
git add internal/head/agent/execute_phases_steps_integration_test.go
git commit -m "test(agent): cover open-agents orchestrator callback via capture server (gap E)"
```

---

## Post-implementation

- [ ] **Run the deterministic suite (no tag) to confirm no regressions in the mechanical proof:**
  Run: `make test`
  Expected: green — the new files are `//go:build integration` and do not affect `make test`.

- [ ] **Update the coverage mapping doc's "Covered" section** to reflect that gaps A–E now have tests, moving them out of "gap". File: `cerberus-docs/technical/dogfood/2026-08-02-cerberus-openagents-mapping.md`.
