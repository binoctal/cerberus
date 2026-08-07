# open-agents Live Integration — Test Report (2026-08-07)

## Scope (honest)

This report covers **cerberus's WebSocket execution surface driven against a live open-agents dev server**. It is NOT an end-to-end `cerberus run` (Scout→Agent→Examiner) report — that pipeline was not run this session (see "Out of scope / gaps"). The tests use **hand-authored `TestCase` Steps** (the test writer declares connect/send/receive), not cases Scout autonomously generated for this run.

## Environment

- cerberus binary: built at HEAD `8b3a0b4` (`make build`), including the sender-exclusion probe and the dimensionGuidance scope change.
- Target: live open-agents `apps/api` dev server, `wrangler dev --port 8989`, Node `v22.22.3` (selected via `fnm`; system default v20 is too old for wrangler).
- Provisioning: `POST /api/dev/setup` per run, yielding a fresh `userId` / `deviceId` / `deviceToken`; web auth via the `demo_token` dev backdoor, bridge via the provisioned device token + `deviceId`.
- Run command: `make integration-openagents` (brings the server up, runs the suite, tears it down).
- No LLM was exercised by these tests — they drive the deterministic `runSteps` executor directly. (LLM-based verification is listed under gaps.)

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

1. **No real `cerberus run`.** The full Scout (LLM plan) → Agent (LLM steer / deterministic steps) → Examiner (LLM judge) pipeline was not run against open-agents this session. The last such run was 2026-07-24, on a pre-probe / pre-dimension-guidance binary.
2. **Scout autonomous generation not exercised end-to-end.** These tests use hand-authored `Steps`. Deterministic Scout case generation (`wsRelayCases`, `wsStepsCase`, `BuildEdgeSteps`) is unit-tested separately; the two halves were not run as one live pipeline.
3. **Probe judge effect uses synthetic evidence.** `TestSenderExclusionProbeLive` verifies the executor's probe behavior, but the Examiner's `Excluded=true → clean verdict` was validated only via the synthetic manual harness (`vocab_validation_manual_test.go`), not against evidence a live run produced.
4. **Vocab is a committed snapshot.** `TestVocabularyDriven` reads the committed `open-agents.vocab.yaml`, not a vocab regenerated from the current running service. If the live open-agents protocol drifted from that file, the test exercises a stale vocab.
5. **`/broadcast` HTTP→WS endpoint (`room.ts:98-108`) is not covered** — Worker→DO→clients push needs an HTTP-into-DO step, a different capability class (documented deferred in the mapping doc).

## Confidence

High for the claim "cerberus's WS executor correctly drives the open-agents relay surface." Not established for the claim "a full autonomous `cerberus run` passes end-to-end against open-agents" — that requires the gap-1 run.

## Reproduce

```
make integration-openagents                         # whole suite
make integration-openagents TEST=TestVocabularyDriven   # the vocab matrix only
```
The target handles server bring-up (fnm 22 → wrangler) and teardown; it reuses an already-running :8989 without killing it.
