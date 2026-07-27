# ws_receive assert — malformed-assert defense (D4) — 2026-07-28

## Context

`ws_receive`'s `assert` is a path→value map the executor evaluates deterministically
on the matched message (every entry must hold, else the receive fails). The
planning LLM emits it. Despite the schema guidance added in `fb85554` (which
steers the LLM to emit correct path→value or omit it), GLM emission is
non-deterministic: across dogfood runs the LLM emitted malformed asserts that
**false-failed a correctly-matched relay at execution** —

- run3: `assert: {field:"type", op:"==", value:"device:online"}` (expression shape)
- run4: `assert: {msgType:"device:online"}` (wrong path name; real field is `type`)

`checkAsserts` treats every map key as a JSON path; the bogus keys did not
resolve on the matched `device:online` message → assert "failed" → the receive
returned `OK=false` despite `MatchedMessage` being set → `runSteps` short-circuited
→ the case FAILED although the relay objectively matched.

The schema fix (`fb85554`) is probabilistic (depends on the LLM cooperating).
This spec adds a **deterministic defense** so a type-matched receive can never
be false-failed by a malformed assert, making relay fidelity independent of GLM.

## Non-goal

Changing the strictness of `assert` for LEGITIMATE content checks. A real
assert whose root field exists in the message must still fail on
mismatch / missing-intermediate. Only asserts whose **root segment is absent
from the matched message** are treated as malformed.

## The fundamental tradeoff (accepted)

A malformed wrong-path assert (`{msgType:...}`, root `msgType` absent) is
**structurally identical** to a legitimate assert on a genuinely-absent root
field (`{payload.approved:true}` on a message with no `payload`). No structural
heuristic can tell them apart. Therefore any defense that catches the former
must also tolerate the latter.

**Decision (D4, user-approved 2026-07-28):** when the routing-key `type`
matched, an assert entry whose root segment is NOT a top-level key of the
matched message is treated as **malformed → skipped (non-fatal)**, with a
visible `warn` log. This masks the rare "legit assert on absent root field"
real-failure in exchange for never false-failing a matched relay on a
malformed assert. The masking is **logged, not silent**.

## Design

### Where

`checkAsserts` (`internal/head/agent/websocket.go:576`) — the single assert
evaluator, called only from `doReceive` (line 711). Plus `doReceive`'s result
construction (it owns the `*zap.Logger`).

### Logic (per assert entry)

```
for each entry (path → expected), in sorted path order:
    root = first dotted segment of path            # "payload.approved" → "payload"
    if extractPath(data, root) not found:          # root absent from matched message
        record path in malformedPaths; continue    # malformed → skip, non-fatal
    got, found = extractPath(data, path)           # root exists → evaluate normally
    if !found:      return fail (root-exists, intermediate missing)   # real failure, preserved
    if !valueEqual: return fail (value mismatch)                      # real failure, preserved
return ok=true, malformedPaths
```

Short-circuit on the first LEGIT (root-exists) failure is preserved. Malformed
entries are skipped without failing.

### Signature change

`checkAsserts` gains one return value (keeps it a pure function; the single
caller adapts):

```go
func checkAsserts(data []byte, asserts map[string]any) (
    failPath string, failExpected, failActual any, ok bool, malformedPaths []string)
```

### Result construction (doReceive)

On the `matched` branch, after `checkAsserts`:
- If `!ok` (a root-exists entry failed) → `WSResult{OK:false, Err:"receive: assert …",
  MatchedMessage:…}` **unchanged** (real failure preserved).
- Else if `len(malformedPaths) > 0` → `WSResult{OK:true, MatchedMessage:…}` (the
  relay matched; do NOT fail), and `e.logger.Warn("ws_receive: assert entries
  skipped (root not in matched message, likely malformed — not failing the
  matched receive)", paths, type, connection_id)`.
- Else → `WSResult{OK:true, MatchedMessage:…}` unchanged.

### Semantics summary

| assert shape on a matched `device:online` (`{type,payload}`) | before | after |
|---|---|---|
| `{msgType:"device:online"}` (wrong path) | FAIL (false) | pass + warn |
| `{field,op,value}` (expression) | FAIL (false) | pass + warn |
| `{payload.approved:true}`, payload present, approved=true | pass | pass |
| `{payload.approved:true}`, payload present, approved=false | FAIL (real) | FAIL (real, preserved) |
| `{payload.approved:true}`, payload present, approved absent | FAIL (real) | FAIL (real, preserved) |
| `{payload.approved:true}`, **no `payload` root** | FAIL (real) | **pass + warn (MASKED)** |

The only behavioral regression is the last row (accepted, logged).

## Test plan (`internal/head/agent/websocket_test.go`)

New (deterministic, via the executor or a direct `checkAsserts` table test):
1. Malformed roots `{msgType:"device:online"}` and `{field,op,value}` on a
   matched message → `ok=true`, `malformedPaths` lists them.
2. Root-exists value mismatch → `ok=false` (preserved).
3. Root-exists intermediate missing → `ok=false` (preserved).
4. Legit assert on absent root (`{payload.approved:true}`, no `payload`) →
   `ok=true` + `malformedPaths=["payload.approved"]` (documented mask).

Existing test review:
- `TestReceiveAssertMissingPathFails` (line 1030): **confirmed root-missing** —
  message `{"type":"x"}` + assert `{payload.approved:true}` (root `payload`
  absent). Under D4 this becomes pass+warn, so the test would regress. UPDATE:
  change the message to `{"type":"x","payload":{}}` (root present, leaf
  `approved` absent) so the test keeps locking "a missing path fails" under the
  new semantics (root exists → intermediate missing → real failure preserved).
  The root-missing→pass+warn behavior is covered by new test #4 above.
- `TestReceiveAssertValueMismatchFails`, `…NumericNormalization`,
  `…MultipleReportsFirstSorted`, `…RejectedUnderTextFraming`: unaffected
  (root-exists / framing checks), should stay green.

## Verification

- `make check` (fmt + lint + test -race) EXIT 0.
- Live dogfood (open-agents `device:online` relay): with the schema fix already
  making the LLM omit assert, runs should still pass; the defense is a backstop.
  To exercise the defense live, a goal that provokes a malformed assert would be
  needed (non-deterministic); the deterministic unit tests are the primary proof.

## Out of scope

- Strengthening the schema further (already done in `fb85554`).
- Routing single-connection WS cases deterministically (Finding-2 — separate).
- Changing `WSResult` JSON shape (the warn is log-only).
