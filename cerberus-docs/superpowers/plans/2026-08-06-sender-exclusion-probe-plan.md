# Sender-Exclusion Probe Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give WS relay fan-out cases an active sender-exclusion probe so the Examiner's `membership` dimension reports a measured `Excluded` (`*true`/`*false`) instead of `nil` ("not probed"), resolving the recurring `routing` `honest-uncertain` drift into a confident verdict.

**Architecture:** A negative-receive step (`ExpectAbsent`) planned by Scout asserts the sender does not receive its own broadcast. The executor inverts success for `ExpectAbsent` (timeout = pass, echo = fail). The outcome flows through `Evidence.ExpectAbsent` into `deriveDimensions`, which sets `Dimension.Excluded` from the probe. No prompt-template change; `renderDimensions` already renders all three `Excluded` states. Non-probe cases stay byte-identical (zero regression).

**Tech Stack:** Go 1.25, table-driven tests, existing WS executor httptest harness, existing `//go:build manual` validation harness.

## Global Constraints

- Module: `github.com/binoctal/cerberus`, Go 1.25, no CGo.
- Commit author: `binoctal <binoctal@gmail.com>`, NO `Co-Authored-By`.
- Code comments and commit messages in English; match existing comment density.
- All docs go in `cerberus-docs/`, never `docs/`.
- `Excluded` must stay `nil` when no probe ran (zero-regression: byte-identical prompt for non-probe traces).

---

## File Structure

- `internal/types/actions_http.go` — `WSReceiveAction.ExpectAbsent` field.
- `internal/types/actions_test.go` — field round-trip in the marshal/validate table.
- `internal/head/agent/types.go` — `TestStep.ExpectAbsent` + `Evidence.ExpectAbsent` fields.
- `internal/head/agent/execute_phases_steps.go` — thread `ExpectAbsent` in `stepToAction` and `stepEvidence`.
- `internal/head/agent/execute_phases_steps_test.go` — round-trip + evidence-threading unit test.
- `internal/head/agent/websocket.go` — `doReceive` inverted-success branch for `ExpectAbsent`.
- `internal/head/agent/websocket_test.go` — executor test: ExpectAbsent pass-on-timeout, fail-on-echo.
- `internal/head/examiner/dimensions.go` — `deriveDimensions` sets `Excluded` from a sender probe.
- `internal/head/examiner/dimensions_test.go` — probe-outcome table test; regression test that no-probe stays nil.
- `internal/head/scout/ws_cases.go` — `wsRelayCases` appends a negative-receive probe for each joining peer.
- `internal/head/scout/ws_relay_test.go` — assembly test: relay case contains the probe step.
- `internal/head/examiner/vocab_validation_manual_test.go` — add a probe-carrying `routing` case.

---

### Task 1: `ExpectAbsent` flag on `WSReceiveAction` and `TestStep`, threaded through `stepToAction`

**Files:**
- Modify: `internal/types/actions_http.go:233` (add field), `internal/types/actions_test.go` (round-trip).
- Modify: `internal/head/agent/types.go:76` (add `TestStep.ExpectAbsent`).
- Modify: `internal/head/agent/execute_phases_steps.go:70` (thread into action).
- Test: `internal/head/agent/execute_phases_steps_test.go` (create or extend).

**Interfaces:**
- Produces: `WSReceiveAction.ExpectAbsent bool`; `TestStep.ExpectAbsent bool`; `stepToAction` copies `s.ExpectAbsent` into `WSReceiveAction.ExpectAbsent` on the `ws_receive` branch.

- [ ] **Step 1: Write the failing round-trip test**

Create `internal/head/agent/execute_phases_steps_test.go`:

```go
package agent

import (
	"testing"

	"github.com/binoctal/cerberus/internal/types"
)

// TestStepToAction_ExpectAbsent threads the negative-probe flag onto the
// ws_receive action so the executor can invert success semantics.
func TestStepToAction_ExpectAbsent(t *testing.T) {
	tc := &TestCase{Target: "ws://x"}
	got, err := stepToAction(tc, TestStep{
		Action: "ws_receive", ConnectionID: "c1", Type: "workflow:task_progress",
		Timeout: 2, ExpectAbsent: true,
	})
	if err != nil {
		t.Fatalf("stepToAction: %v", err)
	}
	r, ok := got.(types.WSReceiveAction)
	if !ok {
		t.Fatalf("result type %T, want WSReceiveAction", got)
	}
	if !r.ExpectAbsent {
		t.Fatalf("ExpectAbsent not threaded: %+v", r)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/head/agent/ -run TestStepToAction_ExpectAbsent -v`
Expected: FAIL — `unknown field 'ExpectAbsent' in struct literal` (compile error) — the field does not exist yet.

- [ ] **Step 3: Add the field to `WSReceiveAction`**

In `internal/types/actions_http.go`, add to `WSReceiveAction` (after `MatchAll`):

```go
	// ExpectAbsent inverts matching: the receive PASSES when no matching frame
	// arrives within Timeout (the sender was correctly excluded from a broadcast)
	// and FAILS when one does (the server echoed to the sender). Used by relay
	// fan-out cases to actively probe sender-exclusion. Without it, timeout is a
	// failure as usual.
	ExpectAbsent bool `json:"expect_absent,omitempty"`
```

- [ ] **Step 4: Add the field to `TestStep`**

In `internal/head/agent/types.go`, add to `TestStep` (after `Timeout`):

```go
	ExpectAbsent bool `json:"expect_absent,omitempty"` // ws_receive: assert the type does NOT arrive (sender-exclusion probe)
```

- [ ] **Step 5: Thread it in `stepToAction`**

In `internal/head/agent/execute_phases_steps.go`, replace the `case "ws_receive":` return:

```go
	case "ws_receive":
		return types.WSReceiveAction{ConnectionID: s.ConnectionID, Type: s.Type,
			Aliases: s.Aliases, Assert: s.Asserts, Timeout: s.Timeout, Decisive: true, MatchAll: s.MatchAll,
			ExpectAbsent: s.ExpectAbsent}, nil
```

- [ ] **Step 6: Run the test to verify it passes**

Run: `go test ./internal/head/agent/ -run TestStepToAction_ExpectAbsent -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/types/actions_http.go internal/head/agent/types.go \
  internal/head/agent/execute_phases_steps.go internal/head/agent/execute_phases_steps_test.go
git -c user.name=binoctal -c user.email=binoctal@gmail.com \
  commit -m "feat(ws): ExpectAbsent flag on WSReceiveAction and TestStep"
```

---

### Task 2: `doReceive` inverts success when `ExpectAbsent`

**Files:**
- Modify: `internal/head/agent/websocket.go:961` (the single-match `readMatching` path; `MatchAll`+`ExpectAbsent` is rejected as a case-authoring error).
- Test: `internal/head/agent/websocket_test.go` (two new tests).

**Interfaces:**
- Consumes: `WSReceiveAction.ExpectAbsent` (Task 1).
- Produces: `doReceive` returns `WSResult{OK:true}` on timeout and `WSResult{OK:false,...}` on match when `ExpectAbsent`. `MatchedMessage`/`SeenMessages` stay populated so a real echo is visible downstream.

- [ ] **Step 1: Write the failing tests**

Append to `internal/head/agent/websocket_test.go`:

```go
// TestWSReceiveExpectAbsent_PassesOnTimeout verifies a negative probe succeeds
// when the sender does NOT receive its own broadcast within the window.
func TestWSReceiveExpectAbsent_PassesOnTimeout(t *testing.T) {
	url := newWSTestServer(t, func(conn *websocket.Conn) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		write := func(m map[string]any) {
			b, _ := json.Marshal(m)
			_ = conn.Write(ctx, websocket.MessageText, b)
		}
		// Server sends an unrelated type only — never the probed type.
		write(map[string]any{"type": "unrelated"})
		_, _, _ = conn.Read(ctx)
	})
	ex := newWSExecutor()
	ctx := context.Background()
	ex.Execute(ctx, types.WSConnectAction{URL: url, ConnectionID: "c1"})

	res := ex.Execute(ctx, types.WSReceiveAction{
		ConnectionID: "c1", Type: "workflow:task_progress", Timeout: 1, ExpectAbsent: true,
	})
	ws, ok := res.(types.WSResult)
	if !ok {
		t.Fatalf("result type %T, want WSResult", res)
	}
	if !ws.OK {
		t.Fatalf("ExpectAbsent receive should pass on timeout, got failure: %+v", ws)
	}
}

// TestWSReceiveExpectAbsent_FailsOnEcho verifies a negative probe FAILS when the
// server wrongly echoes the broadcast back to the sender.
func TestWSReceiveExpectAbsent_FailsOnEcho(t *testing.T) {
	url := newWSTestServer(t, func(conn *websocket.Conn) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		write := func(m map[string]any) {
			b, _ := json.Marshal(m)
			_ = conn.Write(ctx, websocket.MessageText, b)
		}
		write(map[string]any{"type": "workflow:task_progress", "payload": map[string]any{"pct": 50}})
		_, _, _ = conn.Read(ctx)
	})
	ex := newWSExecutor()
	ctx := context.Background()
	ex.Execute(ctx, types.WSConnectAction{URL: url, ConnectionID: "c1"})

	res := ex.Execute(ctx, types.WSReceiveAction{
		ConnectionID: "c1", Type: "workflow:task_progress", Timeout: 2, ExpectAbsent: true,
	})
	ws, ok := res.(types.WSResult)
	if !ok {
		t.Fatalf("result type %T, want WSResult", res)
	}
	if ws.OK {
		t.Fatalf("ExpectAbsent receive should fail on echo, got success: %+v", ws)
	}
	if !strings.Contains(ws.MatchedMessage, "workflow:task_progress") {
		t.Fatalf("echoing frame should be visible as evidence: %s", ws.MatchedMessage)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/head/agent/ -run TestWSReceiveExpectAbsent -v`
Expected: FAIL — `PassesOnTimeout` fails (timeout still returns `OK:false`); `FailsOnEcho` fails (match still returns `OK:true`).

- [ ] **Step 3: Implement the inversion**

In `internal/head/agent/websocket.go`, guard `MatchAll`+`ExpectAbsent` early (after the `want := ...` line, before the `if a.MatchAll` branch):

```go
	// ExpectAbsent is a negative probe (assert the type does NOT arrive). It is
	// only meaningful on the single-match path: a MatchAll absence-probe is a
	// case-authoring error (what would "every item of an empty burst" assert?).
	if a.MatchAll && a.ExpectAbsent {
		return types.WSResult{OK: false, Err: "receive: expect_absent is incompatible with match_all", Latency: time.Since(start)}
	}
```

Then invert the two single-match outcomes. Replace the `case "matched":` and `case "timeout":` returns with:

```go
	switch status {
	case "matched":
		// ExpectAbsent: a match means the sender wrongly received its own
		// broadcast — a relay bug. Surface the echoing frame as evidence.
		if a.ExpectAbsent {
			return types.WSResult{
				OK:             false,
				Err:            fmt.Sprintf("receive: expected %q absent but it arrived (sender not excluded)", a.Type),
				MatchedMessage: frameForResult(framing, matched.data),
				SeenMessages:   seen,
				Latency:        time.Since(start),
			}
		}
		data := matched.data
		// For json framing, evaluate field-level assertions (if any) in sorted
		// path order; the first failure fails the receive with a precise message.
		// The matched frame is evidence either way. No asserts (or non-json
		// framing) → arrival-only success.
		if framing == "" || framing == "json" {
			p, exp, act, ok, malformed := checkAsserts(data, a.Assert)
			if !ok {
				return types.WSResult{
					OK:             false,
					Err:            fmt.Sprintf("receive: assert %s: expected %v, got %v", p, exp, act),
					MatchedMessage: frameForResult(framing, data),
					SeenMessages:   seen,
					Latency:        time.Since(start),
				}
			}
			if len(malformed) > 0 {
				// Visible masking: these assert entries referenced roots absent
				// from the matched message (likely malformed). The type matched,
				// so the receive is NOT failed; logged so the skip is visible.
				e.logger.Warn("ws_receive: assert entries skipped (root not in matched message, likely malformed — not failing the matched receive)",
					zap.String("type", a.Type),
					zap.String("connection_id", a.ConnectionID),
					zap.Strings("skipped_paths", malformed))
			}
		}
		return types.WSResult{OK: true, MatchedMessage: frameForResult(framing, data), SeenMessages: seen, Latency: time.Since(start)}
	case "timeout":
		// ExpectAbsent: no frame arrived within the window — the sender was
		// correctly excluded. This is the probe's success path.
		if a.ExpectAbsent {
			return types.WSResult{OK: true, SeenMessages: seen, Latency: time.Since(start)}
		}
		// No matching frame within the deadline. The connection is STILL ALIVE
		// (the pump keeps running): return OK:false without closing, so a later
		// send/receive on the same connection_id can succeed.
		errMsg := fmt.Sprintf("receive: timed out awaiting %q", a.Type)
		if len(a.Aliases) > 0 {
			errMsg = fmt.Sprintf("%s (aliases: %v)", errMsg, a.Aliases)
		}
		return types.WSResult{OK: false, Err: errMsg, SeenMessages: seen, Latency: time.Since(start)}
	default: // "closed"
```

(Note: the body of the original `matched` case below the ExpectAbsent guard is unchanged; only the two new `if a.ExpectAbsent` blocks are inserted. Keep the existing `data := matched.data` … assert logic intact beneath the guard.)

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/head/agent/ -run TestWSReceiveExpectAbsent -v`
Expected: PASS — both subtests.

- [ ] **Step 5: Run the full agent package (no-regression)**

Run: `go test ./internal/head/agent/`
Expected: PASS — existing receive tests unaffected (they do not set ExpectAbsent).

- [ ] **Step 6: Commit**

```bash
git add internal/head/agent/websocket.go internal/head/agent/websocket_test.go
git -c user.name=binoctal -c user.email=binoctal@gmail.com \
  commit -m "feat(ws): doReceive inverts success for ExpectAbsent probe"
```

---

### Task 3: Carry `ExpectAbsent` on `Evidence` via `stepEvidence`

**Files:**
- Modify: `internal/head/agent/types.go:119` (`Evidence` struct).
- Modify: `internal/head/agent/execute_phases_steps.go:14` (`stepEvidence`).
- Test: `internal/head/agent/execute_phases_steps_test.go` (extend).

**Interfaces:**
- Produces: `Evidence.ExpectAbsent bool`; `stepEvidence` sets `ev.ExpectAbsent = s.ExpectAbsent` for `ws_receive`. Consumed by Task 4.

- [ ] **Step 1: Write the failing test**

Append to `internal/head/agent/execute_phases_steps_test.go`:

```go
// TestStepEvidence_ExpectAbsentThreaded verifies the probe flag lands on the
// trace Evidence so the examiner can recognize a negative probe.
func TestStepEvidence_ExpectAbsentThreaded(t *testing.T) {
	ev := stepEvidence(TestStep{
		Action: "ws_receive", ConnectionID: "c1", Type: "workflow:task_progress", ExpectAbsent: true,
	}, types.WSResult{OK: true})
	if !ev.ExpectAbsent {
		t.Fatalf("ExpectAbsent not threaded onto Evidence: %+v", ev)
	}
	if !ev.Matched {
		// OK WSResult with no matched frame — Matched should still be false here.
		// The point is the flag is carried regardless of match outcome.
		t.Logf("matched=%v (expected false for a non-matching OK result)", ev.Matched)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/head/agent/ -run TestStepEvidence_ExpectAbsentThreaded -v`
Expected: FAIL — `ev.ExpectAbsent undefined` (compile error).

- [ ] **Step 3: Add the field to `Evidence` and thread it**

In `internal/head/agent/types.go`, add to `Evidence` (after `Matched`):

```go
	ExpectAbsent bool `json:"expect_absent,omitempty"` // ws_receive: this was a negative (sender-exclusion) probe
```

In `internal/head/agent/execute_phases_steps.go`, in `stepEvidence` extend the `ws_receive` block:

```go
	if s.Action == "ws_receive" {
		ev.MatchedType = s.Type
		ev.Matched = wsReceiveMatched(result)
		ev.ExpectAbsent = s.ExpectAbsent
	}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/head/agent/ -run TestStepEvidence -v`
Expected: PASS — both the new test and any existing `stepEvidence` tests.

- [ ] **Step 5: Commit**

```bash
git add internal/head/agent/types.go internal/head/agent/execute_phases_steps.go \
  internal/head/agent/execute_phases_steps_test.go
git -c user.name=binoctal -c user.email=binoctal@gmail.com \
  commit -m "feat(agent): carry ExpectAbsent on step Evidence"
```

---

### Task 4: `deriveDimensions` sets `Excluded` from a sender probe

**Files:**
- Modify: `internal/head/examiner/dimensions.go:109` (`deriveDimensions`).
- Test: `internal/head/examiner/dimensions_test.go` (probe-outcome table + regression).

**Interfaces:**
- Consumes: `Evidence.ExpectAbsent` (Task 3), `Evidence.ConnectionID`, `Evidence.MatchedType`, `Evidence.Matched`.
- Produces: `Dimension.Excluded` is `*true` when the sender's probe timed out, `*false` when the server echoed, and `nil` when no probe ran (unchanged).

- [ ] **Step 1: Write the failing tests**

Append to `internal/head/examiner/dimensions_test.go`:

```go
// TestDeriveDimensions_ProbeSetsExcluded verifies a sender negative-probe
// settles Dimension.Excluded: no echo ⇒ *true (excluded), echo ⇒ *false.
func TestDeriveDimensions_ProbeSetsExcluded(t *testing.T) {
	sender := "c-web"
	echoed := true
	tests := []struct {
		name    string
		matched bool // probe outcome: did the sender receive its own broadcast?
		want    *bool
	}{
		{"probe timed out ⇒ sender excluded", false, &[]bool{true}[0]},
		{"probe echoed ⇒ sender NOT excluded", true, &echoed},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			j := &Judge{}
			res := agent.StepResult{
				TestCase: &agent.TestCase{ID: "relay"},
				Evidence: []agent.Evidence{
					{Action: "ws_send", ConnectionID: sender, MatchedType: "workflow:task_progress"},
					{Action: "ws_receive", ConnectionID: "c-bridge", MatchedType: "workflow:task_progress", Matched: true},
					{Action: "ws_receive", ConnectionID: sender, MatchedType: "workflow:task_progress", Matched: tc.matched, ExpectAbsent: true},
				},
			}
			dims := j.deriveDimensions(res)
			require.Len(t, dims, 1)
			require.NotNil(t, dims[0].Excluded, "Excluded must be set when a probe ran")
			assert.Equal(t, *tc.want, *dims[0].Excluded)
		})
	}
}

// TestDeriveDimensions_NonSenderProbeIgnored verifies a negative probe on a
// non-sender connection does not settle Excluded (it is not a sender-exclusion
// signal).
func TestDeriveDimensions_NonSenderProbeIgnored(t *testing.T) {
	j := &Judge{}
	res := agent.StepResult{
		TestCase: &agent.TestCase{ID: "relay"},
		Evidence: []agent.Evidence{
			{Action: "ws_send", ConnectionID: "c-web", MatchedType: "workflow:task_progress"},
			{Action: "ws_receive", ConnectionID: "c-bridge", MatchedType: "workflow:task_progress", Matched: true},
			// Probe on a recipient, not the sender — must be ignored.
			{Action: "ws_receive", ConnectionID: "c-bridge", MatchedType: "workflow:task_progress", Matched: false, ExpectAbsent: true},
		},
	}
	dims := j.deriveDimensions(res)
	require.Len(t, dims, 1)
	assert.Nil(t, dims[0].Excluded, "non-sender probe must not settle Excluded")
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/head/examiner/ -run 'TestDeriveDimensions_ProbeSetsExcluded|TestDeriveDimensions_NonSenderProbeIgnored' -v`
Expected: FAIL — `Excluded` is `nil` in all cases (probe not yet recognized).

- [ ] **Step 3: Implement probe resolution in `deriveDimensions`**

Replace `deriveDimensions` in `internal/head/examiner/dimensions.go` with a two-pass version (pass 1 establishes senders + recipients; pass 2 resolves probes against the now-known sender, so Evidence ordering does not matter):

```go
// deriveDimensions produces flow-level dimensions (source 2) from a step
// result's per-step trace. It derives membership: for each message type that a
// ws_send sent, the recipients are the connections whose ws_receive matched it,
// and the sender is the ws_send connection. Excluded is set from a sender
// negative-probe (ExpectAbsent) when one ran: no echo ⇒ *true (sender excluded),
// echo ⇒ *false. Without a probe, Excluded stays nil — the honest "not probed"
// state the judge already renders.
func (j *Judge) deriveDimensions(r agent.StepResult) []types.Dimension {
	senders := map[string]string{}             // type -> sender connectionID
	recipients := map[string]map[string]bool{} // type -> set of recipient connectionIDs
	var probes []agent.Evidence                // ExpectAbsent receives, resolved in pass 2
	for _, ev := range r.Evidence {
		if ev.MatchedType == "" {
			continue
		}
		switch ev.Action {
		case "ws_send":
			if _, ok := senders[ev.MatchedType]; !ok {
				senders[ev.MatchedType] = ev.ConnectionID
			}
			if recipients[ev.MatchedType] == nil {
				recipients[ev.MatchedType] = map[string]bool{}
			}
		case "ws_receive":
			if ev.ExpectAbsent {
				probes = append(probes, ev)
				continue
			}
			if ev.Matched {
				if recipients[ev.MatchedType] == nil {
					recipients[ev.MatchedType] = map[string]bool{}
				}
				recipients[ev.MatchedType][ev.ConnectionID] = true
			}
		}
	}
	// Pass 2: a probe settles Excluded only for its type's sender connection.
	excluded := map[string]*bool{}
	for _, p := range probes {
		sender, ok := senders[p.MatchedType]
		if !ok || p.ConnectionID != sender {
			continue
		}
		b := !p.Matched // no echo ⇒ sender excluded
		excluded[p.MatchedType] = &b
	}
	if len(senders) == 0 {
		return nil
	}
	typesSorted := make([]string, 0, len(senders))
	for t := range senders {
		typesSorted = append(typesSorted, t)
	}
	sort.Strings(typesSorted)
	out := make([]types.Dimension, 0, len(typesSorted))
	for _, t := range typesSorted {
		rcv := make([]string, 0, len(recipients[t]))
		for c := range recipients[t] {
			rcv = append(rcv, c)
		}
		sort.Strings(rcv)
		dim := types.Dimension{
			Kind:       "membership",
			Label:      t + " recipients",
			Recipients: rcv,
			Sender:     senders[t],
		}
		if e, ok := excluded[t]; ok {
			dim.Excluded = e
		}
		out = append(out, dim)
	}
	return out
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/head/examiner/ -run TestDeriveDimensions -v`
Expected: PASS — the two new tests AND the existing `FanOutMembership` / `SingleRecipient` / `UnmatchedReceiveExcluded` tests (their traces have no probe, so `Excluded` stays nil).

- [ ] **Step 5: Commit**

```bash
git add internal/head/examiner/dimensions.go internal/head/examiner/dimensions_test.go
git -c user.name=binoctal -c user.email=binoctal@gmail.com \
  commit -m "feat(examiner): derive Excluded from sender negative-probe"
```

---

### Task 5: Scout appends a sender-exclusion probe to the peer-join relay case

**Files:**
- Modify: `internal/head/scout/ws_cases.go:206` (`wsRelayCases`).
- Test: `internal/head/scout/ws_relay_test.go` (assembly test).

**Interfaces:**
- Consumes: `TestStep.ExpectAbsent` (Task 1).
- Produces: each peer-join relay case ends with one negative-receive step per joining peer, asserting that peer does not receive its own join signal (`<signal>`). This is the faithful sender-exclusion mapping for the presence-relay contract (the joiner is the "sender" of the join event).

- [ ] **Step 1: Write the failing assembly test**

Append to `internal/head/scout/ws_relay_test.go` (read the file first to match its imports/`protocolBuilder` style; the case below uses the existing two-role peer-join relay fixture pattern from `TestAssemblePlan_UnsoundWSFlowDoesNotCover` — adapt the builder call to that file's helper names):

```go
// TestWsRelayCase_AppendsSenderExclusionProbe verifies the deterministic
// peer-join relay case ends with a negative-receive probe asserting each
// joining peer does not receive its own join signal.
func TestWsRelayCase_AppendsSenderExclusionProbe(t *testing.T) {
	svc := <reuse the existing two-role relay fixture builder from this file>
	cases, _, _ := wsRelayCases(svc)
	require.NotEmpty(t, cases)
	// Find the relay case's steps and assert the last step per peer is a probe.
	var relay agent.TestCase
	for _, c := range cases {
		relay = c
		break
	}
	var probeSteps []agent.TestStep
	for _, s := range relay.Steps {
		if s.Action == "ws_receive" && s.ExpectAbsent {
			probeSteps = append(probeSteps, s)
		}
	}
	require.NotEmpty(t, probeSteps, "relay case must include ≥1 ExpectAbsent probe step")
	for _, s := range probeSteps {
		assert.True(t, s.ExpectAbsent)
		assert.NotEqual(t, "", s.Type, "probe must target the join signal type")
		assert.Greater(t, s.Timeout, 0, "probe timeout must be bounded")
	}
}
```

> NOTE for the implementer: replace `<reuse the existing two-role relay fixture builder from this file>` with the actual fixture construction used by the neighboring `TestAssemblePlan_UnsoundWSFlowDoesNotCover` test — read `ws_relay_test.go` and copy its `svc` setup verbatim. Do not invent a new builder.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/head/scout/ -run TestWsRelayCase_AppendsSenderExclusionProbe -v`
Expected: FAIL — `probeSteps` is empty (no ExpectAbsent step emitted yet).

- [ ] **Step 3: Append the probe steps in `wsRelayCases`**

In `internal/head/scout/ws_cases.go`, inside `wsRelayCases`, after the existing `ws_receive` (the receiver's signal-receive) step and before building the `TestCase`, append one probe per joining peer. A short, bounded timeout keeps wall-clock cost low (the probe always waits out its window). After the line:

```go
		steps = append(steps, agent.TestStep{
			Action: "ws_receive", ConnectionID: aName, Type: signal, Timeout: a.Handshake.Timeout,
		})
```

add:

```go
		// Sender-exclusion probe: each joining peer is the "sender" of the join
		// event, so it must NOT receive its own join signal. A short bounded
		// timeout (the probe always waits it out) keeps the cost low. The
		// examiner turns the probe outcome into a measured Dimension.Excluded.
		const probeTimeout = 2
		for _, p := range peers {
			steps = append(steps, agent.TestStep{
				Action: "ws_receive", ConnectionID: p, Type: signal,
				Timeout: probeTimeout, ExpectAbsent: true,
			})
		}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/head/scout/ -run TestWsRelayCase_AppendsSenderExclusionProbe -v`
Expected: PASS.

- [ ] **Step 5: Run the full scout package (no-regression)**

Run: `go test ./internal/head/scout/`
Expected: PASS — existing assembly/relay tests still pass (they assert on emitted case shapes that the appended probe steps do not violate).

- [ ] **Step 6: Commit**

```bash
git add internal/head/scout/ws_cases.go internal/head/scout/ws_relay_test.go
git -c user.name=binoctal -c user.email=binoctal@gmail.com \
  commit -m "feat(scout): append sender-exclusion probe to peer-join relay"
```

---

### Task 6: Manual validation harness — probe-carrying `routing` case

**Files:**
- Modify: `internal/head/examiner/vocab_validation_manual_test.go:165` (the `routing` case) — give it a fan-out Evidence trace WITH a sender probe so the judge sees a measured `Excluded`.

**Interfaces:**
- Consumes: `Evidence.ExpectAbsent` (Task 3), `deriveDimensions` Excluded (Task 4).
- Produces: the `routing` case carries a real per-step trace including a sender negative-probe; the judge should leave `honest-uncertain`.

- [ ] **Step 1: Replace the `routing` case with a probe-carrying variant**

In `buildValidationCases`, replace the single-line `mk("routing", ...)` entry with an explicit `validationCase` whose Evidence traces a broadcast with a sender probe. Keep the other `mk(...)` cases and the `fanout` case unchanged:

```go
		// routing now carries a real fan-out trace WITH a sender negative-probe,
		// so deriveDimensions yields a membership dimension with a measured
		// Excluded (*true: the sender did not receive its own broadcast). This is
		// the case that previously drifted honest-uncertain on "not probed"; the
		// probe should let the judge rule confidently.
		{
			name: "routing",
			result: agent.StepResult{
				TestCase: &agent.TestCase{
					ID: "vc-routing", Name: "routing", Target: "ws://localhost:8989/ws",
					Expectation: "every connected web peer except the sender receives the broadcast",
				},
				Status:   agent.StepPassed,
				Attempts: 1,
				Result: types.WSResult{
					OK:             true,
					MatchedMessage: `{"type":"workflow:task_progress","payload":{"pct":50}}`,
					MatchedCount:   1,
				},
				Evidence: []agent.Evidence{
					{Action: "ws_send", ConnectionID: "c-web", MatchedType: "workflow:task_progress", Content: "ws_send: ok"},
					{Action: "ws_receive", ConnectionID: "c-bridge", MatchedType: "workflow:task_progress", Matched: true, Content: "ws_receive: matched"},
					{Action: "ws_receive", ConnectionID: "c-web-2", MatchedType: "workflow:task_progress", Matched: true, Content: "ws_receive: matched"},
					{Action: "ws_receive", ConnectionID: "c-web", MatchedType: "workflow:task_progress", Matched: false, ExpectAbsent: true, Content: "ws_receive: timed out (sender excluded)"},
				},
			},
		},
```

- [ ] **Step 2: Verify the manual build compiles**

Run: `go vet -tags=manual ./internal/head/examiner/`
Expected: clean.

- [ ] **Step 3: Verify the default build still passes**

Run: `go test ./internal/head/examiner/`
Expected: PASS — `buildValidationCases` is only compiled under `-tags=manual`, but the default build must still compile.

- [ ] **Step 4: Commit**

```bash
git add internal/head/examiner/vocab_validation_manual_test.go
git -c user.name=binoctal -c user.email=binoctal@gmail.com \
  commit -m "test(examiner): routing validation case carries sender probe"
```

- [ ] **Step 5 (manual, optional): Re-run the live validation**

This step requires live LLM credentials (GLM relay) and is NOT required for the code to land. To re-measure drift end-to-end:

Run: `go test -tags=manual ./internal/head/examiner/ -run TestExaminerVocabValidation -v`

Then record the result under `cerberus-docs/technical/validation/` following the four-category metric. The expected change: the `routing` case should leave `honest-uncertain` (now `clean`, since `Excluded=*true` is a concrete fact matching the expectation).

---

### Task 7: Full verification

- [ ] **Step 1: Format and vet**

Run: `make fmt && go vet ./... && go vet -tags=manual ./...`
Expected: clean.

- [ ] **Step 2: Lint**

Run: `make lint`
Expected: clean.

- [ ] **Step 3: Full test suite**

Run: `make test`
Expected: PASS, race-clean — including the new ExpectAbsent / Excluded tests.

- [ ] **Step 4: Commit any fmt drift**

```bash
git add -A
git -c user.name=binoctal -c user.email=binoctal@gmail.com \
  commit -m "chore: fmt after sender-exclusion probe" || echo "nothing to commit"
```

---

## Self-Review Notes

- **Spec coverage:** `WSReceiveAction.ExpectAbsent` (spec Decision 2) → Task 1. `TestStep` + `stepToAction` threading → Task 1. `doReceive` inversion → Task 2. `Evidence.ExpectAbsent` + `stepEvidence` (Decision 3) → Task 3. `deriveDimensions` Excluded (Decision 4) → Task 4. Scout probe (Decision 5) → Task 5. Validation harness → Task 6. Success-criteria verification → Task 7.
- **Placeholder scan:** every code step shows actual content. The one "reuse the existing fixture builder" instruction (Task 5 Step 1) is explicit and scoped — the implementer must copy the neighboring test's `svc` setup rather than invent one; the assertion logic is fully written.
- **Type consistency:** `ExpectAbsent bool` is the field name on `WSReceiveAction`, `TestStep`, and `Evidence` (Tasks 1 & 3). `stepToAction` and `stepEvidence` both read `s.ExpectAbsent`. `deriveDimensions` reads `ev.ExpectAbsent` and writes `*bool` to `Dimension.Excluded` — matching the existing `renderDimensions` switch (`nil`/`*true`/`*false`).
- **Zero-regression:** the existing `TestDeriveDimensions_FanOutMembership` asserts `Excluded` is nil on a no-probe trace; Task 4's two-pass logic preserves that (probes slice is empty ⇒ `excluded` map empty ⇒ `dim.Excluded` untouched). Non-WS and non-relay cases never set ExpectAbsent ⇒ byte-identical prompt.
