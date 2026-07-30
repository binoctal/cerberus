# WS-Aware Repair Loop — Design Spec (D2)

> Feature: D2 — make the Examiner→Scout repair loop WebSocket-aware. Status:
> design spec, 2026-07-31. Depends on the repair loop (#3) and the WS subsystem
> (shipped). Strategic basis: `2026-07-30-examiner-replan-autotest-coverage-thinking.md` §2 (D2).

## 1. Problem

The repair loop is **HTTP-only end-to-end**:

- **Hint vocabulary** (`agent/types.go`): `endpoint_drift | auth | shape | none` —
  all HTTP/endpoint-flavoured. WS failures (handshake await mismatch, wrong
  send envelope, decisive-receive no-match, match_all item violation) get
  shoehorned into `shape` or emit `none` and short-circuit — so the loop rarely
  engages on the WS subsystem.
- **Repair tool** (`scout/repair_plan.go`): `repair_case` carries only HTTP
  fields (`method/path/service/body/expectation`). `repairCaseFromCall` builds a
  TestCase with `Target/Method/Body` and **drops `Steps`**. A failed WS case
  (which carries `Steps []TestStep`) is therefore "repaired" into an HTTP-shaped
  case — silently broken. WS cases literally cannot be repaired today.

So D2 has two halves: (a) a WS hint taxonomy so the judge can diagnose WS
failures correctably, and (b) WS repair emission so Scout can produce a
corrected WS flow.

## 2. Goal

Close the repair loop over WS: a failed WS step is diagnosed with a WS
`redispatch_hint`, and Scout emits a corrected WS TestCase (with `Steps`)
targeting the diagnosed step field. HTTP repair is unchanged.

## 3. Non-goals

- Do **not** change WS execution, the read pump, framing, or match semantics.
- Do **not** add new WS step verbs (ws_connect/send/receive/disconnect stay).
- Do **not** change repair-loop control flow, eligibility, or termination
  (those are hint-agnostic; a WS hint flows through exactly like an HTTP hint).
- Do **not** redefine the `coverage` hint (D1) — orthogonal.

## 4. Decisions (pinned)

| Decision | Choice |
|---|---|
| WS hint taxonomy | Add THREE hints: `handshake`, `ws_shape`, `ws_match` (see §5.1). Connection/URL/role dial failures reuse `endpoint_drift` (still "wrong endpoint"). |
| Four surfaces | enum (types) + judge tool schema + judge prompt + parser all gain the WS hints in lockstep. |
| Repair emission | `repair_case` gains an optional `steps` array; when present, `repairCaseFromCall` builds a WS TestCase (`Steps`, `Action`/`Target` from the flow) and HTTP fields are ignored. When absent, HTTP repair is unchanged. |
| Repair prompt | One prompt handles both: for WS hints, instruct correcting the relevant step field (handshake→receive await type; ws_shape→send message; ws_match→receive type/aliases/asserts/match_all). |
| Backward compat | Existing HTTP hints (`endpoint_drift|auth|shape`) and HTTP repair are byte-for-byte unchanged; `none` still means not-correctable. |

## 5. Data Model

### 5.1 New hint values (`internal/head/agent/types.go`)

```go
HintHandshake RedispatchHint = "handshake" // WS: mandatory/role handshake await mismatch
HintWsShape   RedispatchHint = "ws_shape"  // WS: wrong ws_send message envelope/payload
HintWsMatch   RedispatchHint = "ws_match"  // WS: ws_receive type/assert/match_all criteria wrong
```

- `handshake`: a declared handshake did not arrive or the awaited type is wrong
  (incl. peer-dependent/optional handshake races). Repair target: the
  `ws_receive` await `type` (or the connect's role/handshake).
- `ws_shape`: the `ws_send` message envelope/payload does not match the
  protocol. Repair target: the `ws_send` `message`.
- `ws_match`: a `ws_receive` matched nothing decisive, or an `assert` failed,
  or a `match_all` item violated the predicate. Repair target: the
  `ws_receive` `type`/`aliases`/`asserts`/`match_all`.

### 5.2 Repair tool (`internal/head/scout/repair_plan.go`)

`repair_case` gains an optional `steps` array whose items mirror `agent.TestStep`
(action/connection_id/url/message/type/aliases/asserts/match_all/timeout/role).
The required set stays `["replaces"]`; HTTP fields become optional. When the
failed case has `Steps`, the repair emits `steps`; otherwise it emits HTTP
fields as today.

## 6. Components

### 6.1 Judge (examiner)
- `tools.go`: `redispatch_hint` enum →
  `["none","endpoint_drift","auth","shape","handshake","ws_shape","ws_match"]`.
- `prompts.go`: extend the fail-cause bullet with the WS modes and which step
  field each implicates.
- `assembly.go`: `parseRedispatchHint` accepts the three new values.

### 6.2 Scout repair (scout)
- `repair_plan.go`: `repairTools` adds `steps`; `repairCaseFromCall` branches on
  `steps` present → WS TestCase (`Steps`, `Action="ws_flow"`, `Target` from the
  first connect url or the original) vs HTTP TestCase (unchanged).
- `buildRepairPrompt`: include the failed case's `Steps` when present, and guide
  WS repair per hint.

### 6.3 Session (no change)
`eligibleFailures` already keys on `Status==Fail && Hint!=HintNone` with a
non-nil TestCase; WS cases qualify. `executeRepairLoop` is hint-agnostic.

## 7. Testing Strategy (TDD — failing test first; negative per case)

1. **Enum + parser**: `parseRedispatchHint` accepts the 3 WS values; rejects
   bogus → `none`. Negative: a typo'd WS hint → `none` (RED if accepted).
2. **Judge schema**: the `redispatch_hint` enum contains the 3 WS values.
3. **Judge prompt**: the prompt text names the 3 WS modes.
4. **Repair tool schema**: `repair_case` has a `steps` array property.
5. **WS repair emission**: a `repair_case` with `steps` → TestCase with `Steps`
   (action/type/asserts carried); HTTP `repair_case` (no steps) → unchanged.
6. **Repair prompt**: mentions correcting WS step fields.
7. **Eligibility**: a failed WS case (with Steps) + WS hint is eligible.
8. **Integration**: failed WS case + `ws_match` hint → repair emits a WS
   replacement with corrected `Steps`; `Replaces` bound.
9. **Negative**: HTTP repair unchanged when no steps; non-WS hint never yields
   steps; unrecognized WS failure → `none` (not auto-repaired).

## 8. File Inventory

New: none.
Modified:
- `internal/head/agent/types.go` — 3 WS hint constants.
- `internal/head/examiner/tools.go` — judge enum.
- `internal/head/examiner/prompts.go` — WS fail-cause bullet.
- `internal/head/examiner/assembly.go` — parseRedispatchHint (no change if it
  switches on the constants by value — verify; likely just add to the case set).
- `internal/head/scout/repair_plan.go` — `steps` in repairTools,
  `repairCaseFromCall` branch, prompt.
- Tests in each package.
