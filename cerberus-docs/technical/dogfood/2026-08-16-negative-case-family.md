# Negative Case Family — Live Run Findings

Date: 2026-08-16
Branch: `feat/negative-case-family`
Spec: `cerberus-docs/superpowers/specs/2026-08-16-negative-case-family-design.md`
Run: `/tmp/cerberus-neg-run3.log` (ws-realtime, 7m49s)

## Result

**45 pass / 0 fail** — the 5 declared violations all PASS against the live
open-agents dev api:

| Case | Verdict | Probes the SUT fact |
|---|---|---|
| `ws-realtime-web-oversize-message` | pass | 1MiB+ frame → close 1009 (room.ts:31-32) |
| `ws-realtime-bridge-bridge-rate-limit` | pass | 205-burst → error frame + close 1008 "Rate limit exceeded" |
| `ws-realtime-web-missing-device-id` | pass | `session:send` w/o deviceId → error `MISSING_DEVICE_ID` |
| `ws-realtime-web-csrf-no-origin` | pass | POST w/o Origin → 403 `CSRF_ERROR` |
| `ws-realtime-web-invalid-token` | pass | garbage Bearer → 401 |

Health gates: insufficient budget 0, empty target 0, hallucinated-id 0.
`judge failed: 1` — benign: the LLM judge made zero tool calls on the csrf
case ("judge decide: zero tool calls (drift or quality)") and degraded to
step status by design; the verdict stayed pass. Claims gate unchanged
(exit 3, pre-existing ws-relay emulated-only — negatives are unbound by
spec Non-goals).

## SUT facts corrected en route (spec deltas)

1. **Rate-limit semantics**: violations count PER DENIED MESSAGE
   (rateLimiter.ts `checkLimit`), not per window. One burst of
   `max + MAX_VIOLATIONS` = **205** messages closes mid-burst (message 205
   denied with violations=5 → close 1008). The spec's multi-window + pacer
   design was wrong and sent post-close traffic at a dead socket; the
   schema dropped `windows`, the generator emits a single burst sized by
   the declaration.
2. **CSRF bypass-by-Bearer**: open-agents CSRF middleware ALLOWS a missing
   Origin when a `Bearer` Authorization is present (security.ts:44-48).
   http_auth violation steps therefore must NOT inject AuthRole — the bare
   client is the point. (Cerberus-side this surfaced as `expect_status`
   not participating in step success; fixed — an explicit `expect_status`
   is now the step's success gate.)
3. **route_missing needs a payload object**: a frame with NO payload lands
   in the SUT's generic `MESSAGE_PROCESSING_ERROR` before the route check;
   `{"type":T,"payload":{}}` reaches the `MISSING_DEVICE_ID` branch
   (room.ts:450).

## Open-agents findings (probed 2026-08-16, previously unfiled)

- **JWT without `exp` claim is ACCEPTED** (200 on /api/sessions) — minted
  HS256 with the dev JWT_SECRET, sub=dev user, no exp. No rejection to
  declare; a token-lifetime finding for open-agents.
- **IDOR probe is safe on /api/sessions**: `?userId=` query param is
  ignored; ownership comes from the auth-derived `c.get('userId')`
  (routes/sessions.ts:15).
- Ported from the 2026-08-15 fidelity ladder (see
  `cerberus-docs/technical/dogfood/2026-08-15-real-e2e-fidelity-ladder.md`):
  ACP-first auto-detect blocks plain PTY; internal orchestrator endpoints
  500; selectDevice round-robin can pick the busier device; ACP rejects
  relative cwd; dev DB pro plan max_concurrent_tasks was 0 (fixed to 3).

## Run recipe (unchanged)

wrangler api on :8989 → `cd dogfood/ws-realtime &&
CERBERUS_MIGRATION_DIR=../../migrations ../../build/cerberus run`.
