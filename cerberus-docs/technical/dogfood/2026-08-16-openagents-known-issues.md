# open-agents Known Issues (Protocol Inconsistencies)

Date: 2026-08-16
Source: research for the fidelity-ladder real-E2E plan (2026-08-14), re-verified
against `../open-agents` HEAD on 2026-08-16. Closes fidelity-ladder Task 8
item 4 (see `superpowers/plans/2026-08-14-fidelity-ladder-real-e2e.md`).

Scope: facts a Cerberus Scout/executor must know when modeling open-agents
behavior. Each item lists the verified file:line evidence and the Cerberus-side
consequence. None of these are Cerberus bugs; they are upstream divergences
between the open-agents API worker, the Durable Object room, and the Go bridge.

---

## 1. DO message whitelist vs bridge handleMessage diff

The DO (`apps/api/src/realtime/room.ts:340+`) only forwards whitelisted
`msg.type` values on the web→bridge path; the Go bridge
(`bridge/internal/bridge/bridge.go` handleMessage) accepts a wider set. Types
the **bridge can handle but the DO silently drops** (web origin can never
reach them):

- `session:resume`
- `scanner:toggle`
- `workflow:get_state`, `workflow:set_state`, `workflow:merge_all`,
  `workflow:task_cleanup`, `workflow:task_merge`

Reverse direction: the DO forwards `device:listDir` (room.ts web→bridge
section) but the Go bridge has **no handler** for it — only the internal
`listDirectories` helper and its test exist. Web-origin directory browsing is
dead end-to-end: DO routes it, bridge ignores it, yet bridge *emits*
`device:listDirResult` (DO bridge→web whitelist).

**Cerberus consequence:** do not generate coverage cases that require
web→bridge `session:resume` or the merge/cleanup workflow commands over WS;
they cannot pass. `device:listDir` cases will observe the request forwarded
but no `device:listDirResult` ever returning.

## 2. Three divergent dev-setup endpoints

Three implementations of "create dev user + device", all live in dev mode:

| Endpoint | File | Existing-user behavior | Extras |
|---|---|---|---|
| `POST /api/dev/setup` | `routes/dev.ts:13` (mounted first) | verifies password, 401 `INVALID_PASSWORD` on mismatch | `plan='free'`, no role, returns password in body |
| `POST /api/dev/setup` | `worker.ts:102` | no password check | role `superadmin`, no plan |
| `POST /api/auth/dev/setup` | `routes/auth.ts:704` | no password check | role `superadmin`, **returns JWT pair** |

Hono matches in registration order: `app.route('/api/dev', devRoutes)` at
worker.ts:99 wins, so the worker.ts:102 duplicate is **dead code**. The
`/api/auth/dev/setup` variant is the only one that returns a JWT directly.

**Cerberus consequence:** the live `/api/dev/setup` (dev.ts) returns no JWT —
protected HTTP routes still need `POST /api/dev/login` (dev.ts:112), matching
the live-port/auth gotchas memory. Tests must not assert on the worker.ts
variant's response shape.

## 3. auth/sessions + auth/tokens routes defined but unmounted

`routes/auth/index.ts` combines `authRoutes` + `sessionsRoutes` +
`tokenRoutes` (auth/sessions.ts, auth/tokens.ts). But `worker.ts:4` imports
`from './routes/auth'`, which resolves to `routes/auth.ts` (file beats
directory) — so `/api/auth/sessions` and `/api/auth/tokens` are **never
mounted in production**; only `test/routes/auth-{sessions,tokens}.test.ts`
import them directly. Session listing/revocation and API-token management are
unreachable via the deployed worker.

**Cerberus consequence:** HTTP surface extraction should not count
`/api/auth/sessions*` or `/api/auth/tokens*` as reachable claims; negative
cases against them would 404 for routing reasons, not auth reasons.

## 4. `workflow:*` vs `multiagent:*` naming fork

The same subsystem carries three names depending on layer:

- WS message types: `workflow:*` (room.ts whitelist, bridge.go handleMessage)
- DB tables: `multiagent_missions`, `multiagent_tasks`
- HTTP routes: `/api/missions`, with a legacy 301 redirect
  `/api/workflows/jobs/* → /api/missions/*` (worker.ts:306)

**Cerberus consequence:** vocab/protocol modeling must map
`workflow:task_assign` (wire) ↔ `multiagent_tasks` (storage) ↔ `/api/missions`
(HTTP) as one concept. Coverage attribution keyed on literal prefixes will
under-count. The legacy redirect means both prefixes appear in logs.

---

## Re-verification

All file:line references checked 2026-08-16 against
`/home/mason/Documents/code_projects/private/open-agents` working tree.
If open-agents refactors, re-run the greps in this doc's history before
trusting the consequences.
