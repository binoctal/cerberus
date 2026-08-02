# open-agents Full Relay Coverage — Design — 2026-08-02

Bring cerberus's open-agents integration coverage from "one dogfood case" to the
full relay/routing/callback surface of `apps/realtime/src/room.ts` (running inside
the `apps/api` worker). Adds a capture-server capability for HTTP side-effect
assertions (gap E). Implementation is Go `//go:build integration` tests, hybrid:
authored as test harness now, factored for a future uplift to a product step.

## Background (verified against real code)

- `UserRoom` DO runs **inside the `apps/api` worker** in local dev
  (`apps/api/src/worker.ts:28` imports `room.ts`, `:37` exports `UserRoom`; api
  `wrangler.toml` carries the DO binding). The standalone `apps/realtime/`
  wrangler is a deployment variant, not what `npm run dev` runs.
- open-agents shards by `userId`: a `web` client and a `bridge` device connect to
  the same `/ws/<userId>` and land in one `UserRoom` DO, which relays between them.
- The existing case `TestRunStepsMultiConnectionOpenAgents` covers only: both
  connects, the `device:online` peer-join relay, and a best-effort (currently
  failing) `session:start`. Gaps A–E are documented in
  `cerberus-docs/technical/dogfood/2026-08-02-cerberus-openagents-mapping.md`.

## Decisions (locked)

1. **Gap E mechanism: capture server.** cerberus runs a local HTTP capture
   endpoint; open-agents' `API_BASE_URL` is pointed at it; the test asserts the
   `notifyOrchestrator` callback (`room.ts:326-338`) arrived. Black-box, decoupled
   from open-agents internals, and naturally generalizes to any outbound callback.
2. **Form: hybrid.** Capture server lives in a `_test.go` file this round; no
   changes to `prompts.go`, rule engine, or action-type registry. The capture
   implementation is kept dependency-free and self-contained so it can be lifted
   to a product step in a later epic without rewrite.
3. **Coverage shape: representative + parametrized.** One table-driven test per
   gap (≈5 test functions); each row is one message type. Adding a type is a
   one-line table change.

## Components

### 1. Capture server — `internal/head/agent/captureserver_test.go`

`//go:build integration`. Pure, no open-agents coupling.

```go
type capturedPOST struct {
    Path string
    Body string
    At   time.Time
}

type captureServer struct {
    base string                 // http://127.0.0.1:<port>
    mu    sync.Mutex
    posts []capturedPOST
    srv   *http.Server
}

// newCaptureServer binds a fixed port. If the port is taken (another capture
// server, or nothing to do with us), t.Skipf with a clear prerequisite message
// rather than fail — matches the existing reachable()-skip idiom.
func newCaptureServer(t *testing.T, port int) *captureServer

// awaitPOST polls until a matching POST is recorded or timeout elapses.
// bodySubstring is matched against the captured body (nil/"" matches any).
// Returns the capture and true on match; (zero, false) on timeout.
func (c *captureServer) awaitPOST(path, bodySubstring string, timeout time.Duration) (capturedPOST, bool)

// reset drops recorded POSTs so each subtest starts clean.
func (c *captureServer) reset()
```

Lifecycle: one server per `TestMain`/suite OR per `TestOrchestratorCallback`
(cheaper to keep one for the suite and `reset()` per subtest). Fixed port
**9099** (see gap E prerequisites).

### 2. Shared fixture — collapse the per-test boilerplate

Factor the existing test's `devSetup` + Protocol/Role assembly + `WSProtocolIndex`
into a helper. The existing `TestRunStepsMultiConnectionOpenAgents` is refactored
to call it (dedup; no behavior change).

```go
type oaFixture struct {
    se       *stepExecution   // stepExecution wired with the open-agents wsIdx
    userId   string
    deviceId string
    capture  *captureServer   // non-nil only when withCapture=true
}

// setupOpenAgents provisions user+device via /api/dev/setup and wires the
// protocol (web await = device:online, optional). withCapture also starts the
// capture server on port 9099 (skip if unavailable).
func setupOpenAgents(t *testing.T, withCapture bool) oaFixture
```

### 3. Five parametrized tests

All `//go:build integration`, all skip-if-unreachable via `reachable("http://localhost:8989")`.

| Test | Gap | Shape |
|---|---|---|
| `TestBridgeToWebRelay` | A | table `{type, message}`; per row: connect web+bridge → `ws_send c-bridge message` → `ws_receive c-web type` (+ optional `assert`) |
| `TestWebToBridgeRouting` | B | table `{type, message}`; per row: `ws_send c-web {type, payload:{deviceId}}` → `ws_receive c-bridge type`. Includes the `session:start`→bridge replies `session:created`→web receives round trip |
| `TestLifecycleSignals` | C | three sub-cases (not a uniform table): `device:offline` on bridge disconnect; `sendToBridge` silent-drop on unknown deviceId; fan-out to ≥2 web clients |
| `TestAuthErrorPaths` | D | table `{name, connectOverride, expectErrContains}` where `connectOverride` mutates the connect params (invalid `type`; bridge without `deviceId`; missing `token`; wrong bridge `token`); assert connect rejected (`OK:false`) + best-effort status substring in `Err` |
| `TestOrchestratorCallback` | E | table `{type, message}` over the three `notifyOrchestrator` triggers; per row: bridge sends → `capture.awaitPOST("/api/multiagent/internal/orchestrator/event", ...)` |

**Hard-assert vs best-effort (unchanged philosophy):**
- HARD (capability): connects succeed, a relayed frame of the expected type
  arrives, the callback POST is captured. These fail the test.
- BEST-EFFORT (protocol detail): exact payload-field matching via `assert`, and
  the 4xx status substring in gap D. A mismatch is a logged dogfood finding, not
  a red test.

### 4. Message-type tables (extracted from `room.ts:178-262`)

**Gap A — Bridge→Web** (`room.ts:178-220`, relayed only when `meta.type==='bridge'`):
`encrypted`, `session:created`, `session:started`, `session:output`, `session:stopped`,
`session:error`, `session:message`, `session:status`, `chat:response`, `chat:thought`,
`chat:permission`, `permission:request`, `acp:status`, `acp:output`, `acp:tool_call`,
`acp:tool_result`, `agent:status`, `tool:call`, `session:usage`,
`multiagent:task_started`, `multiagent:task_progress`, `multiagent:task_completed`,
`multiagent:task_failed`, `multiagent:job_completed`, `multiagent:task_result`,
`multiagent:task_error`, `prompts:synced`, `mcp:synced`, `mcp:list_response`,
`config:synced`, `rules:synced`, `storage:synced`.

**Gap B — Web→Bridge** (`room.ts:224-252`, routed only when `meta.type==='web'` AND
`payload.deviceId` present): `session:start`, `session:send`, `session:stop`,
`session:resize`, `chat:send`, `permission:response`, `control:takeover`,
`config:sync`, `rules:sync`, `storage:sync`, `prompts:sync`, `mcp:sync`,
`mcp:list`, `multiagent:start_job`, `multiagent:pause_job`, `multiagent:cancel_job`,
`multiagent:start_task`, `multiagent:task_assign`, `acp:query_status`.

**Gap C — Lifecycle:** `device:offline` (`room.ts:154-160`, on bridge disconnect);
the silent-drop path of `sendToBridge` (`room.ts:295`, unknown deviceId);
`broadcastToWeb` fan-out (`room.ts:269`).

**Gap D — Auth/error** (`room.ts:49,53,57,63` + `worker.ts:365`): missing/invalid
`type` → 400; bridge without `deviceId` → 400; missing `token` → 401; bad bridge
token → 401 (Worker DB check).

**Gap E — notifyOrchestrator triggers** (`room.ts:217`): `multiagent:task_result`,
`multiagent:task_error`, `multiagent:task_progress`.

## Gap E prerequisites (corrected)

`room.ts:329` returns early ("API_BASE_URL not set") when the var is absent, so by
default the callback never fires. To exercise gap E, open-agents must run with the
DO's env pointing at the capture server. **wrangler does not read shell env
prefixes** (`API_BASE_URL=... npm run dev` does NOT work). Use one of:

- add `API_BASE_URL = "http://127.0.0.1:9099"` to `apps/api/.dev.vars` (file
  already exists), or
- run `wrangler dev --var API_BASE_URL:http://127.0.0.1:9099 --port 8989`.

`TestOrchestratorCallback` skips (not fails) if port 9099 cannot be bound or no
callback arrives within timeout, logging the prerequisite. Gaps A–D do not depend
on this and run regardless.

## Verified non-issues

- **Handshake frame drain.** `readMatching` (`websocket.go`) drains buffered
  non-matching frames (a stale `device:online`) into `seen` and keeps waiting for
  the target type, so the first `ws_receive` in gap A is not blocked by the
  peer-join frame. No special handling needed.
- **DO env reachability.** Because the DO runs in the api worker, the api
  worker's `API_BASE_URL` reaches `room.ts`. No cross-worker config required.

## Out of scope (YAGNI)

- **No product-step uplift this round.** The capture server stays in `_test.go`;
  `prompts.go` / rule engine / `types` registry are untouched. Uplift is a later
  epic.
- **No real bridge process.** The bridge side is driven by `ws_send c-bridge`
  injecting messages; open-agents' `bridge/` process is not started. We test the
  DO's relay/routing, not bridge business logic.
- **No unit counterparts for the new cases.** `TestRunStepsMultiConnection`
  (in-process) remains the mechanical proof; the new cases assert open-agents
  protocol specifics that cannot be reproduced in-process.
- **No `/broadcast` internal HTTP→WS endpoint** (`room.ts:26-35`). That needs
  cerberus to POST into the DO — a different capability class, deferred.

## Open question for the plan

The plan must pick: capture server **per-suite** (one `TestMain`, `reset()` per
subtest) vs **per-test** (simpler, a few extra `net/http` starts). Lean:
per-suite for `TestOrchestratorCallback`, none at all for A–D.
