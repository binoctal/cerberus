# M3-3 `protocol infer` — Signal-Level Dogfood (open-agents) — 2026-07-31

Validated the M3-3 tool-calling pipeline (Tasks 1–4: `argsToProtocol`,
`protocol_draft` tool, rewritten prompt, `DecideWithTools` + three-state errors)
against the real, undocumented `open-agents` WebSocket target. This closes the
M3-3 trigger opened on 2026-07-23 (`2026-07-23-ws-tier2-open-agents.md`), where
authoring a protocol declaration required discovering four structures by hand.

## Setup

- Target up: `fnm use 22 && cd apps/api && npm run dev` (`wrangler dev --port
  8989`). Ready in ~1s on local D1 + the `UserRoom` Durable Object.
- **WS OPEN confirmed** via a deterministic `ws` probe against
  `ws://localhost:8989/ws/demo_user?type=web&token=demo_token`: `OPEN`, then 0
  frames in 8s — the peer-gated silence the 2026-07-23 record established (a
  lone web client gets no `devices:sync`; that is the protocol's defining
  wrinkle, not a defect).
- cerberus binary: `build/cerberus` (built 2026-07-31). Run from the
  `open-agents` repo root so `--from apps/api/src/realtime` resolves.
- LLM credentials are inherited from the host Claude Code session via
  `.claude/settings.json` (`ANTHROPIC_AUTH_TOKEN` + `ANTHROPIC_BASE_URL`, a
  Bearer-token proxy). cerberus's `providerKey("ANTHROPIC")` falls back to
  `ANTHROPIC_AUTH_TOKEN` → `AuthSchemeBearer`, so no extra config was needed.
  Model from `project.yaml`: `claude-sonnet-4-6`.
- `.cerberus/project.yaml` (in the open-agents repo, untracked) was extended
  with name-only actors `web`, `bridge`, `user`. The original file had no
  actors; `ValidateProtocol` rejects a `credential_ref` that names no actor, so
  actors were added to let a draft through for coverage scoring. `user` covers
  the model's first-attempt naming (see Run 1).

## Run

```
cerberus protocol infer --name open-agents \
  --from apps/api/src/realtime --service api --dry-run
```

`readInputs` is non-recursive: the model saw only `realtime/room.ts` (~25 KB),
`rateLimiter.ts`, and `rateLimiter.test.ts`. Both missing structures live inside
`room.ts`, i.e. **in the model's input scope**.

### Run 1 — three-state error (invalid protocol)

```
Error: model produced an invalid protocol:
  roles["web"].credential_ref "user" does not match any actor
```

This is the desired three-state behaviour: the model emitted a `protocol_draft`
tool call (so `DecideWithTools` + `argsToProtocol` worked), but named the web
connection's credential_ref `"user"` (reading it off `/ws/{userId}` /
`demo_user`). `ValidateProtocol` caught it; the error is clean (no raw LLM
payload leaked). Re-running after adding a `user` actor produced Run 2. **The
name variance across runs is itself a finding** — see Prompt iteration points.

### Run 2 — full draft (EXIT 0)

```yaml
framing: json
type_path: type
auth:
  strategy: query
  param: token
  credential_ref: ""
roles:
  bridge:
    credential_ref: ""
    params:
      deviceId: '{{deviceId}}'
      type: bridge
  web:
    credential_ref: ""
    params:
      type: web
```

## Per-structure coverage

Scored against the four in-scope structures the 2026-07-23 record discovered by
hand:

| Structure | Expected (manual discovery) | Drafted? | Note |
|---|---|---|---|
| Envelope / `type_path` | `{type,payload,timestamp}` → `type` | **yes** | `framing: json`, `type_path: type` |
| Multi-role | `web` + `bridge` | **yes** | Both roles, with correct discriminator params (`type:web`/`type:bridge`) and `deviceId` on bridge |
| Conditional handshake | `devices:sync`, optional (peer-gated) | **no** | No `handshake` block emitted |
| Batching | `session:output-batch` → item_type `session:output`, items_path `payload.lines` | **no** | No `batches` block emitted |

Auth (`?token=`) is also correct: `auth: {strategy: query, param: token}`.

**Score: 2/4 in-scope structures** (envelope + multi-role). Handshake and
batching were both missed.

## Why the two structures were missed

Both are present in `room.ts` and were inside the model's input — these are
**recognition gaps, not input-scope gaps**:

- **Batching** — `room.ts:39-40, 341-344, 474-499`: `session:output` routes to
  `batchOutput`, which starts a 50ms `setTimeout`; `flushBatch` then emits
  `{type: 'session:output-batch', payload: {sessionId, lines, timestamp}}`. The
  batch routing key, `item_type` (`session:output`), and `items_path`
  (`payload.lines`) are all literally in the source. The model did not map this
  timer+flush pattern onto the `batches` tool field.
- **Conditional handshake** — `room.ts:200-220`: on a web connect the server
  builds `onlineDevices` from attached bridge sockets and **only when
  `onlineDevices.length > 0`** sends `{type: 'devices:sync', ...}`. The model
  read the code (it is the same `room.ts` that yielded the roles) but did not
  classify the conditional post-connect send as a `handshake`.

## Pipeline verdict (Tasks 1–4)

The M3-3 pipeline works end-to-end on a real target:

- `DecideWithTools` returned a `protocol_draft` tool call (no drift).
- `argsToProtocol` assembled a `*project.Protocol` with no parse error.
- `ValidateProtocol` fired on the invalid `credential_ref` (Run 1) and the error
  was the clean three-state message, not `ErrNoProtocol` and not a raw dump.
- On a valid shape (Run 2) the draft printed under `--dry-run` with nothing
  written.

No底层 model change is needed; the remaining gap is prompt-side recognition of
two non-obvious code patterns.

## Prompt iteration points

1. **Inject the actor list into the prompt.** The model cannot know which
   `credential_ref` names ValidateProtocol will accept; it guessed `"user"` once
   and left it blank another time. The prompt should list `cfg.Actors[*].Name`
   so the model picks an existing name (or the prompt iteration should make
   "leave credential_ref blank when unsure" explicit, since blank passes
   validation today).
2. **Strengthen the batching recognition cue.** The current prompt describes
   batching abstractly ("if the server coalesces frames"). It should call out
   the concrete `setTimeout`/timer-flush pattern and point the model at
   `item_type` (the pre-batch type) vs the batch routing key, since both appear
   verbatim in `room.ts` and were still missed.
3. **Strengthen the handshake recognition cue for conditional sends.** The
   prompt mentions a peer-gated handshake, but the model did not connect the
   `if (onlineDevices.length > 0) ws.send(...)` block to `handshake.optional`.
   An explicit cue — "a send right after connect that is guarded by a peer/
   state condition is an `optional: true` handshake" — would help.

These are prompt-only follow-ups; they do not affect the T1–T4 code.

## Path-param note (dynamic `/ws/{userId}` is already supported)

The connection URL embeds a runtime id: `/ws/{userId}`. cerberus already
handles this via the F3 path-param mechanism — no code gap:

- A service URL may declare a `{name}` path segment, e.g.
  `ws://localhost:8989/ws/{userId}`.
- An actor declares `generated_path_params: { userId: uuid }`; session setup
  (`resolveGeneratedPathParams`) synthesizes a uuid and merges it into
  `Credentials.PathParams`.
- At WS connect, `resolveURLParams` (`internal/head/agent/websocket.go:706`)
  substitutes `{userId}` (both raw and percent-encoded forms) from PathParams.
  A leftover placeholder is a hard error (clear failure over a silent wrong
  dial).

This is exercised by `TestConnectTemplatedURL` and the no-auth
generated-path-param test, so the `/ws/{userId}` shape is covered, not
theoretical.

For an open-agents integration the remaining gap is **config, not code**:

- `services.api.url` is currently `http://localhost:8989` (no `/ws/{userId}`
  path, no placeholder). It needs `ws://localhost:8989/ws/{userId}`.
- The `web` actor needs `generated_path_params: { userId: uuid }` (or a static
  captured value).
- Caveat: a uuid `userId` dials successfully (the dev-token backdoor accepts
  any userId) but lands in an empty `UserRoom` Durable Object — open-agents
  shards by userId, so `demo_user`'s pre-seeded data is in a different shard.
  Tests needing existing data should use a static `demo_user`; tests that drive
  their own state (e.g. via a bridge) can use a generated uuid.

This is an actor/connection-layer concern, **not** a `protocol infer` gap:
`Infer` infers wire shape, and a URL path segment is not part of that.

## Outcome

- The M3-3 tool-calling migration (T1–T4) reaches a real undocumented WS target
  and produces a validated protocol draft; the three-state error path and the
  clean draft path both behaved as designed.
- Coverage is 2/4 on the in-scope structures: envelope and multi-role are
  inferred correctly; conditional handshake and batching are missed despite being
  in the input — a prompt-recognition gap, with concrete iteration points above.
- The M3-3 trigger (real discovery cost) is addressed: the blank-page cost of
  authoring `type_path`, roles, and auth is removed; the remaining handshake/
  batch authoring is a smaller, targeted prompt improvement.

## Prompt iteration — 2026-08-01

The three prompt iteration points above were applied to `buildInferPrompt`
(`internal/protocoldiscover/infer.go`): (a) the declared actor names are now
injected so `credential_ref` picks a real actor; (b) the batching cue names the
`setTimeout`/timer-flush-to-a-different-routing-key pattern; (c) the handshake
cue names the conditional/peer-guarded-send pattern (`if (peers.length > 0)
ws.send(...)` → `optional=true`). The binary was rebuilt and the dogfood
re-run twice against the same target.

### Run 3 (strengthened prompt)

```yaml
framing: json
type_path: type
auth: { strategy: query, param: token, credential_ref: "" }
roles:
  bridge: { credential_ref: user, params: { deviceId: "", type: bridge },
            handshake: { await_type: session:output-batch, timeout: 5000, optional: true } }
  web:    { credential_ref: user, params: { type: web },
            handshake: { await_type: device:online, timeout: 5000, optional: true } }
batches:
  session:output-batch: { item_type: session:output, items_path: lines }
```

### Run 4 (strengthened prompt, variance)

```yaml
framing: json
type_path: type
auth: { strategy: query, param: token, credential_ref: "" }
roles:
  bridge: { credential_ref: bridge, params: { deviceId: "", type: bridge } }
  web:    { credential_ref: user, params: { type: web } }
batches:
  session:output-batch: { item_type: session:output, items_path: payload.items }
```

### Updated coverage

| Structure | Before | After iteration |
|---|---|---|
| Envelope / `type_path` | yes | yes (stable) |
| Multi-role | yes | yes (stable) |
| Batching | **no** | **yes (stable, 2/2)** — `item_type` correct; `items_path` imprecise (`lines` / `payload.items` vs `payload.lines`) |
| Conditional handshake | **no** | **partial, unstable (1/2)** — `optional=true` correct when present, but `await_type` wrong (`device:online` vs `devices:sync`); bridge over-fitted a batch key as its handshake |

### Verdict

The batching cue (`setTimeout` flush) **stably closed** that gap: both runs
emit a correct `session:output-batch` decomposition. The handshake cue
(`guarded` send) **partially worked**: it produced an `optional:true`
handshake once, but the `await_type` was hallucinated and the second run
dropped the handshake entirely — peer-gated conditional sends remain the hard
case.

### Remaining iteration points

1. **Handshake stability + `await_type` accuracy.** The model grasps
   "optional handshake" conceptually but invents the await type instead of
   copying the literal routing key the guarded send emits (`devices:sync`,
   `room.ts:212`). A cue like "set await_type to the EXACT `type:` value of the
   guarded send" or a short worked example may help.
2. **`items_path` precision.** Both runs got the array field but mangled the
   dotted prefix (`lines` / `payload.items` vs `payload.lines`). The cue should
   stress "the FULL dotted path from the frame root to the array".
3. **Role ↔ handshake binding.** Run 3 attached a handshake to `bridge` using a
   batch routing key — the model conflates "message after connect" with
   "frequent message". Scoping handshake to "a send in the connect/open handler,
   not in the message handler" would reduce the over-fit.

The pipeline (T1–T4) needed no change; these are prompt-only refinements.

## Value-accuracy pass — 2026-08-01

A second prompt pass targeted the three remaining value-precision gaps:
handshake `await_type` must be the **verbatim** `type:` literal of the guarded
send (not paraphrased); the handshake must bind to the **connect/open handler**,
not the message handler; and `items_path` must be the **full dotted path from
the frame root**. Four runs were sampled (Runs 5–8):

| Run | Outcome | items_path | handshake await_type |
|---|---|---|---|
| 5 | full draft | `payload.lines` ✅ | `session:state` / `device:online` ✗ |
| 6 | `found=false` (false negative — room.ts is a WS DO) | — | — |
| 7 | partial (roles + auth only) | — | — |
| 8 | `could not parse model output` (malformed args) | — | — |

### Findings

- **`items_path` improved.** In Run 5 (the only run that emitted a batch) the
  value was the correct `payload.lines`, versus `lines` / `payload.items` before
  the frame-root cue. Sample size is one (variance swallowed the rest), so this
  is suggestive, not conclusive.
- **`await_type` did not improve.** Across every run that emitted a handshake
  the model invented a plausible-but-wrong type name instead of copying the
  source's `devices:sync` (`room.ts:212`). The verbatim cue is insufficient:
  the model does not reliably re-localize the guarded-send literal.
- **Variance dominates.** Four runs produced four different shapes — a correct
  draft, a false `found=false`, a partial draft, and a parse failure. This is
  the single biggest quality risk for `protocol infer`; it is not a cue-word
  problem.

### Conclusion

Prompt copy edits have hit diminishing returns: `items_path` likely improved,
but `await_type` did not, and run-to-run variance (1/4 false negative, 1/4
parse failure) swamps both. Reliable value accuracy likely needs an
architecture change rather than further prompt wording:

- **N-sample voting / best-of-N** — run the draft a few times and merge or pick
  the consensus, absorbing the false-negative/parse-failure tails.
- **Two-step extraction** — first locate the guarded send and the flush call in
  the source (grounding), then read the exact literals off that anchored span,
  instead of asking one shot to both find and transcribe.
- **Few-shot** — one worked example of "transcribe the `type:` literal verbatim"
  may teach the behavior the verbatim cue could not.

These are out of scope for the current task and left as follow-ups. The T1–T4
pipeline and the structure-recognition gains (batching stable in the prior
pass) stand; the open work is value accuracy under variance.
