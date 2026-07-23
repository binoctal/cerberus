# F1 Multi-Connection Orchestration — open-agents Dogfood — 2026-07-23

Dogfooded cerberus's multi-connection WebSocket orchestration (F1) against a live
`open-agents` target. The capability was first proven deterministically in
`TestRunStepsMultiConnection` (in-process relay); this run validates it on real
multi-party relay traffic via the `//go:build integration` test
`TestRunStepsMultiConnectionOpenAgents`.

## Run

```
go test -tags integration -run TestRunStepsMultiConnectionOpenAgents -v ./internal/head/agent/
--- PASS: TestRunStepsMultiConnectionOpenAgents (5.45s)
```

Target: a running `open-agents` dev server on `localhost:8989` (sibling repo,
`apps/api && npm run dev`). The test provisions a user + bridge device, then
runs one `Steps` case that connects a `web` and a `bridge` socket to the same
`/ws/<userId>` and probes the relay.

## Result

Both connects succeeded — cerberus opened **two real sockets to the same
`UserRoom` Durable Object** in one case:

```
ws_connect: ws ok ws://localhost:8989/ws/user_…?type=web            connection_id=c-web  (2.0s)
ws_connect: ws ok ws://localhost:8989/ws/user_…?deviceId=…&type=bridge connection_id=c-bridge (20ms)
```

The `web` connect's optional handshake awaits `devices:sync`; it times out after
2 s and the connection stays alive (F2 + read-pump), exactly as in the unit test.
The case reaches the third step (the hard capability assertion
`len(Evidence) >= 3` holds), proving both connects succeeded.

## Findings (open-agents specifics discovered at run time)

1. **CSRF requires an `Origin` header.** `POST /api/dev/setup` without an
   `Origin` header is rejected: `{"error":{"code":"CSRF_ERROR","message":"Missing
   origin header"}}`. The test's `devSetup` sets `Origin: http://localhost:8989`.
   (Tier-2 described this endpoint but did not exercise the POST — its empirical
   test used only the web `demo_token` backdoor.)

2. **Bridge auth needs `deviceId` + `token` (not just `token`).** The Worker
   DB-validates a `type=bridge` connection as
   `devices WHERE id = deviceId AND user_id = userId AND device_token = token`
   (`apps/api/src/worker.ts:359-367`). A bridge connect with only `type`+`token`
   (no `deviceId`) is rejected at ~127 ms. The bridge role therefore carries
   `deviceId` (from `/api/dev/setup`'s `config.deviceId`) as a discriminator
   param alongside the auth token. `web` needs none of this — `demo_token` is a
   dev backdoor accepted for any userId.

3. **The relay pushes a frame to `web` on bridge-join, but not `devices:sync`.**
   The `ws_receive` awaiting `devices:sync` timed out with `seen=1`: exactly one
   frame arrived on the web socket after the bridge connected, but it did not
   match the `devices:sync` type. cerberus observed the relay deliver a frame to
   `web`; the exact type/shape of that peer-join signal in this dev build differs
   from the Tier-2 description (or routes differently) and was not captured (the
   test logs match/seen counts, not frame contents). Pinning it down is follow-up
   dogfood work and partly F4 (batching/type-alias) territory.

## Conclusion

- cerberus's multi-connection orchestration works on a real, undocumented
  multi-party relay target: one `Steps` case opened a `web` and a `bridge` socket
  to the same Durable Object and observed the relay push a frame across them.
- The deterministic unit test (`TestRunStepsMultiConnection`) is the mechanical
  proof; this integration run is the real-traffic confirmation.
- The open-agents relay's exact message vocabulary (the peer-join signal type,
  `session:*` request/reply) remains a best-effort dogfood area — exactly the
  kind of protocol understanding the Steer/Scout LLM (or M3-3 `protocol infer`)
  is meant to supply at run time, not something to bake into the executor.
