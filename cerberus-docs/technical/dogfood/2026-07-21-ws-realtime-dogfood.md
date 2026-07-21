# WS Realtime Tier-1 Dogfood — 2026-07-21

Ran the released M0–M3-1 WS engine end-to-end against a minimal self-authored
target to validate the engine on live traffic and collect the M3-2/M3-3 trigger
signals. The dogfood also surfaced one shipped M3-1 bug (fixed) and several
orchestration findings.

## Setup

- Target: `dogfood/ws-realtime/` — `POST /login` (issues a token) + `WS /realtime`
  (validates `?token=`, sends `devices:sync`, replies `device:ack` to
  `device:command`). Mirrors open-agents' login+realtime shape.
- Config: `dogfood/ws-realtime/.cerberus/project.yaml` declaring the `realtime`
  service via `protocol_ref: open-agents` (standalone file) + a `web-actor` with
  an `auth:` login flow. `.cerberus/protocols/open-agents.yaml` declares json
  framing, query-token auth, a `web` role with a `devices:sync` handshake.
- Run: `./build/cerberus run --config dogfood/ws-realtime/.cerberus/project.yaml
  --dir dogfood/ws-realtime --goal "…connect to /realtime, send device:command,
  verify device:ack payload.approved is true."`
- LLM creds/model tiers inherited from `.claude/settings.json`: scout=GLM-5.1,
  **agent (Steer)=GLM-4.5-Air**, examiner=GLM-5.1, critic=glm-5.2[1m].
- One primary (M1-path) run completed: 1m24s, ~17K tokens, 8 cases (Scout-planned).

## Finding 0 (P1, fixed during harness build): M3-1 `protocol_ref` broken at the documented config location

While building the harness, `TestProjectConfig_Loads` failed:
`services[0]: protocol_ref "open-agents": open .cerberus/.cerberus/protocols/open-agents.yaml: no such file`.

Root cause: `LoadFromFile` used `filepath.Dir(configPath)` as `baseDir`, and
`resolveProtocolRefs` joins `baseDir/.cerberus/protocols/<name>.yaml`. With the
config at the documented/default `.cerberus/project.yaml`, `baseDir` = `.cerberus`,
so the path doubled to `.cerberus/.cerberus/protocols/`. M3-1's own unit tests
missed this because they write the config at `<tmpdir>/project.yaml` (project
root), where `Dir` is already the root — exactly the deferred "resolved-from-file
drives BuildWSProtocolIndex" integration gap.

Fix (`a422efe`): `LoadFromFile` computes `baseDir` as the project root (one level
up when the config dir's base is `.cerberus`); env-overlay sibling lookup still
uses the config dir. Regression test
`TestLoadFromFile_ProtocolRefResolvesAtCerberusConfigLocation`; existing
root-layout tests unchanged. Validated end-to-end by this dogfood's run loading
the config cleanly. (opus Minor: heuristic degenerates only if a project root is
itself named `.cerberus`.)

**Lesson:** a config-loading path derived from the config's own location must be
exercised at the *documented* location, not only at the layout the unit tests
happen to use. The deferred integration test was the right instinct.

## Finding 1 (confirmed working): engine wiring + auth chain on a real run

- All four WS action types registered (`ws_connect/send/receive/disconnect`).
- `auth flow resolved actor:web-actor` — the `auth:` login ran at session setup,
  resolved the token, populated `RawToken`. The subsequent `ws_connect`
  (`success:true`) proves the **auth_flow → rawToken → query strip-then-inject**
  chain works end-to-end on live traffic — the integration no prior dogfood or
  unit test exercised (the self-dogfood ran local-mode with no services/auth).
- `protocol_ref` standalone-file loading drove the engine (M3-1 real-run path,
  after the Finding 0 fix).

This closes the core "does the released engine work through the full
Scout→Agent→Examiner pipeline" question for the deterministic mechanics tested.

## Finding 2 (P2, latent defect): steer prompt lists `ws_*` in prose but not in the action-type enum

`prompts.go` describes the WS primitives (lines 21-37) but the RULES action-type
list (line 7) and the output JSON schema `action.type` enum (line 43) enumerate
only `api_request|navigate|wait|…` — no `ws_*`. **This did NOT block the run**:
the Steer LLM still emitted `ws_connect`/`ws_receive` (Finding 3), so the prose
section sufficed for this model. But the inconsistency is a real latent defect —
a stricter model could refuse to emit unlisted action types. Low priority; fix is
to add `ws_*` to both the RULES list and the output enum (single raw-string,
inline edit).

## Finding 3 (P1 — M3-2 trigger): Steer-LLM WS orchestration is unreliable

The single run produced decisive M3-2 evidence:

- **Within-session drift on the same target.** Scout planned three cases against
  `/realtime` (tc-001/002/004). The Steer LLM used `ws_*` for tc-001 but
  `api_request` (HTTP, 426 death-loop) for tc-002 and tc-004 — same target, same
  goal, different action-type choice. The recovery agent correctly diagnosed
  "426 Upgrade Required → needs WebSocket" yet the Steer loop did not switch to
  `ws_connect` for those cases.
- **Broken sequencing.** In tc-001 the LLM emitted `ws_connect` (success) →
  `ws_connect` again (success) → `ws_receive` (fail). It never sent
  `device:command`, so no `device:ack` could arrive. It connected twice instead
  of connect→send→receive.

This is exactly the orchestration unreliability M3-2 (deterministic Scout WS-case
skeleton) exists to remove: today every run depends on the Agent LLM (here
GLM-4.5-Air) re-deriving the WS sequence, and it does so inconsistently. **M3-2
is now trigger-justified.**

## Finding 4 (P2 — candidate engine/usability bug): `ws_receive` instant-fail (2µs)

tc-001 attempt 3 `ws_receive` returned `success:false` in ~2µs — an immediate
error, not a 10s timeout. Most likely cause: the LLM omitted `connection_id` on
`ws_connect` (so the executor assigned an auto id `ws-<seq>`), then referenced a
connection the table did not know (e.g. an invented `conn1`) on `ws_receive`,
which fails fast at the lookup. The steer prompt tells the LLM to "reuse the same
connection_id," but when the LLM omits it, the auto-assigned id is not visible to
it in the result, so it cannot reference it on subsequent send/receive.

Not confirmed from the log alone (the terse steer line carries no error text);
needs a verbose run or a code read of `doReceive`'s not-found path. Candidate
fix: echo the assigned `connection_id` in `WSResult` so the LLM can reuse it.
Recorded for follow-up; not blocking.

## Finding 5 (P2): Examiner false-pass on a WS case that never reached WS

tc-002 was judged `pass` at correctness **0.98** despite every action being a
failing `api_request` (HTTP 426) — it never opened a WebSocket. The Examiner
credited the case without recognizing that no WS verification occurred. Echoes
the 2026-07-18 self-dogfood verdict/summary mismatch. The Examiner prompt lacks
WS-specific signal (e.g. "a pass requires an actual `ws_receive` of the awaited
type"). Recorded for follow-up.

## M3-3 signal: hand-authoring the declaration was cheap — not triggered

Authoring `open-agents.yaml` (framing/type_path/auth/roles/handshake) was
straightforward and quick; the field set is small and documented. The blank-page
cost that M3-3 (`protocol infer`) exists to remove did not materialize for this
representative protocol. **M3-3 is not trigger-justified by this dogfood** (would
re-evaluate against a real, undocumented target at Tier-2).

## Outcome

```
Verdicts: 4 pass, 3 fail, 1 skip, 0 uncertain   |   ~17K tokens   |   1m24s
```

The 4 "pass" are the 3 Go exec cases (`go build/test/vet` against the dogfood
dir) + tc-002's false-pass (Finding 5). The genuine WS exercise was minimal: one
case (tc-001) reached `ws_connect` but never completed a send→receive→assert.

## Resolution (2026-07-21)

- **Finding 0 — fixed** (`a422efe`, merged to main): `protocol_ref` resolves from
  the project root; regression test added.
- **Finding 1 — confirmed:** engine + auth chain + `protocol_ref` work on a real
  run (the integration gap is closed).
- **Findings 2/4/5 — recorded, not fixed:** prompt enum (F2), `ws_receive`
  instant-fail (F4), Examiner WS false-pass (F5). Each is a candidate follow-up.
- **Finding 3 — M3-2 trigger justified:** deterministic Scout WS-case generation
  (the M3-2 spec/plan already written, implementation was dogfooding-gated) now
  has its trigger evidence.
- **M3-3 — not triggered** by this dogfood.

## Confirmed

- cerberus loads a `protocol_ref` project, runs auth_flow at setup, injects the
  raw token into the WS query, and the engine accepts the connection —
  end-to-end on a live target (M0–M3-1 released engine validated).
- WS testing is Steer-LLM-only today and that orchestration is not reliable
  (drift + broken sequencing) — the M3-2 deterministic-case-skeleton milestone is
  the correct next step.
- A minimal login+WS mirror target is an effective, zero-infra dogfood harness;
  it is reusable for Tier-2 (real open-agents) and for regression.

## Deferred / not run

- M2 goal-hinted role run (role-expansion + auto-handshake exercise) and the
  cross-run drift replays were not run — the single M1 run already produced
  decisive M3-2 signal (within-session drift + broken sequencing). Re-run on
  request.
- The ws_receive instant-fail (Finding 4) root-cause needs a verbose run or a
  `doReceive` code read.
