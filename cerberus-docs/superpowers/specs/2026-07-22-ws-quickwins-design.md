# WS Quick Wins (Heuristic Direction + Examiner WS-Awareness) — Design

**Date:** 2026-07-22
**Branch:** `feat/ws-followup-quickwins` (from `main` @ `54d0d23`)
**Scope:** Two small, independent follow-up fixes to the WS realtime engine,
each its own SDD task. Provisional, tune-via-dogfooding by design.

## Trigger

Both fixes close open items recorded after the 2026-07-21 Tier-1 dogfood and
the M3-2 verify runs:

- **#2 (heuristic direction):** `WSCases` generated a `ws_receive` case for
  `device:command`, a type the **client sends**. That case waits for a message
  the server never emits, so it always times out — a guaranteed false-failure
  that pollutes the plan and wastes Examiner budget (dogfood run-5
  `device-command` failure).
- **#3 (Examiner WS false-pass):** dogfood Finding 5 — case `tc-002` was judged
  `pass` at correctness `0.98` despite every action being a failing
  `api_request` (HTTP 426) that never opened a WebSocket. The Examiner prompt
  has no WS-specific failure signal.

## Non-goals

- Full deterministic multi-step cases (`TestCase.Steps` + `matchWSRules`) — the
  strategic fix for `device-ack` (needs Steer to `send device:command` before
  the ack arrives). Deferred; gated on the D4 reproducibility signal.
- Making `device-ack` actually pass (skeleton-scope limit; requires Steps).
- M3-2 Minor cleanup `contains` → `slices.Contains` (separate follow-up).
- URL/timestamp false-positives in `wsTypesNamedInGoal` (pre-existing
  provisional limitation; needs token-shape filtering, out of scope here).

## Background — root cause

`wsDecisiveTypes(role, goal)` collects the role's `handshake.await_type` (a
server greeting — always a correct receive target) plus every colon-bearing
token extracted from the goal by `wsTypesNamedInGoal`. The latter treats **all**
goal-named types as receive targets. `ProtocolRole` carries no message-direction
information (only the handshake `AwaitType`), so direction can only be inferred
from the goal text. A goal phrased *"send device:command, verify device:ack"*
thus yields both `device:command` (send) and `device:ack` (receive) as
`ws_receive` targets.

The Examiner (`internal/head/examiner`) judges result evidence via
`promptJudgeSystem`. `buildEvidenceContext` already surfaces the last action and
per-result-type detail (HTTP status/body, WS matched/seen/error), so the model
*sees* that a WS case produced only failing HTTP — but the prompt gives it no
rule that this must be a fail.

## Fix #2 — direction-aware `wsTypesNamedInGoal`

**File:** `internal/head/scout/ws_cases.go` — only `wsTypesNamedInGoal`.

**Mechanism:** introduce a send-verb set

```
send, sends, sending, emit, emits, publish, publishes
```

When scanning goal fields, a colon token whose **immediately preceding word**
(trimmed of punctuation, lower-cased) is a send-verb is treated as
client-sent and **skipped** — it is not a receive target. Every other path is
unchanged: tokens without a send-verb context default to **include** (existing
behavior), preserving the `type:` skip and brace-strip logic verbatim.

```
"send device:command, verify device:ack"
  device:command  ← prev "send"    → skip (client sends it)
  device:ack      ← prev "verify"  → include (verify is not a send-verb)
"device:command"               (no preceding word) → include (default)
"{type: device:command}"       prev is "type:"     → include (default)
```

**Determinism:** the send-verb set is a map used for lookup only (never
iterated); output is appended in field order. No new map-iteration
nondeterminism. `-race` clean (read-only package-level map after init).

**Existing-test impact (verified):** none of the six `ws_cases_test.go` tests
break. Their goals use `receives`, brace-templates, or empty strings — never an
immediate send-verb before the token. Notably
`TestWSCasesTargetSetAndGoalTemplateBraces` (goal `"send a {type:
device:command} message"`) still includes `device:command` because its preceding
word is `type:`, not `send`. This is why **immediate-preceding** is the chosen
granularity: a "scan back to the nearest verb" heuristic would break that test
(which asserts inclusion). Immediate-preceding is the minimal, non-breaking
choice that fixes the actual dogfood verb-phrasing.

### Provisional limitations (explicit, deferred)

1. **Only the immediately preceding word is inspected.** A token with an
   article between verb and token (`"send a device:command"`) is not excluded.
   This phrasing is exactly the one the existing brace-template test keeps
   included by design.
2. **URL/timestamp false-positives** (`ws://host:port`, `12:30:00`) are
   pre-existing and **not** fixed here.
3. **The deterministic fix is protocol-declared direction** (e.g.
   `roles.web.sends` / `receives` lists) or multi-step `Steps`. This heuristic
   is a provisional patch, to be revisited when those land.

## Fix #3 — WS-aware Examiner rule

**File:** `internal/head/examiner/prompts.go` — only `promptJudgeSystem`.

**Approach: prompt-only.** `buildEvidenceContext` already carries the last
action and result types, so the model has the signal; the gap is a judging rule.
Adding evidence-side annotations would invade evidence construction beyond a
quick win.

**New rule (added to the `RULES` block).** The literal prompt text contains
**no backticks and no `${}`** — `promptJudgeSystem` is a single backtick
raw-string literal, so a backtick would fail to compile. The action names are
written as plain words (`ws_connect`, not formatted). Exact text to insert:

> For a WebSocket case, PASS requires a real upgraded exchange: a successful
> ws_connect and, when the expectation is receiving a message, a ws_receive
> that matched the awaited type. Any plain-HTTP response in a WS case (426
> Upgrade Required, 400, connection closed without upgrade) means the socket
> was never upgraded — that is a FAIL, not a pass. A WS case whose evidence is
> only failing HTTP requests, with no ws_* result and no matched WS message,
> did not test the WebSocket and is a FAIL. A connect-only case (expectation:
> establish the connection) passes on a successful ws_connect without a matched
> receive.

**Generalized beyond 426:** servers vary (426 / 400 / close-without-upgrade),
so the rule names plain-HTTP non-upgrade generically; 426 is one example.

**Connect-only carve-out:** the rule explicitly exempts connect-setup cases
(expectation = establish the connection, whose `WSResult` has no
`MatchedMessage`) so it cannot over-fire and fail a genuine successful connect.

**No unit test:** `promptJudgeSystem` is LLM-facing prose; a `strings.Contains`
assertion is brittle and low-value. Validated by dogfood re-run (does the
Examiner stop false-passing a 426-only WS case?), consistent with how prompt
tuning is verified. The added text is self-documenting via a Go comment citing
dogfood Finding 5.

## Tests

- **#2 unit (table-driven, `ws_cases_test.go`):**
  - `"send device:command, verify device:ack"` → `["device:ack"]`
  - `"verify device:ack"` → `["device:ack"]`
  - `"device:command"` (no verb) → `["device:command"]` (default-include regression guard)
  - `"{type: device:command}"` → `["device:command"]` (brace-strip + no-verb include)
  - `"send devices:sync and verify device:ack"` → `["device:ack"]`
- **#2 behavioral:** via `WSCases`/`wsDecisiveTypes`, assert a verb-phrased goal
  emits a `device:ack` receive case and **not** a `device:command` one — the
  behavioral proof for #2.
- **#3:** no new test (see above).

## Documentation

- **#2:** `cerberus-docs/executors/websocket.md`, "Scout-generated cases (M3-2)"
  subsection — change "any routing type named in the goal" to any
  **receive-directed** routing type, noting that a type following a send-verb
  (`send`/`emit`/`publish`) is treated as client-sent and excluded. Marked
  provisional.
- **#3:** no `cerberus-docs` change — no external doc enumerates the
  `promptJudgeSystem` rules (they live in code). A Go comment in `prompts.go`
  citing dogfood Finding 5 suffices.

## Verification

1. `make check` green (`#2` unit + behavioral tests; zero regression in the
   existing six `ws_cases_test.go` tests).
2. Optional dogfood re-run (run-6, manual verify, not committed): confirm
   (a) a verb-phrased goal no longer emits a `device:command` receive case, and
   (b) the Examiner no longer false-passes a 426-only WS case.

## Constraints

- Go 1.25, pure Go (no CGo), stdlib only (`strings`; no new deps).
- `coder/websocket v1.8.14` untouched; no evaluator / expression engine.
- Commit author `binoctal <binoctal@gmail.com>`, no `Co-Authored-By`.
- Comments and commit messages in English; docs under `cerberus-docs/` only.
- `make check` (fmt + lint + test -race) must be green; tests table-driven.

## Open questions / deferred

- Protocol-declared message direction (`sends`/`receives`) as the deterministic
  successor to the #2 heuristic — revisit with Steps / Tier-2 dogfood signal.
- Examiner-side deterministic fast-fail for "WS case with no `ws_*` result"
  (a stricter, non-LLM path via `objectiveVerdict`) — possible future hardening
  of #3; out of scope for this quick win.
