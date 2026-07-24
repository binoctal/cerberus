# Scout Relay Generation — Live-LLM Dogfood Procedure — 2026-07-24

The Scout relay generation feature (merged `d452d42`) is fully tested at the
deterministic core: `expandWSRelayCases` is a pure function with unit + composition
tests, and the F1 integration test proved the executor runs the expanded multi-
connection `Steps` on real open-agents traffic. The one unvalidated piece is the
**A1 risk**: does the Scout planning LLM actually emit a well-formed `ws_relay`
intent for a relay goal? That needs a real LLM run, which requires the user's
environment (`ANTHROPIC_API_KEY`). This is the procedure to run it there.

## Prerequisites

- `ANTHROPIC_API_KEY` exported (cerberus Scout planning uses it; shared with Claude
  Code).
- open-agents dev server up: `fnm use 22 && cd ../open-agents/apps/api && npm run dev`
  (serves `:8989`). Bring up in its own process group (`setsid … &`) and stop by
  process group; never `pkill -f` from a bash whose argv contains the pattern.
- Fresh cerberus binary: `make build` (the released binary must include Scout relay
  generation, `d452d42`+).

## Provision credentials (one-off per device)

`POST /api/dev/setup` (CSRF requires an `Origin` header; discovered in the F1
dogfood) returns `config.{userId, deviceId, deviceToken}`:

```bash
curl -s -X POST -H "Content-Type: application/json" -H "Origin: http://localhost:8989" \
  -d '{}' http://localhost:8989/api/dev/setup
# -> {"config":{"userId":"user_…","deviceId":"device_…","deviceToken":"token_…"}, …}
```

- `web` authenticates with the dev backdoor `demo_token` (any userId) — no flow.
- `bridge` authenticates with `deviceId` + `deviceToken` (the Worker DB-validates
  `devices WHERE id & user_id & device_token`).
- `userId` is shared by both sockets (same Durable Object → the relay).

## Representative project config

`.cerberus/project.yaml` (sketch — adapt actors/credentials to your layout):

```yaml
project:
  name: open-agents-relay
services:
  - name: realtime
    # Bake the provisioned userId here (web's demo_token path has no auth flow to
    # capture it from), OR use F3 {userId} templating if the connecting actor's
    # auth flow captures it (see F3 design). Baking is simpler for the backdoor.
    url: http://localhost:8989/ws/user_PROVISIONED
    protocol:
      framing: json
      auth: { strategy: query, param: token, credential_ref: bridge-actor }
      roles:
        web:    { credential_ref: web-actor,    params: { type: web },
                  handshake: { await_type: device:online, optional: true, timeout: 2 } }
        bridge: { credential_ref: bridge-actor, params: { type: bridge, deviceId: device_PROVISIONED } }
actors:
  - { name: web-actor,    credentials: { headers: {} } }      # token injected as ?token=demo_token
  - { name: bridge-actor, credentials: { headers: {} } }      # token injected as ?token=<deviceToken>
```

(For `web-actor`/`bridge-actor` tokens: put `demo_token` / the provisioned
`deviceToken` in `.cerberus/credentials.yaml`, or give each an auth flow that
resolves them. The F3 `{userId}` path-param templating applies when an actor's auth
flow captures `userId` — useful if you give `bridge-actor` the `/api/dev/setup`
flow with `path_params: {userId: config.userId}` and a `{userId}` service URL.)

## Run + inspect

Run Scout planning against a relay goal and inspect whether it emitted a
`ws_relay` case:

```bash
./build/cerberus run --goal "the web client connects, the bridge client connects to the same user, and the web client receives the device:online signal relayed when the bridge joins" --verbose
```

What to look for in the plan/case output:

1. **A `ws_relay` case was generated** (action `ws_relay`, body `{roles, steps}`)
   and **expanded** into a multi-connection `Steps` case (`ws_connect` × roles →
   ordered `ws_send`/`ws_receive`). The expander logs "expanded ws_relay cases"
   at augment time.
2. **Both connects succeeded** on real traffic (`ws ok … type=web` and
   `… type=bridge` to the same `/ws/<userId>`).
3. The relay signal (`device:online`) was **matched** on the web socket.

## Interpretation

- **Well-formed `ws_relay` emitted + expanded + ran** ⇒ Scout relay generation is
  validated end-to-end on real traffic (the A1 risk closed).
- **Malformed/absent `ws_relay`** (the LLM did not emit one, or emitted a bad
  intent) ⇒ the deterministic expander drops it gracefully (case removed, run
  continues; no crash). This is a prompt-tuning signal for the relay bullet in
  `internal/head/scout/prompts.go` (`promptPlanSystem`), not a cerberus defect —
  the expander + executor are independently proven. Tune the bullet wording and
  re-run.

## Known wrinkles (from the F1 dogfood)

- `/api/dev/setup` needs an `Origin` header (CSRF).
- `bridge` needs `deviceId` + `token` (not just `token`) — Worker DB-validates both.
- `session:start` with a minimal body is rejected (`MESSAGE_PROCESSING_ERROR`) —
  the real `session:start` needs a payload; that message-body understanding is the
  LLM's job at runtime, not baked into the executor.
- `web`'s `demo_token` path has no auth flow, so `userId` for the web socket is
  baked (F3 `{userId}` templating serves actors with a flow). For a fully dynamic
  userId, give both actors a flow that captures it.
