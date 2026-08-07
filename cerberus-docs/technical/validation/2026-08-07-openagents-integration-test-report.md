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

A `>0` coverage fraction requires a run whose WS cases authenticate and exchange messages; that math is proven by unit tests (`internal/session` `TestExercisedEdges`: 1/2 edges exercised → `Pct 0.5`) and was not hit in this single run due to the pre-existing auth drift (orthogonal to this change). Run stats: 3 pass / 1 fail / 1 skip, ~16K tokens, 1m25s.

What this run definitively proves live: (1) the deterministic `PathThreshold=1.0` gate for has-vocab contracts; (2) Phase 2 path-coverage routing replaces the LLM coverage verdict; (3) `Kind:"path"` gaps do not trigger phantom coverage repair.
