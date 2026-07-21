# WebSocket Realtime Engine — Tier-1 Dogfood Harness (Design)

**Date:** 2026-07-21
**Status:** Design (awaiting review before plan)
**Depends on:** M0 (`...-m0-design.md`), M1 (`...-m1-design.md`), M2 (roles, field-assertions, framing, role-carriers), M3-1 (`...-ws-protocol-files-design.md`)
**Related:** M3 proposal (`...-m3-proposal.md`), M3-2 (`...-ws-scout-cases-design.md`), M3-3 (`...-ws-protocol-infer-design.md`)

## Background & Motivation

M0–M3-1 are merged to `main` (FF-merged, not pushed; HEAD `6721337`). Each milestone
shipped through a full cycle (spec → plan → TDD → opus final review) and is covered by
`internal/head/agent/websocket_test.go` (1224 lines) plus `ws_protocol_test.go` (142),
`ws_actions_test.go` (140), `result_ws_test.go` (47).

Those tests call the engine internals directly — `doConnect`/`doSend`/`doReceive`,
handshake, `injectAuth`, `BuildWSProtocolIndex` — against local test servers, with
hand-crafted `TestCase`/actions and actor `RawToken` values set in test code
(`ws_protocol_test.go:43` constructs `CredentialRef{RawToken: "JWT"}`, exploiting the
`yaml:"-"` field). They do **not** exercise three things that only a real `cerberus run`
can:

1. **The Steer LLM choosing and sequencing `ws_*` actions.** WS testing today is
   Steer-LLM-only: Scout emits no `ws_*` cases (`matchRules` has no WS phase; `TestCase`
   has no `Steps` field — single `Action` hint), so a case carries only
   `{target, expectation}` and the ReAct `steer()` loop must decide, from the steer prompt
   (`internal/head/agent/prompts.go:21-37`) + the loaded protocol declaration, to emit
   `ws_connect{role}` → `ws_send` → `ws_receive{decisive,assert}`. Whether the model does
   this reliably — and whether it drifts run-to-run — is untested.
2. **The auth-flow → raw-token → WS injection chain in a real run.** Session setup
   resolves each actor's `auth:` block once (`session/auth_setup.go:44` calls
   `ResolveAuthHeader`, `:57` writes `Credentials.RawToken`; `lifecycle_run.go:31` runs
   this *before* execution), then `BuildWSProtocolIndex` (`ws_protocol.go:129`) copies
   `RawToken` into `ActorTokens`, which `injectAuth` (`websocket.go:285`) injects. The
   prior self-dogfood (2026-07-18) ran in local mode with **no services and no auth** —
   this chain has never run against a live target.
3. **`protocol_ref` standalone-file loading in a real run** (M3-1's deferred integration
   test — "resolved-from-file protocol drives BuildWSProtocolIndex") and the full
   Scout→Agent→Examiner pipeline against a live WS service.

This dogfood closes that gap. It is also the M3 proposal's stated gate: M3-2 (Scout WS
case generation) and M3-3 (`protocol infer`) are **dogfooding-gated** — their trigger
signals (orchestration drift, blank-page declaration cost, cross-project reuse) can only
be observed against a real target.

### Grounding (verified against code, not assumed)

- `cmd/cerberus/main_run.go:31` `loadProjectConfig(configFlag,…)` → `project.LoadFromFile`
  → `LoadFromYAML(data, filepath.Dir(path))`, so `--config <path>` makes `baseDir` the
  config file's directory and `protocol_ref` resolves to `<baseDir>/.cerberus/protocols/<name>.yaml`.
- `run_phases_agent.go:25` and `resume_phases_run.go:25` both build
  `agent.BuildMultiExecutor(…, BuildWSProtocolIndex(cfg), …)`, so the WS executor is
  wired with the protocol index on both the run and resume paths.
- The auth chain (`auth_setup.go` → `ws_protocol.go:129` → `injectAuth`) is wired to run
  before execution.

### Verified frictions (second-pass review — these reshape the run strategy)

- **Role discovery is unsolved.** The steer LLM receives only
  `formatResultContext(tc, prevResult, attempt)` + the service base URL + the case
  Target/Expectation (`executor_steer.go:15,20,29`). The protocol declaration (roles,
  framing, `handshake.await_type`) is **not** injected into the steer context — so the
  LLM cannot know the role name `web` or that the handshake is `devices:sync`.
  `websocket.md` flags this as an open question; the dogfood confirms it. **Consequence:**
  roles are not usable in practice without goal-hinting. The run strategy therefore uses
  the **M1 fallback path (no role) as primary** and a **goal-hinted role run as a secondary
  stretch** to exercise the M2 role-expansion logic. The gap itself is the M3-2 trigger.
- **The steer prompt's action-type enum excludes `ws_*`.** `prompts.go` describes the WS
  primitives (lines 21-37) but the RULES action-type list (line 7) and the output JSON
  schema `action.type` enum (line 43) list only `api_request|navigate|wait|…` — no
  `ws_connect/ws_send/ws_receive/ws_disconnect`. A compliant LLM may therefore never emit
  `ws_*`. The parser accepts them; the prompt discourages them. Per the F1 decision, the
  dogfood runs **as-is first**; if the LLM does not emit `ws_*`, that is **Finding #0**
  (prompt defect) and the enum is fixed on a branch
  (`feat/ws-realtime-engine-dogfood-promptenum`) before re-running.

## Goal

Run `cerberus run` end-to-end against a minimal, self-authored WS target that mirrors
open-agents' shape (HTTP login issues a token; WS endpoint validates it), using a
`protocol_ref` declaration, and record:

- Whether each shipped deterministic mechanic works through the full Scout→Agent(Steer)
  →Examiner pipeline on live traffic (engine validation).
- M3-2 signal: Steer LLM orchestration quality and run-to-run drift.
- M3-3 signal: the blank-page cost of hand-authoring the protocol declaration.
- Any real-run integration bug (auth chain, protocol_ref loading, executor wiring).
- Explicitly: whether the two verified frictions above (role discovery, prompt enum) block
  WS orchestration in practice — each is a first-class observation / candidate finding, not
  an assumption of success.

## Non-Goals

- **Not** an exhaustive LLM-pipeline exercise of every mechanic. Header/subprotocol auth,
  text/binary framing, and subprotocol role-carriers are already covered by the 1224-line
  unit test and are rare in practice; they stay in unit tests. Tier-1 covers the
  representative path only (json framing, query auth, web role + handshake, field-assert).
- **Not** the real open-agents target. That is Tier-2 (separate effort; staged approach).
  Tier-1's protocol declaration is, however, authored to be reusable there.
- **Not** a regression test or `_test.go`. The harness is a manually-run dogfood (needs
  live LLM creds + a running server); it is not added to `make test`.
- **No** `bridge` role in Tier-1. The two-role cross-event scenario (proposal "C") is an
  optional stretch after the web flow passes; `bridge` is added then.

## Design Decisions

### D1 — Staged target (minimal server first, open-agents second)

Tier-1 (this spec) = a self-authored minimal server; Tier-2 (later) = real open-agents via
`wrangler dev :8787`. Tier-1 cannot be blocked by open-agents infra and is the actual
"did we ship a working engine" proof; Tier-2 is where the authentic blank-page/reuse
signals live and degrades gracefully (hand-write the declaration from open-agents' docs
even if the live server won't come up — which itself reproduces the M3-3 pain).

### D2 — Representative open-agents-like path

One server, one port, two paths: `POST /login` (issues a token) + `WS /realtime`
(validates the token). This mirrors open-agents' Cloudflare-Workers-login +
Durable-Object-realtime split, so Tier-1's declaration is directly reusable at Tier-2.

### D3 — The server also serves login (a feature, not a burden)

An actor's `RawToken` is `yaml:"-"` and is populated only by the `auth:` login flow at
session setup. There is no static-token shortcut for WS in a real run. Therefore the
target must serve an HTTP login endpoint that returns the token the WS endpoint later
validates. Side benefit: this dogfoods the **auth-flow → raw-token → WS strip-then-inject
chain end-to-end** — precisely the integration no prior dogfood or unit test covered.

### D4 — `protocol_ref` (standalone file), not inline

Declares the protocol in `.cerberus/protocols/open-agents.yaml` referenced by
`protocol_ref`. This dogfoods M3-1's real-run file-loading path (closing the deferred
"resolved-from-file drives BuildWSProtocolIndex" gap) and produces the version-controlled,
Tier-2-reusable artifact that is the whole point of M3-1.

### D5 — In-repo standalone runnable program

The target server is a `package main` under `dogfood/ws-realtime/` (in-module, so `go run`
works with no separate `go.mod`; `coder/websocket` is already a dependency — no new deps).
`make lint`/`make test ./...` compile it (no tests); this keeps the harness code clean.
All dogfood artifacts (server source + `.cerberus/` project) are self-contained under
`dogfood/ws-realtime/`, isolated from the repo's own `.cerberus/`.

### D6 — Single `web` role (YAGNI)

Tier-1 declares only the `web` role + `web-actor`. `ValidateProtocol` rejects a role whose
`credential_ref` names no real actor, so declaring `bridge` would force a `bridge-actor`
the primary goal never exercises. `bridge` is added with scenario C / Tier-2.

### D7 — Two run paths (M1 primary, M2 stretch) and run-as-is for the prompt defect

Because role discovery is unsolved (verified friction above), the **primary run uses the
M1 fallback path** — `ws_connect` with no `role`, relying on `protocol.auth` injection,
routing, and `assert`. A **secondary, goal-hinted run** explicitly tells the LLM the role
name and that the handshake is automatic, to exercise the M2 role-expansion + auto-handshake
engine logic. And per the F1 decision, the first run is **as-is** (no prompt pre-fix); the
`ws_*`-enum gap is surfaced as Finding #0 if it blocks orchestration, then fixed on a branch.

## Architecture

### Layout

```
dogfood/ws-realtime/
  main.go                       # minimal login + WS target server (package main)
  .cerberus/
    project.yaml                # service[realtime] + web-actor (auth:) + settings
    protocols/open-agents.yaml  # protocol declaration (protocol_ref target; committable)
    runtime/                    # cerberus runtime db/logs (gitignored; auto-created)
```

`baseDir` = the `project.yaml` directory (`dogfood/ws-realtime/.cerberus/`), so
`protocol_ref: open-agents` resolves to `dogfood/ws-realtime/.cerberus/protocols/open-agents.yaml`.

### Target server (`main.go`) — message contract

One port (`:8787`, matching the docs' example URL; overridable via flag). Pure
`coder/websocket` v1.8.14 + `net/http`.

- `POST /login` `{email, password}` → `200 {"token":"<random>"}` (loose: any non-empty
  credentials; issued tokens held in an in-memory map). Else `401`.
- `WS` upgrade at **both** `/` and `/realtime` (lenient — the protocol declaration carries
  no url path, so the LLM must learn `/realtime` from the goal; accepting `/` avoids a
  path-mismatch deadlock). `websocket.Accept` with `InsecureSkipVerify: true` (non-browser
  client; local dogfood).
- On connect: read query `token`; reject (close `4001`) if absent or not in the issued map.
  **Unconditionally** send `{"type":"devices:sync","devices":[]}` right after upgrade (so the
  M1 no-role path still sees it as evidence and the M2 role path auto-awaits it). Read query
  `type` only to flavor the ack's `role` field (default `web`).
- Read loop: parse JSON; switch on `type`:
  - `device:command` → send `{"type":"device:ack","payload":{"approved":true,"role":"<type-or-web>"}}`.
  - anything else → ignore (accumulate as seen evidence on the engine side).

This single flow exercises: auth-flow login → raw-token, query strip-then-inject, `type`
param role discrimination, `web` auto-handshake (`devices:sync`), `type_path` routing,
send `device:command` → receive `device:ack`, and field-assert on `payload.approved`.

### Protocol declaration (`protocols/open-agents.yaml`)

```yaml
framing: json
type_path: type
auth:
  strategy: query
  param: token
  credential_ref: web-actor
roles:
  web:
    credential_ref: web-actor
    params:
      type: web
    handshake:
      await_type: devices:sync
      timeout: 5
```

Verbatim-shape with the `websocket.md` example (minus `bridge`). Reusable as-is when
Tier-2 points the same `protocol_ref` at real open-agents.

### Project config (`project.yaml`)

```yaml
project:
  name: ws-realtime-dogfood
services:
  - name: realtime
    url: http://localhost:8787
    protocol_ref: open-agents
actors:
  - name: web-actor
    credentials:
      email: web@dogfood.local
      password: dogfood-web
    auth:
      login:
        method: POST
        path: /login
        body: { email: "{email}", password: "{password}" }
      token_from: token
      inject_as: "Authorization: Bearer {token}"   # HTTP only; WS uses the raw token
settings:
  max_duration: 8m
  confidence_threshold: 0.7
  ai_budget:
    session_total_tokens: 60000
    per_call_limit: 10000
```

`auth` is the actor field (`schema.go:36`, `*AuthFlow`). `inject_as` is unused by WS query
auth but retained for HTTP parity / structural validity (confirm `ValidateAuthFlow`
required fields at plan time). LLM credentials/model inherit from `.claude/settings.json`
(GLM) as in the 2026-07-18 self-dogfood.

## Run Procedure

1. `make build`.
2. Terminal 1: `go run ./dogfood/ws-realtime` (serves `:8787`).
3. Terminal 2, **primary (M1 path)**:
   ```
   ./build/cerberus run \
     --config dogfood/ws-realtime/.cerberus/project.yaml \
     --dir dogfood/ws-realtime \
     --goal "As a web client, connect to the realtime service WebSocket at /realtime, send a {type: device:command} message, and verify the server replies with a {type: device:ack} whose payload.approved is true."
   ```
4. **Secondary (M2 path, goal-hinted)** — same config, different goal:
   `"... connect with role 'web' (the server auto-completes a devices:sync handshake after connect), send {type: device:command}, and verify {type: device:ack} with payload.approved true."`
5. **Drift:** repeat the primary goal 2–3 times. **Use a fresh runtime DB per drift run**
   (`rm -rf dogfood/ws-realtime/.cerberus/runtime/` between runs) so reflexion recall does
   not confound the drift measurement. Diff per-run Steer action traces (steer-attempt logs
   / session report).
6. If the primary run shows the LLM never emits `ws_*` (Finding #0), branch
   `feat/ws-realtime-engine-dogfood-promptenum`, add `ws_*` to `prompts.go` RULES (line 7)
   and output schema (line 43), and re-run before drawing engine conclusions.

## Observations → Signal Mapping

| Observation | Validates / Triggers |
|---|---|
| `protocol_ref` file loads in a real run and drives the engine | M3-1; closes the deferred integration gap |
| `auth:` login → raw-token → query strip-then-inject, secret hygiene | M1 chain, never before run live |
| `role: web` discrimination + `devices:sync` auto-handshake | M2 roles/handshake |
| send `device:command` → receive `device:ack` + `assert payload.approved` | M0/M1 routing + M2 field-assert |
| Steer LLM emits `ws_*` at all (vs the enum gap) | **Finding #0 candidate** — prompt defect; M0 orchestration |
| Steer LLM sequences `ws_connect`→`ws_send`→`ws_receive{decisive,assert}` | M0 orchestration quality |
| Roles unusable without goal-hinting (protocol decl not in steer context) | **M3-2 trigger** (role discovery / deterministic case skeleton) |
| Steer action-sequence differences across runs | **M3-2 drift signal** (trigger evidence) |
| Effort to hand-author `open-agents.yaml` from the target's contract | **M3-3 blank-page signal** (trigger evidence) |
| Reusing the same `protocol_ref` artifact at Tier-2 (later) | M3-1 reuse signal |

## Risks & Contingencies

- **R1 — Steer never emits `ws_*` (Finding #0).** Root cause is the verified prompt defect:
  `ws_*` is absent from the steer action-type enum (`prompts.go:7,43`) despite the primitives
  section. Per D7/F1 this is surfaced as-is, then fixed on a branch and re-run. If even with
  the enum fixed Scout emits no WS-targeting case, investigate case seeding (record as a finding).
- **R2 — Roles unusable without goal-hinting.** Confirmed: the protocol declaration is not in
  the steer context. The primary run therefore does not depend on roles; the secondary run
  goal-hints them. The inability to use roles unaided is the M3-2 trigger signal, recorded as
  a finding, not a blocker.
- **R3 — Steer re-receives the handshake.** The auto-handshake consumes `devices:sync`
  into `SeenMessages`; a `ws_receive devices:sync` afterward times out. The goal is worded
  to avoid implying manual handshake receipt. Whether the model respects the steer prompt's
  "handshake runs automatically" hint is an observation.
- **R4 — coder/websocket `Accept` origin check rejects the non-browser dial.** Mitigation:
  `InsecureSkipVerify: true` on the server (local dogfood).
- **R5 — Drift confounded by reflexion recall.** The persistent runtime DB carries L1/L3
  memory across runs. Mitigation: fresh runtime DB per drift run (Run Procedure step 5).
- **R6 — Multi-run token cost.** 2–3 runs ≈ 2–3× the per-run budget; size accordingly and
  stop after the drift signal is clear.
- **Confirmed-OK (no change):** `auth_setup` mutates the same `cfg.Actors` slice
  `BuildWSProtocolIndex` reads (aliasing safe, `auth_setup.go:24`); `loadProjectConfig` routes
  through `LoadFromFile` (path-based baseDir); actor→service login falls back to the first
  service (`auth_setup.go:86`), so `web-actor` logs into `realtime` (`service: realtime`
  optional but may be set for explicitness).

Any engine defect surfaced → standard cycle (branch `feat/ws-realtime-engine-dogfood-<area>`
→ spec → plan → TDD → opus final review → local ff-merge → `make check` → update roadmap
memory + ledger). Prompt issues → record only (prompts.go is a single raw-string literal;
edits are inline, backtick-free).

## Relationship to M0 / M1 / M2 / M3

- **M0–M3-1:** validated through the full pipeline on live traffic (the gap the unit tests
  leave).
- **M3-2:** drift signal collected; if Steer orchestration is unreliable or non-reproducible,
  that is the trigger to implement the deterministic Scout WS-case skeleton.
- **M3-3:** blank-page signal collected; if hand-authoring the declaration is a repeated
  cost, that is the trigger to implement `cerberus protocol infer`.
- **M3-1 reuse:** validated when the same `open-agents.yaml` is pointed at real open-agents
  in Tier-2.

## Testing Strategy

This is a dogfood, not a unit-test addition. Correctness of the *target server* itself is
kept high-confidence by (a) keeping it tiny and obvious, and (b) the engine's existing
1224-line suite already proving the client side. The target's own behavior can optionally
get a small `_test.go` later if it becomes a reused fixture; out of scope for Tier-1.

## Open Questions

1. Does Scout, given a WS-implying goal against a `protocol_ref`-declared service, produce
   a case the Steer loop can run? (Observed at run time.)
2. Does the Steer LLM (GLM) reliably emit the `ws_*` sequence and respect the
   auto-handshake hint? (Observed; this is the core M3-2 input.)
3. Is the hand-authored `open-agents.yaml` cheap enough that M3-3 is unwarranted, or
   painful enough to trigger it? (Judged from the authoring experience.)
4. Scenario C (two-role cross-event) as a second scenario after the web flow passes — worth
   the extra orchestration risk for a stronger drift signal? (Decided after Tier-1 runs.)
