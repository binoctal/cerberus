# Negative / Exception Case Family — Design Spec

Date: 2026-08-16
Status: approved design (brainstormed), pending implementation plan
Branch target: `feat/negative-case-family` (from main @ 3c8c257)

## Problem

Every deterministic generator cerberus has today proves the happy path: a
message sent correctly arrives correctly. The SUT's *rejection* semantics —
oversize payloads, rate-limit enforcement, missing-route errors, auth
boundaries — are completely untested. These are half of the relay promise
and were explicitly deferred (fidelity-ladder plan Task 8, item 1).

## Goals

1. Declarative negative facts: a `violations:` section in the hand-authored
   protocol file (`protocols/*.yaml`), NOT in vocab.yaml (machine-extracted,
   hash-pinned — hand entries would be clobbered on re-extraction).
2. A deterministic scout generator `violationCases` consuming declarations —
   same conventions as `wsRelayCoverageCases` / `realE2ECases` (sorted, deduped,
   ID-namespaced, no LLM).
3. Executor gap-fills only where a declared expectation cannot be expressed
   today: WS close-code assertion, oversize payload generation.
4. Validation: live run in `dogfood/ws-realtime` with the negative family
   passing against the real open-agents dev server.

## Non-goals

- No claims-ledger binding this round (negative verdicts stay out of the
  gate; the tier model's blind spot — "SUT rejection semantics evidenced by
  a self-played actor IS real SUT evidence" — is a future amendment).
- No LLM-generated negatives (deterministic only).
- No extractor for violations (hand-declared now; an extractor can emit the
  same schema later).
- No open-agents SUT facts hardcoded in cerberus source (SUT-generic rule).

## Open-agents facts (probed 2026-08-16, pinned with sources)

| Fact | Value | Source |
|---|---|---|
| Message size limit | 1 MiB hard limit | `room.ts:31` |
| Oversize close code | 1009 `WS_CLOSE_MESSAGE_TOO_LARGE` | `room.ts:32,230` |
| Policy-violation close code | 1008 `WS_CLOSE_POLICY_VIOLATION` | `room.ts:33` |
| Bridge rate limit | 200 msg / 1000 ms | `room.ts:37` (`ConnectionRateLimiter(200,1000)`) |
| Web rate limit | per-connection `meta.wsPerMin` (plan-driven) | `room.ts:265` |
| Rate-limit violation | 1st: error frame `RATE_LIMIT_EXCEEDED`; close 1008 only after 5 violating 1s windows (`MAX_VIOLATIONS=5`) | `room.ts:243-250`, `rateLimiter.ts:18` |
| Missing route field | error frame, code `MISSING_DEVICE_ID` | `room.ts:439,450` |
| Error frame shape | `{type:"error",payload:{code,message},timestamp}` | `room.ts:543-549` |
| CSRF rejection | JSON `CSRF_ERROR` "Invalid origin" (status: probe at plan time) | `middleware/security.ts:16,80+` |

JWT-without-exp and SEC-1 IDOR rejection shapes are NOT yet probed — the
plan contains an explicit probe step before their declarations are written.

## Design

### 1. Schema (`internal/project/protocol.go`)

```yaml
violations:
  - id: oversize-message
    family: oversize            # oversize | rate_limit | route_missing | http_auth
    role: web                   # which declared role triggers it
    trigger:
      bytes: 1048577            # oversize: payload size to send
      type: chat:message        # frame type to wrap
    expect:
      close_code: 1009          # oversize/rate_limit ⇒ close
  - id: bridge-rate-limit
    family: rate_limit
    role: bridge
    trigger:
      messages: 220             # burst count, comfortably over 200/window
      windows: 6                # >= MAX_VIOLATIONS(5)+1 violating 1s windows
      type: chat:message
    expect:
      frame_type: error         # first reaction: always sent
      code: RATE_LIMIT_EXCEEDED
      close_code: 1008          # after the 5th violating window
  - id: missing-device-id
    family: route_missing
    role: web
    trigger:
      type: session:start       # a type the router routes by payload.deviceId
      omit_fields: [deviceId]
    expect:
      frame_type: error
      code: MISSING_DEVICE_ID   # asserted against payload.code
  - id: csrf-no-origin
    family: http_auth
    role: web
    trigger:
      method: POST
      path: /api/dev/setup
      drop_headers: [Origin]
    expect:
      http_status: 403          # pinned by plan-time probe
```

`Violation` struct fields: `ID`, `Family`, `Role`, `Trigger` (typed per
family), `Expect` (`FrameType`+`Code` for error frames, `CloseCode` for
closes, `HTTPStatus` for HTTP; `rate_limit` carries both a frame and a
close expectation — first reaction then threshold close). Validator:
family↔expect combination legality, role must be declared, family-specific
trigger completeness. Unknown families rejected.

### 2. Generator (`internal/head/scout/`)

`violationCases(svc)` — one case per declared violation, emitted inside
`wsCasesForService` (and an http branch for `http_auth`), sorted by ID:

- `route_missing`: ws_connect(role) → ws_send(trigger type minus omitted
  fields) → ws_receive `frame_type` with `Asserts: {"payload.code": code}`.
- `oversize`: ws_connect(role) → ws_send body containing `{{pad:<bytes>}}`
  → `ws_expect_close` with code.
- `rate_limit`: ws_connect(role) → `messages` × ws_send (generator expands
  the burst into N send steps — no executor loop construct, YAGNI) →
  ws_receive `error` asserting `code` → `ws_expect_close` 1008 (the burst
  repeats across `windows` 1s windows so the close threshold is hit; case
  duration ~`windows` seconds by construction).
- `http_auth`: http_request(method, path, headers minus dropped) →
  status assertion (existing `status` step field).

No claims binding (Goals/Non-goals). IDs: `ws-<svc>-<role>-<violation-id>`.
Dedup: skip a violation whose ID-shape is already present (same key logic
as relay coverage — trivial here since IDs are unique by construction).

### 3. Executor (`internal/head/agent/websocket.go`)

- `ws_expect_close` step action: after a trigger send, await the WS close
  frame (the read pump currently ignores/handles close frames implicitly —
  extend it to surface code+reason to the step result), assert the code
  matches `code`, pass/fail. Timeout semantics identical to ws_receive.
- `{{pad:N}}` body template: resolvePlaceholders gains one intrinsic that
  renders N bytes of filler (e.g. `"x".repeat(N)` inline in the payload
  JSON as a string value). Applies to ws_send messages only.

### 4. Dogfood wiring

`dogfood/ws-realtime/.cerberus/protocols/open-agents.yaml` gains the four
declarations above (plus JWT/IDOR entries after the plan-time probe).

### 5. Testing

- Schema + validator unit tests (family↔expect matrix, bad role, incomplete
  trigger).
- Generator fixture tests: one golden case per family asserting exact step
  shape.
- Executor: `ws_expect_close` and `{{pad:N}}` against a fake in-process WS
  server (existing test harnesses in `websocket_test.go` style).
- Live: `dogfood/ws-realtime` full run — negative family passes, health
  gates all 0, exit code unchanged by negatives (no claims binding).

## Error handling

- Undeclared role in a violation → config validation error (fail fast,
  same as existing protocol validation).
- `ws_expect_close` timing out (server did NOT close) → case FAIL with the
  observed tail of received frames in evidence — a missing rejection is a
  product finding, not a test error.
- Oversize send rejected by the *client* websocket library before hitting
  the wire → surface as a distinct failure mode (test-harness limitation),
  not silently pass.

## Open questions (resolved at plan time, not blocking spec)

- CSRF rejection HTTP status literal (403 vs 200-with-error-body).
- JWT-without-exp: whether the api rejects at connect (WS query token) or
  at first protected HTTP route.
- SEC-1 IDOR resource shape (which cross-user path param is observable).
