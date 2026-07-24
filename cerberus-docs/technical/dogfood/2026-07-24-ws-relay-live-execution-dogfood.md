# WS Relay — Live Execution Dogfood — 2026-07-24

## Goal

The first end-to-end `cerberus run` (CLI) validation of the open-agents
web↔bridge relay. Two pieces were each proven independently — Scout relay
generation (the 3C deterministic detector, live-probed via
`TestScoutRelayEmission_Live`) and the executor (F1 multi-connection, the
`//go:build integration` test) — but a real `cerberus run` driving the full
Scout → Agent → Examiner pipeline against live open-agents traffic had never
been validated as a whole. This run closes that gap.

## Setup

- open-agents dev server: a leftover process on `:8989` (reused; `GET /` → 404
  is the normal response, never killed a process we did not start).
- Provision (one-off): `POST /api/dev/setup` with an `Origin` header →
  `userId=user_1779727840484`, `deviceId=device_39254f0f840441f8`,
  `deviceToken=token_e1fb851c-…`.
- cerberus binary: `make build` at HEAD `4ac4db0` (all WS features: F1–F4,
  Scout relay, 3C detector, static-token auth).
- Config (inline protocol, self-contained): `http://` service URL (see Finding
  1), `web=demo_token` static token, `bridge=deviceToken` static token with
  `deviceId` in role params, web role `handshake: device:online, optional,
  timeout 2`.

## Run

```
cerberus run \
  --config <tmp>/project.yaml --dir <tmp> --db <tmp.db> \
  --goal "The web client and the bridge client connect to the realtime service
  on the same user. When the bridge client joins, the web client receives the
  relayed device:online signal. Verify the cross-connection relay: web receives
  device:online after bridge connects."
```

(`run` has no `--verbose` flag; it logs JSON to stderr by default.)

LLM: BigModel GLM (Scout/Examiner `GLM-5.1`, Agent `GLM-4.5-Air`). ~31K tokens,
2m23s. Verdicts: **6 pass / 6 fail / 1 skip**.

## Result — the relay case PASSES (the goal)

`ws-realtime-relay-web-signal-device-online` (the 3C deterministic relay case):

- execute: **pass, attempts=1**
- verdict: **pass, correctness 0.95**

DB trace for this case (`phase=steps` → runSteps, deterministic, **no Steer**):

1. `ws_connect` web → `ws ok …?type=web connection_id=web` (success)
2. `ws_connect` bridge → `ws ok …?deviceId=device_39254f0f…&type=bridge connection_id=bridge` (success)
3. `ws_receive` device:online → **matched=1**:
   `{"type":"device:online","payload":{"deviceId":"device_39254f0f840441f8","deviceName":"Device 41f8"}}`

Both sockets connected the **same** Durable Object (`/ws/user_1779727840484`);
the relayed `device:online` arrived on the web socket. The end-to-end relay is
validated on a real `cerberus run`.

## Findings

### 1. Service URL must be `http://` (not `ws://`)

`Validate` rejects `ws://…` ("must start with http:// or https://"). A `ws://`
service URL makes config load fail, which **silently falls back to the default
config** (no protocol) — the run then tests nothing WS-related and the waste is
not obvious. Use `http://`; `doConnect`'s `wsURL()` flips `http→ws` at dial
time. (F1's integration test used `ws://` because it built the
`WSProtocolIndex` in-process, bypassing service-URL `Validate`.)

**Action:** add a one-line note to `websocket.md` that service URLs are
`http(s)://`; the `ws://` flip is internal.

### 2. Deterministic Steps path vs Steer path — reliability gap confirmed

- The relay **Steps** case (`phase=steps`, runSteps): 3 steps clean **PASS,
  attempts=1**.
- The single-connection connect cases `ws-realtime-web-connect` /
  `ws-realtime-bridge-connect` (the WSCasesCovered connect form — **not** a
  Steps case): execute **fail ×3**, `matched=0 seen=0` (instant close / 404–403).
  These route through rule-engine → Steer (the LLM emits `ws_connect`); Steer
  drift surfaced as inconsistent connection_ids (`web_client`,
  `bridge_conn_1`, `bridge_conn_2`) and bad URLs in the traces.

Implication: the deterministic Steps path (3C relay + Steps exchanges) is not
just a relay feature — it is the **reliable** WS execution path. Single-action
`ws_connect`/`ws_receive` cases that fall back to Steer inherit the
long-standing WS drift (Finding-3). Future: consider routing single-connection
WS cases through a deterministic executor path, or suppressing them when a
Steps case already covers the role (the 3C detector already suppresses the
redundant single-conn *receive* of a relayed signal; the connect form remains).

### 3. Steer WS vocabulary still noisy (Finding-3 continuation)

`tc-001..006` (Scout LLM free-form cases) drift toward HTTP/`api_request`;
mixed pass/fail. Pre-existing, unchanged by this run. Out of scope here.

## Conclusion

The WS arc's headline capability — deterministic multi-connection relay — is
now validated end-to-end on a real `cerberus run` against live open-agents:
web + bridge connect one Durable Object, and web receives the relayed
`device:online`. Scout generation (3C) + executor (F1) + static-token auth
compose correctly under the real CLI pipeline (with the GLM key). Remaining
open items: Finding-2 (route single-conn WS cases deterministically) and
Finding-3 (Steer WS drift on free-form cases).
