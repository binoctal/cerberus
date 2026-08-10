# open-agents Live Integration — Test Report (2026-08-07)

## Scope (honest)

This report covers two layers of verification against a live open-agents dev server:

1. **Execution surface** — cerberus's WebSocket executor driving open-agents via hand-authored `TestCase` Steps (the `//go:build integration` suite).
2. **End-to-end pipeline** — a real `cerberus run` (Scout→Agent→Examiner, GLM-5.2) generating and judging cases autonomously, including the deterministic relay case that carries the sender-exclusion probe.

## Environment

- cerberus binary: built at HEAD `8b3a0b4` (`make build`), including the sender-exclusion probe and the dimensionGuidance scope change.
- Target: live open-agents `apps/api` dev server, `wrangler dev --port 8989`, Node `v22.22.3` (selected via `fnm`; system default v20 is too old for wrangler).
- Provisioning: `POST /api/dev/setup` per run, yielding a fresh `userId` / `deviceId` / `deviceToken`; web auth via the `demo_token` dev backdoor, bridge via the provisioned device token + `deviceId`.
- Run command: `make integration-openagents` (brings the server up, runs the suite, tears it down).
- These execution-surface tests drive the deterministic `runSteps` executor directly (no LLM). The separate end-to-end `cerberus run` below exercises the GLM-5.2 Scout/Agent/Examiner heads.

## Result — open-agents-specific tests

7 top-level tests, **all PASS, 0 FAIL, 0 SKIP** (run against the live server):

| Test | Time | What it verifies on open-agents |
| --- | --- | --- |
| `TestVocabularyDriven` | 31.80s | Every non-unsupported `message_handled` vocab edge relays end-to-end: ~40 bridge→web message types + ~22 web→bridge types + lifecycle triggers + `flushBatch`/`session:output-batch`. Reads `dogfood/ws-realtime/.cerberus/vocab/open-agents.vocab.yaml`. |
| `TestRunStepsMultiConnectionOpenAgents` | 3.57s | Two real sockets (web + bridge) open to the same `/ws/<userId>` Durable Object. |
| `TestSessionStartRoundTrip` | 0.56s | Web→bridge→web chain: `session:start` (with `deviceId`) → bridge receives → bridge replies `session:created` → web receives. |
| `TestLifecycleSignals` | 7.44s | `device:offline` on bridge disconnect; `sendToBridge` silent-drop on unknown `deviceId`; `broadcastToWeb` fan-out to two web clients. |
| `TestAuthErrorPaths` | 4 subtests | Bad connects rejected: invalid type, bridge-no-deviceId, missing token, bad bridge token. |
| `TestOrchestratorCallback` | 1.58s | `notifyOrchestrator` side-effect (`workflow:task_progress/result/error`) captured via HTTP capture server on :9099 (`.dev.vars` `API_BASE_URL` set). Did NOT skip — callbacks were really captured. |
| `TestSenderExclusionProbeLive` | 2.47s | New probe: relay delivers `device:online` to web (matched=1) AND the bridge's `ExpectAbsent device:online` probe times out (matched=0, 2.0s → pass) — sender excluded. |

Suite context: the broader `make integration-openagents` run compiles the whole `internal/head/agent` package under `-tags=integration` (336 tests, all green). The 336 is the package total; only the 7 above target open-agents.

## What this proves

- cerberus can drive the full open-agents WS relay surface (every protocol edge in the committed vocab) correctly against the real service, deterministically, with no LLM in the loop.
- All five originally-identified coverage gaps (A bridge→web, B web→bridge, C lifecycle, D auth, E orchestrator callback) are covered and green live. Gap-E callbacks are genuinely captured, not skipped.
- The sender-exclusion probe behaves correctly on the real service (sender does not receive its own join signal).

## Out of scope / gaps (what was NOT verified)

1. **`/broadcast` HTTP→WS endpoint (`room.ts:98-108`) is not covered** — Worker→DO→clients push needs an HTTP-into-DO step, a different capability class (documented deferred in the mapping doc). This is the only remaining gap; it requires new capability, not just running something.

## End-to-end `cerberus run` (Scout→Agent→Examiner) — 2026-08-07

Two real runs against live open-agents (GLM-5.2 heads, dynamic provisioned user/device via `/api/dev/setup`):

- **Run 1** (`--goal` relay): 4 pass / 0 fail / 1 skip, ~12K tokens, 1m28s. Relay case `ws-realtime-relay-web-signal-device-online` (deterministic, **carries the sender-exclusion probe**) → execute `pass attempts=1` → Examiner verdict **`pass, correctness 0.93`** (confident, above the 0.9 threshold — not `honest-uncertain`).
- **Run 2** (`--db`, to capture the per-step trace): 5 pass / 7 fail / 4 skip, ~51K tokens, 7m03s. The 7 failures are Scout-LLM free-form cases exhibiting the known Steer WS drift (Finding-3) — expected noise, not regressions. The relay case again → **`pass, correctness 0.92`**.

**Per-step proof the probe ran (Run 2 DB, trace for the relay case):**

- web `ws_receive device:online` → `matched` (the relayed join signal arrived on web).
- bridge `ws_receive device:online` → `success=true, matched=0, 2.001s` — the `ExpectAbsent` probe timed out at its 2 s `probeTimeout`; bridge did NOT receive its own join signal. This step's success is what passes the case.

A clean controlled contrast in the same run: the LLM free-form cases (the 7 failures) also ran `bridge ws_receive device:online` and also timed out, but **without** `expect_absent` → `success=false` → case fail. Same observable (bridge times out, no self-echo), opposite verdict — the `expect_absent` flag is exactly the difference between a correct pass and a false fail. The run's own reflexion memory recorded this: *"Using the expect_absent=true flag for the bridge's ws_receive correctly distinguishes between 'timed out waiting and should have received' vs 'correctly received nothing,' producing a pass."*

This closes the previously-open gaps: Scout autonomously generated the probe-carrying relay case (deterministic `wsRelayCases`), Agent executed it against live open-agents, and the Examiner judged it confidently on the live evidence — end-to-end.

## Vocab freshness — re-verified 2026-08-07

`TestVocabularyDriven` reads the committed `open-agents.vocab.yaml`. Re-extracted the vocab from the current `apps/api/src/realtime/room.ts` via `cerberus protocol vocabulary --dry-run` and diffed: **70 edges, identical content** (only differences were the dry-run header line and a source-path prefix). The committed vocab is NOT stale.

## Confidence

High for both: (a) cerberus's WS executor correctly drives the open-agents relay surface, and (b) a full autonomous `cerberus run` generates, executes, and confidently judges the probe-carrying relay case end-to-end against live open-agents. The sole uncovered item is the `/broadcast` HTTP→WS endpoint (capability gap).

## Reproduce

```
make integration-openagents                         # whole suite
make integration-openagents TEST=TestVocabularyDriven   # the vocab matrix only
```
The target handles server bring-up (fnm 22 → wrangler) and teardown; it reuses an already-running :8989 without killing it.

## SaaS Coverage Authority — live re-verification (2026-08-07, branch feat/saas-coverage-authority)

A `cerberus run` against live open-agents (ws-realtime dogfood, server `wrangler dev --port 8989`) after the coverage-authority change, to confirm the coverage assessment is now objective rather than hallucinated.

Observed log lines:

- Scout contract self-assessment note: `CoverageGate has LineThreshold:0 and BranchThreshold:0 ... not just a single path threshold (PathThreshold:1)` — confirms `assembleContract` deterministically set `PathThreshold=1.0` and dropped the LLM's meaningless line/branch gate for the has-vocab contract (the change's Scout task). The self-assessment LLM grumbling about it is expected noise; the deterministic override is the authority.
- Coverage assessment: `session/coverage.go:79 "coverage assessment" reached:false gaps:64 coverage_pct:0` — the Examiner measured message-edge PATH coverage (`Unit=path`, `Measured=true`) over 64 required edges. No LLM was consulted for the verdict (the path branch is objective).
- Repair round: `"repair round" round:1 fail_eligible:1 coverage_axis:false` — the 64 `Kind:"path"` gaps did NOT drive a coverage-axis repair (only failure-driven repair ran). Path gaps are informational, not repair-loop fuel.

Honest reading of `coverage_pct=0` in THIS run: it is a **measured** 0%, not a hallucination. The run's cases were `tc-001` (HTTP probe, fail), `exec-001/002/003` = `go build`/`go test`/`go vet` (pass — non-WS, Scout read the goal loosely), and `repair-tc-001` (WS, failed auth — `token=web-token` is not the valid `demo_token`; the known Steer WS auth drift, Finding-3). No WS message edge was actually exchanged, so `exercisedEdges` correctly returned 0 → 0/64. Contrast with the PRIOR behavior: the old code called the LLM with no data and it fabricated `reached=false coverage_pct=0` with invented gaps; now the 0 is an objective measurement and the 64 gaps name real, declared-but-unexercised edges.

A `>0` coverage fraction against open-agents needed two fixes, both now landed and verified live:

1. **Receive-driven attribution (the real blocker).** The first `pathCoverage` model required an explicit `ws_send` of T from Rs plus a matched `ws_receive` of T by Rr to count edge (Rs→Rr, T). open-agents is a PUSH protocol: bridge→web signals like `device:online` are server-pushed on peer join with NO explicit `ws_send`, so the model measured a constant 0 for them. `exercisedEdges` was reattributed from the receive side using the vocab — a matched `ws_receive` of T by role Rr exercises the declared edge (From→Rr, T) — so push signals are captured while out-of-band receives of undeclared pairs still count nothing (conservative). Unit-locked by `internal/session` `TestExercisedEdges_PushProtocolReceiveDriven`.

2. **Live proof (commit e46b390).** `TestPathCoverage_LiveOpenAgentsRelay` (`//go:build integration`) runs a real bridge-join relay against live open-agents (web+bridge provisioned via `/api/dev/setup`, web receives `device:online`) and asserts the path coverage. Observed: `exercised={bridge|web|device:online}`, **`path_coverage=0.500`** — an objective >0 fraction with the real `device:online` edge attributed, exactly the "X/N edges" the authority model targets.

Note: an *autonomous* `cerberus run` against the current `ws-realtime` dogfood still reports path coverage 0, because the dogfood protocol declares only the `web` role and `internal/head/scout/ws_cases.go:210` skips relay-case generation unless `len(Protocol.Roles) >= 2`; without a declared `bridge` role (and a `/ws/<userId>` path template + provisioning hook), no message-exchanging case is generated. That is a dogfood/protocol declaration gap separate from the coverage-authority change; the live integration test above bypasses it by provisioning both roles directly, which is what proves the measurement itself works against real open-agents evidence. First autonomous run stats: 3 pass / 1 fail / 1 skip, ~16K tokens, 1m25s.

What this run definitively proves live: (1) the deterministic `PathThreshold=1.0` gate for has-vocab contracts; (2) Phase 2 path-coverage routing replaces the LLM coverage verdict; (3) `Kind:"path"` gaps do not trigger phantom coverage repair.

## Autonomous WS message coverage — re-verification (2026-08-08)

**Goal.** A `cerberus run` against the reworked `ws-realtime` dogfood (bridge role + `Responses` + `/ws/{userId}` template + provisioning-only authflow + `wsRequestResponseCases` generator) to honestly answer: does an *autonomous* run now report `coverage_pct > 0` against live open-agents (the path the prior note flagged as a dogfood/protocol gap)?

**Setup.** Build at HEAD (`make build` → `build/cerberus`). open-agents dev server up on `:8989`. Sanity: `make integration-openagents TEST=TestVocabularyDriven` — green (31 cases, all message-edge subtests PASS, 2 known SKIPs). Autonomous run:

```
./build/cerberus run --config dogfood/ws-realtime/.cerberus/project.yaml \
  --dir dogfood/ws-realtime \
  --goal "Relay a session between web and bridge over the realtime WS service"
```

**Observed coverage line (honest, verbatim):**

```
session/coverage.go:79 "coverage assessment" reached:false gaps:64 coverage_pct:0
```

Coverage is **0%** — `coverage_pct:0`, `reached:false` (PathThreshold=1.0), 64 gaps. The session measured path coverage (`"coverage not applicable"` did NOT appear — the coverage engine ran and produced an objective measurement, not a skip).

**Did the reqresp case run?** Yes. Exactly one `wsRequestResponseCases`-generated case executed:

- `ws-realtime-bridge-reqresp-session-start-session-created` → executor `status:fail` (4 ms, attempts:1) → Examiner verdict `status:fail correctness:0`.

Session totals: 6 pass / 19 fail / 6 skip / 5 recovered, ~58K tokens, 6m59s (hit the 420 s cap). The 19 fails break down as: 3 auth, 3 endpoint_drift, 3 handshake, 1 shape (per the Session Summary), with the remaining fail verdicts on rule-engine probe attempts against the unauthenticated actor.

**Root cause (investigated from the log, not assumed).** The predicted deviceId-payload risk did NOT trigger — the failure is upstream of any WS exchange. Both actors were degraded to unauthenticated before the reqresp case opened a socket:

```
session/auth_setup.go:46 "auth flow failed; degrading actor to unauthenticated"
  actor:"web-actor"   error:"auth flow: login returned status 403"
session/auth_setup.go:46 "auth flow failed; degrading actor to unauthenticated"
  actor:"bridge-actor" error:"auth flow: login returned status 403"
```

The open-agents dev server's CSRF middleware requires an `Origin` header on POSTs. The live integration suite (`internal/head/agent/openagents_setup_test.go:103`) sets `req.Header.Set("Origin", base)` for this reason; the runtime authflow (`internal/head/agent/authflow.go:132-141` `ResolveAuthHeader`) sets only `Content-Type` and any explicitly-declared `Login.Headers` — it does **not** synthesize an `Origin` header, so `POST /api/dev/setup` is rejected with HTTP 403, no `config.userId`/`deviceToken` is provisioned, and the reqresp case fails before a WS handshake can begin. The reqresp generator, role-param templating, host-relative login URL, and provisioning-only authflow all loaded and ran correctly (the case ID and 4 ms fail-no-socket shape confirm the wiring); the exchange simply never had credentials.

**Edges exercised.** None. With both actors unauthenticated, no `message_handled` edge was exchanged, so `exercisedEdges` returned 0 → 0/64. This is an honest measured 0%, not a fabricated one (the coverage engine ran to completion against the declared vocab).

**Follow-up (real root cause, not the predicted deviceId gap).** Add CSRF-safe header synthesis to the runtime authflow — either auto-set `Origin: <login host>` on authflow POSTs (matching the dev server's requirement and what the integration harness already does), or surface a `Login.Headers` escape hatch in the dogfood `project.yaml` so `Origin` can be declared without code changes. The minimal `wsSendBody` payload (`{"type":"session:start"}`) was NOT the blocker on this run; whether open-agents additionally requires `payload.deviceId` on `session:start` for the exchange to complete remains to be validated once provisioning succeeds.

**Reliable proof of the measurement machinery.** As in the prior note, the objective >0 proof path is the live integration test, not the autonomous run: `TestVocabularyDriven` (green above, 31 cases) and `TestPathCoverage_LiveOpenAgentsRelay` (commit e46b390, `path_coverage=0.500`) exercise the receive-driven attribution and the deterministic `PathThreshold=1.0` gate against real open-agents evidence with explicit `Origin` provisioning. The autonomous run's 0% is a provisioning-wiring gap, not a coverage-model regression.

## Autonomous WS message coverage — CSRF fix re-verification (2026-08-10)

**Goal.** The 2026-08-08 run proved the coverage machinery is objective but recorded `coverage_pct:0` because both actors were rejected at provisioning (`POST /api/dev/setup` → 403: the open-agents CSRF middleware requires an `Origin` header). This run applies the documented follow-up fix — declaring `Origin` via the supported `Login.Headers` escape hatch in the dogfood `project.yaml` (commit `26c7e24`, config-only, no runtime code change) — and re-answers: does an *autonomous* run now report `coverage_pct > 0`?

**Provisioning root cause — confirmed and fixed.** Direct probe against the live dev server (`wrangler dev --port 8989`):

```
POST /api/dev/setup  (no Origin)            → 403
POST /api/dev/setup  (Origin: localhost)    → 200, {"config":{"userId":"user_...","deviceId":"device_...","deviceToken":"token_..."}}
```

Cross-call determinism verified: `userId` is identical across independent `/api/dev/setup` calls within a server session (so web-actor and bridge-actor, each provisioning independently, land on the **same** Durable Object); `deviceId` differs per call (each provisions its own device). The `path_params` dot-paths in `project.yaml` (`config.userId` / `config.deviceId` / `config.deviceToken`) match the response shape.

**Run.** `make build` at HEAD `26c7e24`; open-agents up on `:8989`; autonomous run against `dogfood/ws-realtime/.cerberus/project.yaml`.

**Observed coverage line (honest, verbatim):**

```
session/auth_setup.go:59  "auth flow resolved"  actor:"web-actor"    value_len:17   (Bearer demo_token)
session/auth_setup.go:59  "auth flow resolved"  actor:"bridge-actor" value_len:49   (Bearer <provisioned deviceToken>)
session/coverage.go:79    "coverage assessment" reached:false gaps:63 coverage_pct:0.015873015873015872
```

**Coverage is now >0** — `coverage_pct = 1/63 ≈ 1.59%`, an objective, measured fraction. `gaps` dropped from 64 to 63: exactly **one** `message_handled` edge was exercised under receive-driven attribution. Contrast with every prior autonomous run on this dogfood (0/64): the CSRF fix let both actors authenticate, the deterministic `wsRequestResponseCases` case (`ws-realtime-bridge-reqresp-session-start-session-created`) executed a real two-socket exchange against live open-agents, and at least one declared message edge was matched on the receive side. `"coverage not applicable"` did NOT appear — the path-coverage engine ran to completion against the declared vocab.

**Honest partial-success caveat.** The reqresp case itself still `status:fail` (Examiner verdict `correctness:0.05`); only 1 of the 2 exchange edges was exercised. Session totals: 3 pass / 3 fail / 1 skip, ~27K tokens, 1m53s (failure causes: 2 handshake, 1 auth). This means the branch's *success criterion* (autonomous `coverage_pct > 0`) is met — but the *complete* request-response exchange (web→bridge `session:start` AND bridge→web `session:created`) did not both complete. The likely next blocker is payload shape: the deterministic `wsSendBody` sends a minimal `{"type":"session:start"}` with no `payload.deviceId`; open-agents' relay likely needs the recipient device id to route the message, so only one direction matched. Validating this (and, if needed, teaching `wsSendBody` / the `Responses` declaration to carry a device-id payload) is the follow-up — separate from the coverage-authority work this branch closes.

**What this run definitively proves (live):** (1) the CSRF/`Origin` provisioning fix resolves the 403 that gated every prior autonomous run; (2) an autonomous `cerberus run` now reports an **objective, non-zero** message-edge path coverage (`1/63`) against live open-agents — closing the dogfood/protocol gap flagged since 2026-08-07. The reliable >0 fraction remains the live integration test (`TestPathCoverage_LiveOpenAgentsRelay`, `path_coverage=0.500`); the autonomous run now agrees in kind (smaller fraction, because it drives only the reqresp pair, not the full relay matrix).
