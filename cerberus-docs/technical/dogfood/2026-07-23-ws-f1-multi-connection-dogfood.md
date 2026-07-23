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

The `web` connect's optional handshake awaits the peer-join signal; it times out
after 2 s (a lone web client gets silence) and the connection stays alive
(F2 + read-pump), exactly as in the unit test. Once the bridge connects, the DO
relays the peer-join signal to `web`, which the third step MATCHES:

```
ws_receive: ws ok   (matched=1)   # device:online relayed bridge-join -> web
ws_send:     ws ok                 # session:start sent from web
ws_receive: ws error (seen=1)      # server rejected the minimal session:start (Finding 4)
```

So the cross-socket relay is proven end-to-end on real traffic (bridge-join → DO →
`web` receives the relayed `device:online`), and the hard capability assertion
(`len(Evidence) >= 3`, both connects) holds.

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

3. **The relay peer-join signal is `device:online`, not `devices:sync`.** When
   the bridge connects, the DO pushes
   `{"type":"device:online","payload":{"deviceId":...,"deviceName":...},"timestamp":...}`
   to the web socket. The Tier-2 doc's `devices:sync` was inaccurate for this dev
   build — the real signal is `device:online` (singular, per joined device). With
   the test awaiting `device:online`, the receive MATCHES (`matched=1`): the
   cross-socket relay is proven end-to-end (bridge-join → DO → web receives the
   relayed signal). The test logs observed frames (`SeenMessages`) so future
   dogfood runs surface the actual wire content, not just match/seen counts.

4. **`session:start` needs a richer payload.** Sending the minimal
   `{"type":"session:start"}` is rejected:
   `{"type":"error","payload":{"code":"MESSAGE_PROCESSING_ERROR","message":"Failed to process message"}}`.
   The real `session:start` requires protocol-specific fields (session config).
   This is open-agents protocol detail — exactly the message-body understanding
   the Steer/Scout LLM (or M3-3) supplies at run time, not a cerberus concern. The
   test logs it as a best-effort finding and still passes on the capability
   assertion (both connects + the relay signal).

## Conclusion

- cerberus's multi-connection orchestration works on a real, undocumented
  multi-party relay target: one `Steps` case opened a `web` and a `bridge` socket
  to the same Durable Object, and the relayed peer-join signal (`device:online`)
  was MATCHED on `web` — the cross-socket relay is proven end-to-end.
- The deterministic unit test (`TestRunStepsMultiConnection`) is the mechanical
  proof; this integration run is the real-traffic confirmation.
- What remains open-agents-protocol-specific (not cerberus): the `session:start`
  message body (Finding 4) and the rest of the `session:*` request/reply
  vocabulary. That is exactly the kind of protocol understanding the Steer/Scout
  LLM (or M3-3 `protocol infer`) supplies at run time, not something to bake into
  the executor. F3 (dynamic `/ws/{userId}`) and F4 (batching/type-alias) are
  still deferred; neither blocked this relay proof.
