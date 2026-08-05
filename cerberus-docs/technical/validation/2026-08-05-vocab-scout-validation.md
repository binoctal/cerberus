# Scout Vocabulary Value — Validation Result (2026-08-05)

## Setup
- Config: `dogfood/ws-realtime/.cerberus` (real open-agents vocab, **67 distinct edge types** after dedup, 70 raw `type:` lines).
- Planner: ToT (`DefaultToTConfig`), `N=3` runs per condition.
- Model: `glm-5.2[1m]` via GLM relay (`https://open.bigmodel.cn/api/anthropic`, bearer auth) — credentials resolved from `.claude/settings.json` env, same path the production binary uses.
- Goal: *“Cover the realtime WebSocket service's message relay between web and bridge actors: session lifecycle, bridge join/leave signaling, and workflow task progress broadcast. Author WS choreography that drives messages from each role and asserts what each peer receives.”*
- Each run also includes one deterministic seed case (`ws-realtime-web-connect`, a single `ws_connect` step).

## Type-hit summary

Raw counts from the classifier (`extractTypeTokens` over the full JSON dump → `classifyTypes` vs the real vocab set):

| condition | run | cases | tokens | hits | invented | invented-list (raw) |
| --- | --- | --- | --- | --- | --- | --- |
| with-vocab | 1 | 7 | 13 | 11 | 2 | `[workflow:task_g workflow:task_gu]` |
| with-vocab | 2 | 8 | 16 | 15 | 1 | `[workflow:task_]` |
| with-vocab | 3 | 9 | 16 | 13 | 3 | `[ermission:request session:t session:teleport]` |
| without-vocab | 1 | 7 | 0 | 0 | 0 | `[]` |
| without-vocab | 2 | 8 | 0 | 0 | 0 | `[]` |
| without-vocab | 3 | 8 | 0 | 0 | 0 | `[]` |

Total wall-clock: 1409 s (~235 s per run).

## Measurement caveat — almost all “invented” tokens are Name-field truncation

Inspecting the dumps shows the apparent inventions are **not model fabrication**. ToT writes a per-case `name` field truncated to ~60 characters with a trailing `…`. The extractor scans the whole dump, so it matches the truncated tail of a real type:

| Raw invented token | Source (truncated `name`) | Real type it came from |
| --- | --- | --- |
| `workflow:task_gu` / `workflow:task_g` | `“…Web sends workflow:task_gu…”` | `workflow:task_guidance` (hit elsewhere) |
| `workflow:task_` | `“…Web sends workflow:task_…”` | a `workflow:task_*` type (hit) |
| `ermission:request` | `“…ermission:request…”` (leading `p` cut) | `permission:request` (hit) |
| `session:t` | `“…session:t…”` | `session:teleport` / `session:takeover` tail |

After discounting truncation, the **only genuine fabrication across all 3 with-vocab runs is `session:teleport`** (run 3, one occurrence). Real with-vocab inventions ≈ **0–1 per run**, against 11–15 hits — a hit rate of ~94–99 % on concrete typed messages.

## Commonality (≥2 of 3 runs)
- **With-vocab invented (common):** none stable. Truncation noise varies by run; `session:teleport` appears once. No recurring fabricated type.
- **Without-vocab invented (common):** none — because **no `namespace:action` tokens are emitted at all** (see below).

## Choreography observations

**With-vocab** — the planner emits concrete, addressable WS choreography using the real protocol vocabulary: `workflow:task_guidance`, `workflow:task_assign`, `workflow:task_started`, `workflow:task_progress`, `workflow:task_status_update`, `workflow:pause`/`workflow:cancel`, `session:resize`, `session:stop`/`session:stopped`, plus role/routing-aware assertions (`broadcast_web` fan-out to N peers, `send_bridge_by_device` route failure on null `deviceId`). Cases target realistic concerns: guidance injection at session teardown, task-assignment thrashing, interleaved resize with active progress broadcast, 50-peer fan-out. Routing direction is correct (web→bridge via workflow actions; bridge→web via broadcasts).

Representative (verbatim target, run 1):
> *“interleaved session:resize with workflow:task_progress: while task progress events are actively broadcasting (every 200ms), web sends session:resize. assert both event streams coexist without message corruption…”*

**Without-vocab** — the planner stays at **abstract prose**. All 3 runs produced zero concrete WS steps (`ws_send`/`ws_receive` counts = 0 in every dump) and zero `namespace:action` messages. Message names are written with **hyphens, not the protocol colon namespace** — `workflow-task-subscribe`, `bridge-joined`, `task-progress`, `session-established`, `bridge-left` — so they do not even parse as protocol types. Cases describe behavior in natural-language role/capability terms (“receives role and allowed capabilities (relay, subscribe, broadcast)”) rather than drivable typed choreography. The only concrete step across all without-vocab dumps is the deterministic seed case.

Representative (verbatim target, run 1):
> *“web client sends a workflow-task-subscribe message after session establishment and receives a subscription-confirmed response; bridge then posts a task-progress event…”*

## Conclusion

**Meets success criteria.** Vocabulary injection produces a clear, repeatable quality lift:

1. **Concreteness.** With vocab, every run emits 11–16 real protocol message types as drivable `namespace:action` choreography (hit rate ~94–99 %). Without vocab, the planner emits **zero** typed messages in all 3 runs — it cannot reconstruct the protocol’s colon-namespaced vocabulary from the goal alone and falls back to hyphenated prose.
2. **Fidelity.** With-vocab inventions are effectively zero (the one real fabrication, `session:teleport`, did not recur). The raw invented counts overstate fabrication because the extractor matches the truncated `name` field; a follow-up should exclude `name` from the scan (see below).
3. **Routing awareness.** With vocab, cases reference real route resolution (`send_bridge_by_device`, `broadcast_web`) and correct web↔bridge direction; without vocab, cases are role/capability prose with no routing detail.

The Scout (ToT) planner derives substantial value from the injected vocabulary: it converts abstract coverage goals into concrete, protocol-faithful WS choreography. No prompt rework needed for the ToT path.

### Follow-ups
- **Extractor noise (measurement only — no production impact):** exclude the truncated `name` field (or scan only `target`/`expectation`/`steps`) so invented-list reflects true fabrication. The validation verdict is unchanged either way.
- **Wire vocab into Agent/Examiner:** this run validates the Scout/plan path only. Agent execution and Examiner judging also consume protocol types; confirming they route on the same vocabulary is the natural next validation.
- **Optional extension:** exercise the direct planner (drop `SetDeepPlan`) under the same two conditions — deferred because the ToT result is decisive.
