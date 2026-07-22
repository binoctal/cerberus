# WS Quick Wins (Heuristic Direction + Examiner WS-Awareness) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stop Scout from generating always-timeout `ws_receive` cases for client-sent WS types, and stop the Examiner from false-passing WS cases that never opened a WebSocket.

**Architecture:** Two independent, provisional fixes. #2 adds a send-verb direction heuristic to `wsTypesNamedInGoal` (goal-text only; no struct/protocol change). #3 adds one WS-aware rule to the Examiner's `promptJudgeSystem` (prompt-only; no evidence-construction change). Each is its own task with its own commit.

**Tech Stack:** Go 1.25, stdlib (`strings`), `github.com/coder/websocket` (untouched), table-driven `testing` + `testify`.

## Global Constraints

- Go 1.25, module `github.com/binoctal/cerberus`.
- Pure Go (no CGo); stdlib only for #2 (`strings`) — no new dependencies.
- `github.com/coder/websocket v1.8.14` untouched; no evaluator / expression engine.
- Commit author `binoctal <binoctal@gmail.com>`, no `Co-Authored-By`.
- Code comments and commit messages in English.
- All docs under `cerberus-docs/` only; never `docs/`.
- `make check` (fmt + lint + test -race) must be green; tests table-driven.
- Determinism: no unsorted map iteration in generated slices.

---

## File Structure

- **Modify** `internal/head/scout/ws_cases.go` — add `wsSendVerbs`, rewrite `wsTypesNamedInGoal` to be direction-aware (Task 1).
- **Modify** `internal/head/scout/ws_cases_test.go` — add `TestWsTypesNamedInGoalDirection` (unit) + `TestWSCasesSendVerbTokenNotReceive` (behavioral) (Task 1).
- **Modify** `cerberus-docs/executors/websocket.md` — one-sentence nuance in "Scout-generated cases (M3-2)" (Task 1).
- **Modify** `internal/head/examiner/prompts.go` — add doc comment + WS rule line to `promptJudgeSystem` (Task 2).

---

## Task 1: Direction-aware `wsTypesNamedInGoal` (heuristic #2)

**Files:**
- Modify: `internal/head/scout/ws_cases.go` (the `wsTypesNamedInGoal` function at lines 88-106; add a new `wsSendVerbs` var immediately before it)
- Modify: `internal/head/scout/ws_cases_test.go` (append two new test functions)
- Modify: `cerberus-docs/executors/websocket.md` (the "Scout-generated cases (M3-2)" subsection)

**Interfaces:**
- Consumes: none new. `wsTypesNamedInGoal(goal string) []string` is called only by `wsDecisiveTypes` (same file); its signature is unchanged.
- Produces: `wsTypesNamedInGoal` now omits client-sent tokens. `WSCases` (unchanged signature) consequently emits fewer `ws_receive` cases for verb-phrased goals.

**RED note:** Before this task, `WSCases(cfg, "send device:command, verify device:ack")` emits a `ws_receive` case for `device:command` (the bug). The new behavioral test asserts it does NOT, so the test starts RED.

- [ ] **Step 1: Write the two failing tests**

Append to `internal/head/scout/ws_cases_test.go` (after the existing tests, before the `filterAction` helper is fine — helpers can stay at the bottom):

```go
// TestWsTypesNamedInGoalDirection pins the send-verb direction heuristic: a
// colon token immediately preceded by a send-verb is client-sent and excluded;
// tokens without a send-verb context default to receive (included).
func TestWsTypesNamedInGoalDirection(t *testing.T) {
	tests := []struct {
		name string
		goal string
		want []string
	}{
		{"send verb excludes following token", "send device:command, verify device:ack", []string{"device:ack"}},
		{"verify keeps token", "verify device:ack", []string{"device:ack"}},
		{"no verb defaults to include", "device:command", []string{"device:command"}},
		{"brace template no verb includes", "{type: device:command}", []string{"device:command"}},
		{"mixed send and verify", "send devices:sync and verify device:ack", []string{"device:ack"}},
		{"emit verb excludes", "emit status:update then verify status:ack", []string{"status:ack"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.ElementsMatch(t, tc.want, wsTypesNamedInGoal(tc.goal))
		})
	}
}

// TestWSCasesSendVerbTokenNotReceive is the behavioral proof for #2: a
// verb-phrased goal must not produce a ws_receive case for a client-sent type.
func TestWSCasesSendVerbTokenNotReceive(t *testing.T) {
	cfg := &project.Config{Services: []project.Service{{
		Name: "rt", URL: "http://x",
		Protocol: &project.Protocol{TypePath: "type", Roles: map[string]*project.ProtocolRole{
			"web": {Handshake: &project.RoleHandshake{AwaitType: "devices:sync", Timeout: 5}},
		}},
	}}}
	cases := WSCases(cfg, "send device:command, verify device:ack")
	types := bodyTypes(filterAction(cases, "ws_receive"))
	// device:command is client-sent (send-verb) -> NOT a receive target.
	assert.NotContains(t, types, "device:command",
		"a client-sent type (send device:command) must not become a ws_receive case")
	// device:ack (verify) and devices:sync (handshake await_type) remain.
	assert.Contains(t, types, "device:ack")
	assert.Contains(t, types, "devices:sync")
}
```

- [ ] **Step 2: Run the new tests, verify they FAIL**

Run: `go test ./internal/head/scout/ -run 'TestWsTypesNamedInGoalDirection|TestWSCasesSendVerbTokenNotReceive' -v`
Expected: FAIL. `TestWsTypesNamedInGoalDirection/"send verb excludes following token"` expects `["device:ack"]` but gets `["device:command", "device:ack"]`; `TestWSCasesSendVerbTokenNotReceive` fails on the `NotContains device:command` assertion. This confirms the bug is captured.

- [ ] **Step 3: Add `wsSendVerbs` and rewrite `wsTypesNamedInGoal`**

In `internal/head/scout/ws_cases.go`, replace the existing `wsTypesNamedInGoal` function (lines 88-106) with the `wsSendVerbs` var followed by the new function:

```go
// wsSendVerbs are goal verbs that mark the following colon token as something
// the CLIENT sends (not a receive target). A token whose immediately preceding
// word is one of these is excluded from ws_receive generation. Provisional —
// tune via dogfooding.
var wsSendVerbs = map[string]bool{
	"send": true, "sends": true, "sending": true,
	"emit": true, "emits": true,
	"publish": true, "publishes": true,
}

// wsTypesNamedInGoal finds candidate routing-type tokens in the goal text. A
// simple heuristic: colon-bearing tokens (e.g. "permission:response") are
// common WS routing keys. A token immediately preceded by a send-verb (see
// wsSendVerbs) is client-sent and excluded; tokens without such context default
// to receive (included). Deterministic; no LLM. Provisional — tune via dogfooding.
func wsTypesNamedInGoal(goal string) []string {
	var out []string
	fields := strings.Fields(goal)
	for i, field := range fields {
		// Strip punctuation incl. braces so a goal template like
		// "{type: device:command}" yields "device:command", not
		// "device:command}" or "{type:".
		f := strings.Trim(field, ".,;:\"'(){}")
		if f == "type:" {
			continue // the default routing-key field name, not a type value
		}
		if !strings.Contains(f, ":") {
			continue
		}
		if contains(out, f) {
			continue
		}
		// Direction: a token immediately preceded by a send-verb is something
		// the CLIENT sends, so it is not a receive target — skip it. Tokens
		// without a send-verb context default to receive (existing behavior).
		if i > 0 && wsSendVerbs[strings.ToLower(strings.Trim(fields[i-1], ".,;:\"'(){}"))] {
			continue
		}
		out = append(out, f)
	}
	return out
}
```

- [ ] **Step 4: Run the tests, verify they PASS**

Run: `go test ./internal/head/scout/ -run 'TestWsTypesNamedInGoalDirection|TestWSCasesSendVerbTokenNotReceive' -v`
Expected: PASS (all subtests).

- [ ] **Step 5: Update the executor doc**

In `cerberus-docs/executors/websocket.md`, in the "Scout-generated cases (M3-2)" subsection, replace this exact phrase:

```
(the role's handshake `await_type` and any routing type named in the goal), linked by `DependsOn`.
```

with:

```
(the role's handshake `await_type` and any **receive-directed** routing type named in the goal), linked by `DependsOn`. A type whose immediately preceding word in the goal is a send-verb (`send`/`emit`/`publish`) is treated as client-sent and not turned into a receive case; tokens without a send-verb context default to receive. Provisional — tune via dogfooding.
```

- [ ] **Step 6: Run make check, verify green**

Run: `make check`
Expected: EXIT 0 (fmt + lint + test -race all pass; the six pre-existing `ws_cases_test.go` tests still pass — none use an immediate send-verb before a token).

- [ ] **Step 7: Commit**

```bash
git add internal/head/scout/ws_cases.go internal/head/scout/ws_cases_test.go cerberus-docs/executors/websocket.md
git commit -m "fix(scout): exclude client-sent goal types from ws_receive generation

wsTypesNamedInGoal now skips a colon token whose immediately preceding word is
a send-verb (send/emit/publish): a client-sent type (e.g. device:command in
\"send device:command\") is not a receive target, so it no longer produces an
always-timeout ws_receive case. Tokens without a send-verb context default to
receive (unchanged). Direction inferred from goal text only — ProtocolRole has
no direction field; the deterministic fix is protocol-declared direction or
multi-step Steps (deferred). Provisional, tune via dogfooding."
```

---

## Task 2: WS-aware Examiner rule (prompt #3)

**Files:**
- Modify: `internal/head/examiner/prompts.go` (add a doc comment above `promptJudgeSystem`; append one rule line inside the raw-string `RULES` block)

**Interfaces:**
- Consumes: none new.
- Produces: `promptJudgeSystem` gains a WS rule. No signature change; `Judge.Judge` is unchanged.

**Constraint reminder:** `promptJudgeSystem` is a single backtick raw-string literal. The added text MUST contain **no backticks and no `${}`** or it will not compile. Action names are plain words (`ws_connect`, not formatted).

- [ ] **Step 1: Add the doc comment and the WS rule line**

In `internal/head/examiner/prompts.go`, first add a doc comment immediately above the `const promptJudgeSystem =` line (currently there is no comment there):

```go
// promptJudgeSystem is the Examiner's verdict rules. The WebSocket bullet (a WS
// case needs a real upgraded exchange; plain-HTTP-only evidence is a FAIL) closes
// the 2026-07-21 dogfood Finding 5 false-pass — a case judged pass@0.98 on HTTP
// 426 evidence that never opened a WebSocket.
const promptJudgeSystem = `You are a test verdict judge. Evaluate test evidence against expectations.
```

Then append one new rule as the last line inside the `RULES:` block — i.e. immediately after the existing line ending `... A Step Error that prevented the test from executing is always FAIL.` and before the closing backtick. Add:

```go
- For a WebSocket case, PASS requires a real upgraded exchange: a successful ws_connect and, when the expectation is receiving a message, a ws_receive that matched the awaited type. Any plain-HTTP response in a WS case (426 Upgrade Required, 400, connection closed without upgrade) means the socket was never upgraded — that is a FAIL, not a pass. A WS case whose evidence is only failing HTTP requests, with no ws_* result and no matched WS message, did not test the WebSocket and is a FAIL. A connect-only case (expectation: establish the connection) passes on a successful ws_connect without a matched receive.`
```

The result: the closing backtick of the raw string now follows this new line. Verify no backticks appear anywhere in the inserted text.

- [ ] **Step 2: Run make check, verify green**

Run: `make check`
Expected: EXIT 0. (Build confirms the raw-string edit compiles; examiner tests pass. There is no new test for #3 — the rule is LLM-facing prose validated by dogfood re-run, per spec.)

- [ ] **Step 3: Commit**

```bash
git add internal/head/examiner/prompts.go
git commit -m "fix(examiner): add WebSocket-aware rule to judge prompt

promptJudgeSystem now requires a real upgraded WS exchange for a WS case to
pass: plain-HTTP-only evidence (426/400/no upgrade) is a FAIL, and a WS case
with no ws_* result did not test the WebSocket. A connect-only case still
passes on a successful ws_connect. Closes the 2026-07-21 dogfood Finding 5
false-pass (tc-002 pass@0.98 on HTTP 426). Prompt-only; no evidence-construction
change; validated by dogfood re-run."
```

---

## Post-implementation (not a task)

- Optional dogfood re-run (run-6, manual, not committed): confirm (a) a verb-phrased goal no longer emits a `device:command` receive case, and (b) the Examiner no longer false-passes a 426-only WS case.
- Whole-branch opus final review, then `superpowers:finishing-a-development-branch` (local ff-merge to main, make check, delete branch; not pushed).
- Update `ws-realtime-engine-roadmap` + `ws-dogfood-tier1-checkpoint` memories and the `.superpowers/sdd/progress.md` ledger.
