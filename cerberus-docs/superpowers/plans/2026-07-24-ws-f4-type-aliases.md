# WS Receive Type-Aliases (F4) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development. Checkbox (`- [ ]`) tracking.

**Goal:** A `ws_receive` (and relay receive step) matches a SET of types — primary `type` OR any `aliases` — so a logical message emitted under several wire types (e.g. `session:output` vs `session:output-batch`) is caught. Backwards-compatible.

**Architecture:** Action-level `Aliases []string` on `WSReceiveAction` + `TestStep`; a `matchAnyType` helper ORs `matchType` over `{type} ∪ aliases`; `doReceive` uses it. The ws_relay expander passes receive-step aliases through. No protocol-schema change.

**Tech Stack:** Go 1.25; existing `matchType`, `WSReceiveAction`, `TestStep`.

## Global Constraints
- Go 1.25, pure-Go (no CGo); `coder/websocket v1.8.14` ONLY; no new deps; no expression evaluator; no protocol-schema change.
- Commit author `binoctal <binoctal@gmail.com>`; NO Co-Authored-By; English; docs only in `cerberus-docs/`; `make check` green.
- Backwards-compat load-bearing: empty `Aliases` ⇒ byte-identical single-type behavior.

---

### Task 1: match-set core

**Files:**
- Modify: `internal/types/actions_http.go` (`WSReceiveAction` += `Aliases`)
- Modify: `internal/head/agent/ws_protocol.go` (add `matchAnyType`)
- Modify: `internal/head/agent/websocket.go` (`doReceive` predicate)
- Modify: `internal/head/agent/types.go` (`TestStep` += `Aliases`)
- Modify: `internal/head/agent/execute_phases_steps.go` (`stepToAction` pass-through)
- Test: `internal/head/agent/ws_protocol_test.go` (or nearest test file) + `websocket_test.go` + `execute_phases_steps_test.go`

**Interfaces:**
- Produces: `WSReceiveAction.Aliases []string`; `TestStep.Aliases []string`; `matchAnyType(framing, data, types, typePath) bool`.

**Reviewer note (controller):** sonnet. Verify backwards-compat (empty aliases == old `matchType` single-call), the OR is short-circuit safe (binary invalid-base64 alias returns false, not panic), and asserts still apply to whichever frame matched.

- [ ] **Step 1: Write failing tests**

Add to a ws_protocol test file (e.g. `internal/head/agent/ws_protocol_test.go`; if `matchType` isn't tested there yet, add `matchType`+`matchAnyType` tests together):

```go
func TestMatchAnyType(t *testing.T) {
	const frame = `{"type":"session:output-batch","payload":{"lines":["hi"]}}`
	tests := []struct {
		name  string
		types []string
		want  bool
	}{
		{"primary match", []string{"session:output-batch"}, true},
		{"alias match primary absent", []string{"session:output", "session:output-batch"}, true},
		{"alias match order independent", []string{"session:output-batch", "session:output"}, true},
		{"no match", []string{"session:output", "device:ack"}, false},
		{"empty set never matches", nil, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, matchAnyType("", []byte(frame), tc.types, "type"))
		})
	}
	// Backwards-compat: matchAnyType over a 1-element set == matchType.
	require.Equal(t, matchType("", []byte(frame), "session:output-batch", "type"),
		matchAnyType("", []byte(frame), []string{"session:output-batch"}, "type"))
}
```

Add to `internal/head/agent/websocket_test.go` (a receive with aliases matches an alias-type frame; asserts apply to the matched alias frame):

```go
func TestWSReceiveAliasesMatch(t *testing.T) {
	url := newWSTestServer(t, func(conn *websocket.Conn) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		// Server emits the BATCHED form, not the primary the client awaits. Write
		// it right after accept; the client's read pump buffers it whenever it
		// arrives, so no handshake/read ordering is needed.
		_ = conn.Write(ctx, websocket.MessageText,
			[]byte(`{"type":"session:output-batch","payload":{"lines":["x"]}}`))
		_, _, _ = conn.Read(ctx) // block until close
	})
	ex := newWSExecutor()
	ctx := context.Background()
	_ = ex.Execute(ctx, types.WSConnectAction{URL: url, ConnectionID: "c1"}) // establish
	res := ex.Execute(ctx, types.WSReceiveAction{
		ConnectionID: "c1", Type: "session:output", Aliases: []string{"session:output-batch"},
		Assert: map[string]any{"payload.lines": []any{"x"}}, Timeout: 2,
	})
	require.True(t, res.Success(), "receive should match the alias type; got %+v", res)
}
```

Add to `internal/head/agent/execute_phases_steps_test.go` (TestStep.Aliases flows to WSReceiveAction.Aliases):

```go
func TestStepToActionReceiveAliases(t *testing.T) {
	action, err := stepToAction(&TestCase{Target: "ws://x"}, TestStep{
		Action: "ws_receive", ConnectionID: "c1", Type: "session:output",
		Aliases: []string{"session:output-batch"}, Timeout: 2,
	})
	require.NoError(t, err)
	wr, ok := action.(types.WSReceiveAction)
	require.True(t, ok)
	require.Equal(t, "session:output", wr.Type)
	require.Equal(t, []string{"session:output-batch"}, wr.Aliases)
}
```

- [ ] **Step 2: Run tests → FAIL (undefined matchAnyType / Aliases fields)**

Run: `go test -run 'TestMatchAnyType|TestWSReceiveAliasesMatch|TestStepToActionReceiveAliases' ./internal/head/agent/`
Expected: compile failure (`undefined: matchAnyType`, unknown fields).

- [ ] **Step 3: Implement**

`internal/types/actions_http.go` — add `Aliases` to `WSReceiveAction` (after `Type`):
```go
	Type    string `json:"type"`
	// Aliases are additional routing types that also satisfy this receive. A frame
	// whose type_path is Type OR any Aliases matches. Empty ⇒ single-type
	// behavior. Asserts apply to whichever frame matched.
	Aliases []string `json:"aliases,omitempty"`
```
(`Validate` unchanged — Type still required; Aliases optional.)

`internal/head/agent/ws_protocol.go` — add after `matchType`:
```go
// matchAnyType reports whether a received frame matches ANY of types under the
// connection's framing. It is matchType over a set; the empty set never matches.
// Used by ws_receive with aliases (e.g. session:output vs session:output-batch).
func matchAnyType(framing string, data []byte, types []string, typePath string) bool {
	for _, t := range types {
		if matchType(framing, data, t, typePath) {
			return true
		}
	}
	return false
}
```

`internal/head/agent/websocket.go` — in `doReceive`, replace the readMatching predicate (lines ~617-619):
```go
	want := append([]string{a.Type}, a.Aliases...)
	matched, seen, status := readMatching(entry, func(m wsMsg) bool {
		return matchAnyType(framing, m.data, want, path)
	}, timeout)
```

`internal/head/agent/types.go` — add `Aliases` to `TestStep` (after `Type`):
```go
	Type    string `json:"type,omitempty"`
	Aliases []string `json:"aliases,omitempty"` // ws_receive: additional matching types
```

`internal/head/agent/execute_phases_steps.go` — in `stepToAction`, pass `Aliases`:
```go
	case "ws_receive":
		return types.WSReceiveAction{ConnectionID: s.ConnectionID, Type: s.Type,
			Aliases: s.Aliases, Assert: s.Asserts, Timeout: s.Timeout, Decisive: true}, nil
```

- [ ] **Step 4: Run tests → PASS**

Run: `go test -race -count=1 -run 'TestMatchAnyType|TestWSReceiveAliasesMatch|TestStepToActionReceiveAliases' ./internal/head/agent/`
Then the full agent package: `go test -race -count=1 ./internal/head/agent/` (backwards-compat: existing WS/Steps tests green).

- [ ] **Step 5: Commit**

```bash
git add internal/types/actions_http.go internal/head/agent/ws_protocol.go internal/head/agent/websocket.go internal/head/agent/types.go internal/head/agent/execute_phases_steps.go internal/head/agent/ws_protocol_test.go internal/head/agent/websocket_test.go internal/head/agent/execute_phases_steps_test.go
git commit -m "feat(ws): ws_receive type aliases (match-set)"
```

---

### Task 2: ws_relay aliases pass-through + docs + prompt

**Files:**
- Modify: `internal/head/scout/ws_relay.go` (`relayStep` += `Aliases`; assembly pass-through)
- Modify: `internal/head/scout/ws_relay_test.go` (a relay receive with aliases)
- Modify: `cerberus-docs/executors/websocket.md` (`ws_receive` aliases note)
- Modify: `internal/head/scout/prompts.go` (relay bullet: receive may list aliases)

**Reviewer note (controller):** small; sonnet review or inline + opus final.

- [ ] **Step 1: ws_relay pass-through**

In `internal/head/scout/ws_relay.go`, add `Aliases` to `relayStep` and pass it in assembly:
```go
type relayStep struct {
	Do      string         `json:"do"`
	Role    string         `json:"role"`
	Type    string         `json:"type"`
	Aliases []string       `json:"aliases"` // receive only: additional matching types
	Assert  map[string]any `json:"assert"`
}
```
In the `case "receive":` assembly, add `Aliases: st.Aliases`.

- [ ] **Step 2: Test**

Add to `internal/head/scout/ws_relay_test.go` (extend the happy-path intent or add a focused subtest): a receive step with `aliases` is assembled onto the `ws_receive` step's `Aliases`. Assert `got.Steps[<receive>].Aliases`.

- [ ] **Step 3: Docs + prompt**

`cerberus-docs/executors/websocket.md` under `### ws_receive`: add a line — "`aliases` (optional): additional routing types that also satisfy the receive (a frame matching the `type` OR any `aliases` succeeds); for protocols that emit one message under several wire types, e.g. `session:output` vs `session:output-batch`."

`internal/head/scout/prompts.go` relay bullet: append "a receive step may list `aliases` (additional matching types, e.g. session:output-batch alongside session:output)." (inline raw-string edit, no backticks/`${}`).

- [ ] **Step 4: make check + commit**

Run: `make check`. Commit:
```bash
git add internal/head/scout/ws_relay.go internal/head/scout/ws_relay_test.go cerberus-docs/executors/websocket.md internal/head/scout/prompts.go
git commit -m "feat(ws): ws_relay + docs aliases pass-through"
```

---

## Post-implementation (controller)
- [ ] **Whole-branch review (opus):** `d452d42..HEAD`. Verify: backwards-compat (empty aliases == old); OR short-circuit safe; asserts apply to matched alias frame; no protocol-schema change; constraints; `make check` green.
- [ ] **Finish:** ff-merge main + delete branch (NO push).
- [ ] **Memory + ledger:** note F4 done (F3 next).
