# WS Multi-Connection / Cross-Socket Orchestration (F1) — Design

Status: Design (autonomous; chosen 2026-07-23 as the D-gap unlock for multi-party
relay protocols such as `open-agents`).
Trigger: the Tier-2 dogfood (`cerberus-docs/technical/dogfood/2026-07-23-ws-tier2-open-agents.md`)
found that a web↔bridge exchange relayed through a `UserRoom` Durable Object needs
TWO sockets; C's `Steps` model was validated only for single-socket RPC. F1 is the
natural extension of `Steps` past one connection.

## Goal

Let cerberus orchestrate a multi-connection WebSocket scenario within ONE test
case — e.g. connect a `web` client and a `bridge` client to the same endpoint,
send from `web`, and receive the relayed reply on `web` (or `bridge`) — so it can
test real multi-party relay protocols end-to-end.

## Key finding: multi-connection is already mechanically supported

Reading `executeStep` (`execute_phases.go`), `runSteps`/`stepToAction`
(`execute_phases_steps.go`), and the executor (`websocket.go`) shows that a
`TestCase` whose `Steps` cite DIFFERENT `connection_id`s already lands on distinct
connections — with **zero production-code change**:

- `executeStep` sets `caseIDKey = tc.ID` once and, when `len(Steps) > 0`, dispatches
  to `runSteps` (Phase 0). No per-case single-connection setup; no single-conn
  assumption.
- `runSteps` iterates `tc.Steps` and runs each via `r.executor.Execute(se.ctx, action)`
  under the shared case context. It threads no connection state; each step is
  independent.
- `stepToAction` maps each step to a typed WS action carrying that step's
  `ConnectionID`/`Role`. `ws_connect` dials `tc.Target`; `ws_send`/`ws_receive`/
  `ws_disconnect` address a connection by id.
- The executor keys its connection table by `<caseID>:<connectionID>`. Two steps
  with different ids in the same case therefore create two table entries — two
  independent connections, each with its own read pump — namespaced together so
  they share the case lifetime.
- Case-scoped cleanup holds: when the case context is cancelled (case end, or
  short-circuit on a failed step), every connection's `<-ctx.Done()` goroutine
  closes its socket. A failed step tears down all of the case's connections,
  which is correct (the case is over).

The relay itself is transparent to the executor: `doReceive` matches by `type`
(+ field asserts) exactly as today. cerberus does not need to know a message was
relayed; it connects two sockets and receives on whichever it is told to.

So F1 is NOT "implement multi-connection." It is: **prove, lock with tests,
dogfood on real traffic, and document** a capability that is already present.

## Forks resolved

1. **Scenario abstraction vs Steps extension → Steps extension.** `TestStep`
   already carries per-step `ConnectionID` and `Role`; `runSteps` has no
   single-conn assumption. A new `Scenario` abstraction is redundant and would
   violate the lean/no-evaluator constraint.
2. **How relay messages match → unchanged.** Match by `type` + dotted-path
   asserts (`matchType` + `checkAsserts`). The relay is transparent.
3. **Where the relay flow comes from → defer Scout generation.** This unit proves
   + dogfoods the capability with a HAND-AUTHORED multi-connection `Steps` case.
   Scout auto-generation of relay flows (LLM plan-time, or deterministic from a
   richer protocol) is deferred to a later unit, its direction informed by what
   the dogfood surfaces. (The relay's type-transform — web sends `session:start`,
   web receives `session:created` — and its ordering/conditional-handshake are
   genuinely an understanding problem; baking them into a deterministic schema is
   M3-3-adjacent blank-page cost.)
4. **Dynamic URL `/ws/{userId}` (F3) → deferred.** This unit bakes the userId
   into `svc.URL` / `TestCase.Target` so `web` and `bridge` share one path.

## Design

### Deliverable 1 — capability test (in `make test`, deterministic, `-race`)

A new in-process relay test server plus a `TestRunStepsMultiConnection` that proves
the capability end-to-end against a live in-process `coder/websocket` server.

**`newWSRelayServer(t)` (new helper):** an `httptest` server on `/ws` that models
the relay. It keeps a small hub — a `map[string]*websocket.Conn` keyed by the
connection's role, read from `r.URL.Query().Get("type")` and guarded by a mutex.
Each accepted connection registers itself, then runs a read loop that copies every
received frame to the OTHER registered connection (pure byte forwarding). This is
new shared-state test infra (the existing `newWSTestServer` hands each conn to an
isolated handler); it must be race-clean, mirroring the existing WS test patterns.
No conditional `devices:sync` push — the unit test does not need open-agents'
peer-gated behavior (that is the integration test's job).

**`TestRunStepsMultiConnection`:** a protocol with json framing, two roles
(`web` carrying a discriminator param `type=web` and an OPTIONAL handshake
awaiting `devices:sync`; `bridge` carrying `type=bridge`), wired via
`newStepExecutionWithIdx`. The case:

```
1. ws_connect  c-web    role=web      (optional handshake awaits devices:sync;
                                       none arrives -> times out -> OK, conn ALIVE)
2. ws_connect  c-bridge role=bridge
3. ws_send     c-web    {"type":"echo:web"}        -> relay forwards to bridge
4. ws_receive  c-bridge type=echo:web              (web->bridge relay)
5. ws_send     c-bridge {"type":"echo:bridge"}     -> relay forwards to web
6. ws_receive  c-web    type=echo:bridge           (bridge->web relay)
```

Assertions: `StepPassed`; server-side accept counter `== 2` (two distinct
connections in one case); `len(Evidence) == 6` (one per step). Step 6 receiving on
`c-web` proves three things at once — (a) two connections coexist in one case,
(b) bidirectional cross-socket relay, (c) the `web` connection survived its
optional-handshake timeout and is still usable (the F2 + read-pump headline,
exercised across connections).

The optional handshake adds ~1 s to the case (step 1 blocks for the declared
`RoleHandshake.Timeout`, minimum 1 s). Accepted for the deterministic proof.

### Deliverable 2 — integration test (`//go:build integration`, open-agents dogfood)

A build-tagged test `TestRunStepsMultiConnectionOpenAgents` that runs cerberus's
own `runSteps` against the real open-agents target, proving the capability on live
traffic. It is a DOGFOOD: hard assertions are capability-level; exact-protocol
matching is best-effort and discovered at run time.

**Isolation:** the `//go:build integration` file is excluded from `make test`
(`go test -v -race ./...` passes no `-tags`). Run explicitly with
`go test -tags integration ./internal/head/agent/`. `t.Skip` if
`localhost:8989` is unreachable so it never fails offline.

**Bring-up (documented, not automated by the test):** in the sibling repo,
`fnm use 22 && cd apps/api && npm run dev` (port 8989). The test does not start
or stop the server.

**Auth/userId wiring (grounded in open-agents source):**
- `POST http://localhost:8989/api/dev/setup` with `{}` → response `config.userId`
  and `config.deviceToken` (`token_<uuid>`).
- `web` connects with `?type=web&token=demo_token` — `demo_token` is accepted in
  development regardless of userId (`isValidWebToken`, `room.ts:631`).
- `bridge` connects with `?type=bridge&token=<deviceToken>` — format- and
  DB-validated.
- Both connect to `/ws/<config.userId>` (the SAME path → the SAME `UserRoom` DO →
  relay).

**Wiring:** `WSProtocolIndex` maps `localhost:8989` to a Protocol
(auth `query`/`token`; roles `web{type:web, credential_ref:web-actor,
handshake{await devices:sync, optional:true, timeout:2}}`,
`bridge{type:bridge, credential_ref:bridge-actor}`).
`ActorTokens: {web-actor: demo_token, bridge-actor: deviceToken}`.
`TestCase.Target = ws://localhost:8989/ws/<userId>`.

**Steps (provisional — exact types discovered at run time):**
```
1. ws_connect  c-web    role=web
2. ws_connect  c-bridge role=bridge
3. ws_receive  c-web    type=devices:sync     (DO pushes sync once bridge joins)
4. ws_send     c-web    {"type":"session:start", ...}
5. ws_receive  c-web    type=session:created  (relayed reply; type best-effort)
```

**Assertions:** HARD — step 1 and step 2 both `Success` (cerberus established two
real sockets to the same DO). The step-3 `devices:sync` receive is the relay
signal: assert it matches if the protocol fires it; otherwise downgrade to
`t.Log` + soft. Steps 4–5 are best-effort probes; `t.Log` every observed frame for
the findings doc. A mismatch there is a dogfood finding about open-agents, NOT a
cerberus regression (the deterministic unit test is the mechanical proof).

### Deliverable 3 — docs

`websocket.md`: add a "Multi-connection orchestration" subsection under the
`Steps` material. State that a `Steps` case may cite multiple `connection_id`s
(and roles) — each distinct id is a distinct connection namespaced by the case —
so cross-socket relay scenarios (web↔bridge through a broker) are expressible with
no executor change; the relay is transparent (match by `type`). Cross-link this
spec. Update the `runSteps`/`stepToAction` doc comments to note that DIFFERENT
connection_ids yield distinct connections (the current comments speak only of
shared ids).

## Constraints

- Go 1.25, pure-Go (no CGo); `coder/websocket v1.8.14` only (forbidden: nhooyr,
  gorilla). The relay test server uses `coder/websocket.Accept` like the existing
  helpers.
- No new dependencies; no expression evaluator; no protocol-schema change (the
  existing roles + optional handshake suffice).
- Author `binoctal <binoctal@gmail.com>`; no `Co-Authored-By`; English comments
  and commit messages; docs only in `cerberus-docs/`.
- `make check` (fmt + lint + test `-race`) green; the integration test is
  build-tagged out of it.
- Determinism: sort any map iteration used in error/reporting paths.
- Never `pkill -f <pattern>` from a bash whose argv contains it.

## Testing

- Existing WS + Steps tests stay green (backwards-compat; zero prod change).
- `TestRunStepsMultiConnection` proves the capability deterministically under
  `-race` (two connections, bidirectional relay, optional-handshake survival).
- `TestRunStepsMultiConnectionOpenAgents` (`//go:build integration`) dogfoods on
  real open-agents traffic; `t.Skip` offline; capability-level hard asserts +
  best-effort protocol probes.
- `make check` green; the relay server + multi-pump case exercised under `-race`.

## Non-goals

- Scout auto-generation of relay flows (LLM plan-time or deterministic-from-
  protocol) — deferred; informed by this dogfood.
- F3 dynamic URL/path-param injection from the auth result (userId baked in here).
- F4 message-batching / type-alias matching (`session:output` ↔ `session:output-batch`).
- Per-step URL override on `ws_connect` (web and bridge share `tc.Target`; a
  per-step URL is YAGNI until a relay needs two distinct endpoints).
- Any production-code change to the executor, `runSteps`, `stepToAction`,
  `TestStep`, or the protocol schema. (Doc-comment updates only.)

## Open questions (resolve in the plan)

1. Relay-server hub: drop frames whose peer is not yet connected (simplest), vs.
   buffer one frame — lean: drop (the test sends only after both connect).
2. Integration-test step-3 `devices:sync`: hard-assert vs soft — decide during the
   dogfood run once the real behavior is observed; default hard, downgrade if
   flaky.
3. Whether to also assert, in the unit test, that a SECOND connect with the SAME
   `connection_id` reuses/overwrites one entry (edge documentation) — lean: no,
   out of scope (distinct ids is the F1 contract).
