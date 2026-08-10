# Generic WS Send Payload Templating Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [`) syntax for tracking.

**Goal:** Make deterministic `ws_send` steps carry templated payloads (`{{role.param}}` / `{{param}}`) resolved at send time from provisioned actor state, so the ws-realtime reqresp exchange completes both directions and autonomous message-edge coverage rises above the current `1/63` baseline.

**Architecture:** (A) a runtime resolver in the WS executor (`resolveMessageBody`) substitutes double-brace placeholders against `e.idx.ActorPathParams` just before writing the frame — the owning actor's params for `{{param}}`, a declared role's actor for `{{role.param}}`; (B) the connection entry gains the `credentialRef` that opened it so owning-actor lookup works; (C) a `wsSendBody(typ, payload)` envelope builder plus a per-role `request_payload` declaration feed the resolver via the existing `wsRequestResponseCases` generator.

**Tech Stack:** Go 1.25, table-driven tests, existing `agent`/`scout`/`project` packages.

**Spec:** `cerberus-docs/superpowers/specs/2026-08-10-ws-send-payload-templating-design.md`

## Global Constraints

- Module: `github.com/binoctal/cerberus`, Go 1.25, no CGo.
- Commit author: `binoctal <binoctal@gmail.com>`, NO `Co-Authored-By`.
- Code comments and commit messages in English; match existing comment density.
- All docs go in `cerberus-docs/`, never `docs/`.
- Zero regression: roles without `request_payload` and messages without placeholders produce byte-identical sends and identical case generation.
- Verify honestly: live coverage claims require a real run; never assert a fraction from unit tests alone.
- Placeholders are **double-brace** `{{...}}` (single-brace collides with JSON object braces in the marshaled body).

---

## File Structure

- `internal/project/protocol_schema.go` — add `RequestPayload` to `ProtocolRole`.
- `internal/project/validate_protocol.go` — add `validateProtocolRequestPayload`, wire into `validateProtocolRole`.
- `internal/project/validate_protocol_test.go` — validation test.
- `internal/head/agent/websocket.go` — `resolveMessageBody`; `wsEntry.credentialRef`; thread `credentialRef` through `store`/`doConnect`; call resolver in `doSend`.
- `internal/head/agent/websocket_test.go` — resolver unit test.
- `internal/head/scout/ws_cases.go` — `wsSendBody(typ, payload)`; update call sites; `wsRequestResponseCases` emits declared payload.
- `internal/head/scout/ws_cases_test.go` — `wsSendBody` + generator payload tests.
- `internal/head/scout/assembly.go` — update the one `wsSendBody` call site to the new signature.
- `dogfood/ws-realtime/.cerberus/protocols/open-agents.yaml` — add `bridge.request_payload`.
- `internal/head/agent/pathcoverage_live_integration_test.go` — live resolver proof.
- `cerberus-docs/technical/validation/2026-08-07-openagents-integration-test-report.md` — append the autonomous two-direction re-verification.

---

### Task 1: `ProtocolRole.RequestPayload` schema field + validation

**Files:**
- Modify: `internal/project/protocol_schema.go` (add field after `Responses`, ~line 73)
- Modify: `internal/project/validate_protocol.go` (add validator; wire at ~line 83-85)
- Test: `internal/project/validate_protocol_test.go`

**Interfaces:**
- Produces: `ProtocolRole.RequestPayload map[string]map[string]string` (`yaml:"request_payload,omitempty"`, JSON `request_payload,omitempty`) — outer key = received message type, inner = payload field → placeholder template. Empty/absent ⇒ none (backward-compatible).
- Produces: `validateProtocolRequestPayload(roleName string, role *ProtocolRole) error`, wired into `validateProtocolRole` right after the existing `validateProtocolResponses(name, role)` call.

- [ ] **Step 1: Write the failing test**

Append to `internal/project/validate_protocol_test.go` (match the file's existing `TestValidateProtocolRole_Responses` style — it builds a `*Protocol` via an inline helper and calls `validateProtocol`):

```go
func TestValidateProtocolRole_RequestPayload(t *testing.T) {
	mk := func(rp map[string]map[string]string) *Protocol {
		return &Protocol{
			TypePath: "type",
			Auth:     &ProtocolAuth{Strategy: "query", Param: "token"},
			Roles: map[string]*ProtocolRole{
				"web":    {Params: map[string]string{"type": "web"}},
				"bridge": {Params: map[string]string{"type": "bridge"}, RequestPayload: rp},
			},
		}
	}

	if err := validateProtocol(mk(map[string]map[string]string{
		"session:start": {"deviceId": "{{bridge.deviceId}}"},
	})); err != nil {
		t.Fatalf("valid request_payload must pass: %v", err)
	}
	if err := validateProtocol(mk(map[string]map[string]string{
		"": {"deviceId": "x"},
	})); err == nil {
		t.Fatalf("empty received_type key must fail")
	}
	if err := validateProtocol(mk(map[string]map[string]string{
		"session:start": {"": "x"},
	})); err == nil {
		t.Fatalf("empty field name must fail")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/project/ -run TestValidateProtocolRole_RequestPayload -v`
Expected: FAIL — `RequestPayload` field undefined (compile error) or validation absent.

- [ ] **Step 3: Add the field**

In `internal/project/protocol_schema.go`, add to `ProtocolRole` immediately after the existing `Responses` field:

```go
	// RequestPayload declares the payload fields a requester must include when
	// sending a given received_type to this role (received_type → {field → template}).
	// Templates carry {{param}}/{{role.param}} placeholders resolved at send time
	// from provisioned actor state. Drives the deterministic two-role
	// request-response generator. Empty ⇒ the requester sends a bare type envelope.
	RequestPayload map[string]map[string]string `yaml:"request_payload,omitempty" json:"request_payload,omitempty"`
```

- [ ] **Step 4: Add the validator + wire it**

In `internal/project/validate_protocol.go`, add after `validateProtocolResponses`:

```go
// validateProtocolRequestPayload checks a role's request_payload map: the outer
// received_type key and every inner field name must be non-empty. Placeholder
// templates are NOT checked for resolvability here — resolution depends on
// runtime provisioning and surfaces as a clear send-time failure.
func validateProtocolRequestPayload(roleName string, role *ProtocolRole) error {
	for recvType, fields := range role.RequestPayload {
		if recvType == "" {
			return fmt.Errorf("roles[%q].request_payload: received_type key is empty", roleName)
		}
		for field := range fields {
			if field == "" {
				return fmt.Errorf("roles[%q].request_payload[%q]: field name is empty", roleName, recvType)
			}
		}
	}
	return nil
}
```

Wire it into `validateProtocolRole`: locate the block

```go
	if err := validateProtocolResponses(name, role); err != nil {
		return err
	}
	return nil
```

and insert the request_payload check before the final `return nil`:

```go
	if err := validateProtocolResponses(name, role); err != nil {
		return err
	}
	if err := validateProtocolRequestPayload(name, role); err != nil {
		return err
	}
	return nil
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/project/ -run TestValidateProtocolRole_RequestPayload -v`
Expected: PASS.

- [ ] **Step 6: Run the package (no regression)**

Run: `go test ./internal/project/`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/project/protocol_schema.go internal/project/validate_protocol.go internal/project/validate_protocol_test.go
git -c user.name=binoctal -c user.email=binoctal@gmail.com \
  commit -m "feat(project): ProtocolRole.RequestPayload declaration + validation"
```

---

### Task 2: `resolveMessageBody` + `wsEntry.credentialRef` + `doSend` wiring

**Files:**
- Modify: `internal/head/agent/websocket.go` (`wsEntry` struct ~line 54; `store` ~line 146; `doConnect` call ~line 504; `doSend` ~line 907; new `resolveMessageBody`)
- Test: `internal/head/agent/websocket_test.go`

**Interfaces:**
- Consumes: `e.idx.ActorPathParams` (map actor → {param: value}), `entry.protocol.Roles[*].CredentialRef`.
- Produces: `wsEntry.credentialRef string` (set at connect); `store(id, conn, ctx, proto, credentialRef)`; method `func (e *WebSocketExecutor) resolveMessageBody(entry *wsEntry, msg string) (string, error)`. `doSend` resolves `a.Message` before writing.

- [ ] **Step 1: Write the failing test**

Append to `internal/head/agent/websocket_test.go` (package `agent`; reuse `project` import). The test builds a minimal executor + entry directly, so it needs no live socket:

```go
func TestResolveMessageBody(t *testing.T) {
	p := &project.Protocol{Roles: map[string]*project.ProtocolRole{
		"bridge": {CredentialRef: "bridge-actor"},
	}}
	e := &WebSocketExecutor{idx: &WSProtocolIndex{
		ActorPathParams: map[string]map[string]string{
			"web-actor":    {"userId": "u1"},
			"bridge-actor": {"deviceId": "dev_9", "userId": "u1"},
		},
	}}
	entry := &wsEntry{protocol: p, credentialRef: "web-actor"}

	// owning-actor placeholder resolves from the connection owner's params.
	got, err := e.resolveMessageBody(entry, `{"type":"x","payload":{"userId":"{{userId}}"}}`)
	require.NoError(t, err)
	assert.JSONEq(t, `{"type":"x","payload":{"userId":"u1"}}`, got)

	// cross-actor placeholder resolves from the declared role's actor.
	got, err = e.resolveMessageBody(entry, `{"type":"session:start","payload":{"deviceId":"{{bridge.deviceId}}"}}`)
	require.NoError(t, err)
	assert.JSONEq(t, `{"type":"session:start","payload":{"deviceId":"dev_9"}}`, got)

	// no placeholder → verbatim; JSON object braces are untouched (no false match).
	in := `{"type":"session:start","payload":{"deviceId":"none"}}`
	got, err = e.resolveMessageBody(entry, in)
	require.NoError(t, err)
	assert.Equal(t, in, got)

	// undeclared role in a dot placeholder is left literal, no error.
	got, err = e.resolveMessageBody(entry, `{"k":"{{ghost.x}}"}`)
	require.NoError(t, err)
	assert.Equal(t, `{"k":"{{ghost.x}}"}`, got)

	// declared role but missing param → hard fail naming the placeholder.
	_, err = e.resolveMessageBody(entry, `{"k":"{{bridge.noSuch}}"}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unresolved placeholder")
	assert.Contains(t, err.Error(), "{{bridge.noSuch}}")

	// owning-actor missing param → hard fail.
	_, err = e.resolveMessageBody(entry, `{"k":"{{noSuch}}"}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unresolved placeholder")
}
```

> NOTE for the implementer: confirm `WebSocketExecutor`, `WSProtocolIndex`, and `wsEntry` are all in package `agent` (they are — `websocket.go` / `ws_protocol.go`). If `websocket_test.go` does not already import `github.com/stretchr/testify/require`, add it. The executor's other fields are not needed; a struct literal with only `idx` set compiles because `WebSocketExecutor` has no required non-pointer fields at construction.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/head/agent/ -run TestResolveMessageBody -v`
Expected: FAIL — `resolveMessageBody` undefined (compile error).

- [ ] **Step 3: Add `credentialRef` to `wsEntry` and thread it through `store`**

In `internal/head/agent/websocket.go`, add a field to the `wsEntry` struct (after `protocol`):

```go
type wsEntry struct {
	conn          *websocket.Conn
	ctx           context.Context   // per-case ctx; cancellation closes the conn
	protocol      *project.Protocol // service protocol resolved at connect; nil = M0
	credentialRef string            // actor that opened this conn; for {{param}}/{{role.param}} send-body templating
	msgs          chan wsMsg        // buffered (256); pump pushes every inbound frame
	pumpErr       error             // set when the pump exits (read error / ctx done)
	done          chan struct{}     // closed when the pump has exited
	readMu        sync.Mutex        // serializes channel consumption (one consumer at a time)
	pending       wsMsg             // a frame peeked then put back by readMatchingAll
	hasPending    bool              // pending holds a frame when true
}
```

Change `store` to accept and persist the credential ref (replace the existing signature + struct literal):

```go
func (e *WebSocketExecutor) store(id string, conn *websocket.Conn, ctx context.Context, proto *project.Protocol, credentialRef string) {
	entry := &wsEntry{
		conn:          conn,
		ctx:           ctx,
		protocol:      proto,
		credentialRef: credentialRef,
		msgs:          make(chan wsMsg, 256),
		done:          make(chan struct{}),
	}
```

Leave the rest of `store` (the `e.mu.Lock()` block, read pump, ctx-cancel goroutine) unchanged.

Update the single call site in `doConnect` (~line 504) to pass the already-resolved `credentialRef`:

```go
	e.store(key, conn, ctx, proto, credentialRef)
```

(`credentialRef` is in scope at that point in `doConnect` — it was resolved earlier in the function. `store` has no other callers.)

- [ ] **Step 4: Implement `resolveMessageBody`**

Add the method to `internal/head/agent/websocket.go` (near `resolveURLParams`, ~line 760). Add `"regexp"` to the import block if it is not already present (check the file's imports — `regexp` is likely already imported; if not, add it):

```go
// wsBodyPlaceholderRe matches {{param}} / {{role.param}} send-body placeholders.
// Double braces avoid collision with JSON object braces in the marshaled body.
// The inner class is restricted to identifier/dot characters so JSON can never
// match. (Consistent with the {{uuid}} role-param sentinel convention.)
var wsBodyPlaceholderRe = regexp.MustCompile(`\{\{([A-Za-z0-9_.]+)\}\}`)

// resolveMessageBody substitutes {{param}} / {{role.param}} placeholders in a
// ws_send body against provisioned actor state: {{param}} reads the connection
// owner's captured path params; {{role.param}} reads the named declared role's
// actor params (cross-actor — a web sender can reach a bridge peer's deviceId).
// A declared-role or owning-actor placeholder with no captured value is a hard
// error (clear failure over a silent malformed send); a dot placeholder whose
// role is NOT declared is left literal (not interpreted). A body with no {{ is
// returned verbatim.
func (e *WebSocketExecutor) resolveMessageBody(entry *wsEntry, msg string) (string, error) {
	if !strings.Contains(msg, "{{") {
		return msg, nil
	}
	var own map[string]string
	if e.idx != nil && entry.credentialRef != "" {
		own = e.idx.ActorPathParams[entry.credentialRef]
	}
	var unresolved string
	out := wsBodyPlaceholderRe.ReplaceAllStringFunc(msg, func(match string) string {
		token := match[2 : len(match)-2] // strip the {{ }} delimiters
		if i := strings.IndexByte(token, '.'); i > 0 {
			role, param := token[:i], token[i+1:]
			if entry.protocol != nil {
				if r, ok := entry.protocol.Roles[role]; ok && r != nil {
					// Declared role: resolve from its actor; a missing param is an error.
					if r.CredentialRef != "" && e.idx != nil {
						if v, ok := e.idx.ActorPathParams[r.CredentialRef][param]; ok {
							return v
						}
					}
					if unresolved == "" {
						unresolved = match
					}
					return match
				}
			}
			// Undeclared role: not a placeholder, leave it literal.
			return match
		}
		// Owning-actor {{param}}.
		if own != nil {
			if v, ok := own[token]; ok {
				return v
			}
		}
		if unresolved == "" {
			unresolved = match
		}
		return match
	})
	if unresolved != "" {
		return "", fmt.Errorf("unresolved placeholder %s", unresolved)
	}
	return out, nil
}
```

- [ ] **Step 5: Call the resolver in `doSend`**

In `doSend` (~line 907), immediately after the successful `e.lookup` (before `conn := entry.conn`), resolve the message:

```go
func (e *WebSocketExecutor) doSend(ctx context.Context, a types.WSSendAction, start time.Time) types.ExecutorResult {
	entry, ok := e.lookup(caseNamespace(ctx, a.ConnectionID))
	if !ok {
		return types.WSResult{OK: false, Err: fmt.Sprintf("unknown connection_id: %s", a.ConnectionID), Latency: time.Since(start)}
	}
	resolved, rerr := e.resolveMessageBody(entry, a.Message)
	if rerr != nil {
		return types.WSResult{OK: false, Err: "ws send: " + rerr.Error(), Latency: time.Since(start)}
	}
	conn := entry.conn
```

Then replace the two write sites that use `a.Message` with `resolved`: the binary branch (`base64.StdEncoding.DecodeString(a.Message)` → `...DecodeString(resolved)`) and the text branch (`conn.Write(writeCtx, websocket.MessageText, []byte(a.Message))` → `[]byte(resolved)`). No other `doSend` logic changes.

- [ ] **Step 6: Run test to verify it passes**

Run: `go test ./internal/head/agent/ -run TestResolveMessageBody -v`
Expected: PASS.

- [ ] **Step 7: Run the package + vet (no regression, all `store`/signature call sites covered)**

Run: `go test ./internal/head/agent/ && go vet ./internal/head/agent/`
Expected: PASS, vet clean.

- [ ] **Step 8: Commit**

```bash
git add internal/head/agent/websocket.go internal/head/agent/websocket_test.go
git -c user.name=binoctal -c user.email=binoctal@gmail.com \
  commit -m "feat(agent): resolve {{role.param}} placeholders in ws_send bodies at send time"
```

---

### Task 3: `wsSendBody(typ, payload)` envelope builder + update call sites

**Files:**
- Modify: `internal/head/scout/ws_cases.go` (`wsSendBody` ~line 350; call sites at ~line 304, ~635, ~637)
- Modify: `internal/head/scout/assembly.go` (call site ~line 160)
- Test: `internal/head/scout/ws_cases_test.go`

**Interfaces:**
- Produces: `wsSendBody(typ string, payload map[string]string) string` — `{"type":<typ>}` when payload is empty/nil (byte-identical to today); `{"type":<typ>,"payload":{...}}` (nested, deterministic key order) when payload is non-empty.

- [ ] **Step 1: Write the failing test**

Append to `internal/head/scout/ws_cases_test.go`:

```go
func TestWsSendBody(t *testing.T) {
	// No payload ⇒ bare type envelope, byte-identical to the historical form.
	assert.Equal(t, `{"type":"session:start"}`, wsSendBody("session:start", nil))
	assert.Equal(t, `{"type":"session:start"}`, wsSendBody("session:start", map[string]string{}))
	// Payload present ⇒ nested envelope with the template carried verbatim.
	// Keys are deterministically ordered by encoding/json (alphabetical).
	assert.JSONEq(t, `{"type":"session:start","payload":{"deviceId":"{{bridge.deviceId}}"}}`,
		wsSendBody("session:start", map[string]string{"deviceId": "{{bridge.deviceId}}"}))
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/head/scout/ -run TestWsSendBody -v`
Expected: FAIL — `wsSendBody` takes one arg (compile error: wrong number of parameters).

- [ ] **Step 3: Change `wsSendBody` signature + body**

Replace `wsSendBody` in `internal/head/scout/ws_cases.go`:

```go
// wsSendBody builds the JSON payload for a ws_send step. With no payload it
// emits the bare {"type": "<typ>"} envelope (the standard WS routing-key shape);
// with a payload it emits {"type": "<typ>", "payload": {<field>: <value>, ...}}.
// encoding/json sorts map keys, so the nested form is deterministic. Placeholder
// templates (e.g. "{{bridge.deviceId}}") are carried verbatim and resolved at
// send time by the executor. Marshal of these maps cannot fail; the error is
// intentionally ignored.
func wsSendBody(typ string, payload map[string]string) string {
	if len(payload) == 0 {
		b, _ := json.Marshal(map[string]string{"type": typ})
		return string(b)
	}
	b, _ := json.Marshal(map[string]any{"type": typ, "payload": payload})
	return string(b)
}
```

- [ ] **Step 4: Update all call sites to the new signature**

There are four call sites (grep `wsSendBody(` to confirm). Update each:

1. `internal/head/scout/assembly.go` ~line 160 (LLM-authored ws_send) — pass `nil`:

```go
			Message: wsSendBody(llm.StrField(call, "type"), nil),
```

2. `internal/head/scout/ws_cases.go` ~line 304 (`wsStepsCase` send) — pass `nil`:

```go
			Message: wsSendBody(ex.sendType, nil),
```

3. `internal/head/scout/ws_cases.go` ~line 635 (`wsRequestResponseCases` requester send) — pass `nil` for now (Task 4 fills the real payload):

```go
					Message: wsSendBody(recvType, nil),
```

4. `internal/head/scout/ws_cases.go` ~line 637 (`wsRequestResponseCases` responder reply send) — pass `nil`:

```go
					Message: wsSendBody(sendType, nil),
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/head/scout/ -run TestWsSendBody -v`
Expected: PASS.

- [ ] **Step 6: Run the package (no regression — all call sites compile, behavior unchanged where payload is nil)**

Run: `go test ./internal/head/scout/`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/head/scout/ws_cases.go internal/head/scout/ws_cases_test.go internal/head/scout/assembly.go
git -c user.name=binoctal -c user.email=binoctal@gmail.com \
  commit -m "feat(scout): wsSendBody carries an optional payload map"
```

---

### Task 4: `wsRequestResponseCases` emits the declared request payload

**Files:**
- Modify: `internal/head/scout/ws_cases.go` (`wsRequestResponseCases` requester send step, ~line 635)
- Test: `internal/head/scout/ws_cases_test.go`

**Interfaces:**
- Consumes: `ProtocolRole.RequestPayload` (Task 1), `wsSendBody(typ, payload)` (Task 3).
- Produces: the requester's send step carries `role.RequestPayload[recvType]` when the responder role declares it; bare type envelope otherwise.

- [ ] **Step 1: Write the failing test**

Append to `internal/head/scout/ws_cases_test.go`:

```go
func TestWsRequestResponseCases_RequestPayload(t *testing.T) {
	svc := project.Service{
		Name: "realtime",
		URL:  "http://localhost:8989/ws/{userId}",
		Protocol: &project.Protocol{TypePath: "type", Roles: map[string]*project.ProtocolRole{
			"web": {Params: map[string]string{"type": "web"}},
			"bridge": {
				Params:        map[string]string{"type": "bridge"},
				Responses:     map[string]string{"session:start": "session:created"},
				RequestPayload: map[string]map[string]string{
					"session:start": {"deviceId": "{{bridge.deviceId}}"},
				},
			},
		}},
		Vocabulary: &project.Vocabulary{Edges: []project.VocabEdge{
			{FromRole: "web", ToRole: "bridge", Type: "session:start", Trigger: "message_handled"},
			{FromRole: "bridge", ToRole: "web", Type: "session:created", Trigger: "message_handled"},
		}},
	}
	cases, connected := wsRequestResponseCases(svc)
	require.Len(t, cases, 1)
	// Step index 2 is the requester's ws_send of the received type.
	sendStep := cases[0].Steps[2]
	require.Equal(t, "ws_send", sendStep.Action)
	assert.JSONEq(t, `{"type":"session:start","payload":{"deviceId":"{{bridge.deviceId}}"}}`, sendStep.Message)
	assert.True(t, connected["web"] && connected["bridge"])
}

func TestWsRequestResponseCases_RequestPayloadAbsent(t *testing.T) {
	svc := project.Service{
		Name: "realtime", URL: "http://localhost:8989/ws/{userId}",
		Protocol: &project.Protocol{TypePath: "type", Roles: map[string]*project.ProtocolRole{
			"web":    {Params: map[string]string{"type": "web"}},
			"bridge": {Params: map[string]string{"type": "bridge"}, Responses: map[string]string{"session:start": "session:created"}},
		}},
		Vocabulary: &project.Vocabulary{Edges: []project.VocabEdge{
			{FromRole: "web", ToRole: "bridge", Type: "session:start", Trigger: "message_handled"},
			{FromRole: "bridge", ToRole: "web", Type: "session:created", Trigger: "message_handled"},
		}},
	}
	cases, _ := wsRequestResponseCases(svc)
	require.Len(t, cases, 1)
	// No request_payload ⇒ bare type envelope (byte-identical to pre-feature).
	assert.Equal(t, `{"type":"session:start"}`, cases[0].Steps[2].Message)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/head/scout/ -run TestWsRequestResponseCases_RequestPayload -v`
Expected: FAIL — requester send Message is the bare `{"type":"session:start"}` (Task 3 left it nil).

- [ ] **Step 3: Wire the declared payload into the requester send**

In `wsRequestResponseCases` (`internal/head/scout/ws_cases.go`), locate the requester's `ws_send` step (the one whose `ConnectionID: requester` and `Message: wsSendBody(recvType, nil)`). Change its `Message` to read the responder role's declared payload:

```go
						{Action: "ws_send", ConnectionID: requester, Message: wsSendBody(recvType, role.RequestPayload[recvType])},
```

`role.RequestPayload[recvType]` returns `nil` when the map or key is absent, so `wsSendBody` emits the bare envelope — byte-identical to the no-declaration case. Leave the responder reply send (`wsSendBody(sendType, nil)`) unchanged.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/head/scout/ -run TestWsRequestResponseCases -v`
Expected: PASS (new tests + existing `TestWsRequestResponseCases` / `_NoneWhenNoResponses`).

- [ ] **Step 5: Run the scout package (no regression)**

Run: `go test ./internal/head/scout/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/head/scout/ws_cases.go internal/head/scout/ws_cases_test.go
git -c user.name=binoctal -c user.email=binoctal@gmail.com \
  commit -m "feat(scout): wsRequestResponseCases emits declared request payload"
```

---

### Task 5: ws-realtime dogfood declares `bridge.request_payload`

**Files:**
- Modify: `dogfood/ws-realtime/.cerberus/protocols/open-agents.yaml`

**Interfaces:**
- Consumes: Tasks 1, 3, 4.
- Produces: an autonomous run whose reqresp case sends `{"type":"session:start","payload":{"deviceId":"{{bridge.deviceId}}"}}`, resolved at send time to the provisioned bridge device id.

- [ ] **Step 1: Add the declaration**

In `dogfood/ws-realtime/.cerberus/protocols/open-agents.yaml`, add `request_payload` to the `bridge` role:

```yaml
  bridge:
    credential_ref: bridge-actor
    params:
      type: bridge
      deviceId: "{deviceId}"   # templated from bridge-actor's provisioned path params at CONNECT
    responses:
      session:start: session:created   # reqresp generator drives this exchange
    request_payload:            # NEW: payload the requester (web) must include for the DO to relay
      session:start:
        deviceId: "{{bridge.deviceId}}"   # resolved at SEND time from bridge-actor's path params
```

(Only the `request_payload` block is new; the surrounding keys are unchanged. Note the two deviceId templating sites are distinct: `params.deviceId: "{deviceId}"` is the connect-time role discriminator, single-brace, resolved from the bridge's OWN params; `request_payload...deviceId: "{{bridge.deviceId}}"` is the send-time payload, double-brace, resolved cross-actor.)

- [ ] **Step 2: Validate the config loads**

Run: `go build -o /tmp/cerberus ./cmd/cerberus && /tmp/cerberus run --config dogfood/ws-realtime/.cerberus/project.yaml --dir dogfood/ws-realtime --goal "smoke" 2>&1 | head -5`
Expected: the run STARTS (session created) with NO config/validation errors. Kill it once the session is created — this step only confirms the schema parses and the protocol validates (the new `request_payload` block is accepted).

- [ ] **Step 3: Commit**

```bash
git add dogfood/ws-realtime/.cerberus/protocols/open-agents.yaml
git -c user.name=binoctal -c user.email=binoctal@gmail.com \
  commit -m "feat(dogfood): ws-realtime bridge declares request_payload for session:start"
```

---

### Task 6: Live integration test — send-time placeholder resolution completes both directions

**Files:**
- Modify: `internal/head/agent/pathcoverage_live_integration_test.go` (`//go:build integration`, package `agent`)

**Interfaces:**
- Consumes: Task 2 (`resolveMessageBody` via the executor), the existing `setupOpenAgents` / `newStepExecutionWithIdx` / `exercisedEdgesMirror`.
- Produces: a `//go:build integration` test proving a `ws_send` whose body carries a literal `{{bridge.deviceId}}` placeholder is resolved against the bridge actor's provisioned params and the two-role exchange completes BOTH directions on real open-agents.

> Rationale: `TestPathCoverage_LiveTwoRoleExchange` (already in this file) proves the relay completes both directions when the deviceId is hand-injected via `fmt.Sprintf`. This new test proves the **templating mechanism** specifically: the placeholder survives the plan→executor path as a literal string and is materialized at send time. It is distinct evidence.

- [ ] **Step 1: Write the test**

Append to `internal/head/agent/pathcoverage_live_integration_test.go`. The test seeds `ActorPathParams` (which `setupOpenAgents` does NOT populate) so `{{bridge.deviceId}}` can resolve, then sends the literal placeholder body:

```go
// TestPathCoverage_LiveSendBodyTemplating proves the send-time placeholder
// resolver: a ws_send body carrying the literal {{bridge.deviceId}} placeholder
// is resolved against the bridge actor's provisioned path params at send time,
// so open-agents' DO relays session:start to the bridge (room.ts gates on
// payload.deviceId) and BOTH exchange directions complete. Distinct from
// TestPathCoverage_LiveTwoRoleExchange, which hand-injects the deviceId.
func TestPathCoverage_LiveSendBodyTemplating(t *testing.T) {
	f := setupOpenAgents(t, false)
	// setupOpenAgents populates ActorTokens but NOT ActorPathParams; the resolver
	// reads ActorPathParams, so seed the bridge's provisioned device id there.
	f.wsIdx.ActorPathParams = map[string]map[string]string{
		"web-actor":    {"userId": f.userId},
		"bridge-actor": {"deviceId": f.deviceId, "userId": f.userId},
	}
	tc := &TestCase{
		ID:     "tc-sendbody-template",
		Target: "ws://localhost:8989/ws/" + f.userId,
		Steps: []TestStep{
			{Action: "ws_connect", Role: "web", ConnectionID: "web"},
			{Action: "ws_connect", Role: "bridge", ConnectionID: "bridge"},
			{Action: "ws_receive", ConnectionID: "web", Type: "device:online", Timeout: 3},
			// Literal placeholder in the body; resolved to f.deviceId at send time.
			{Action: "ws_send", ConnectionID: "web", Message: `{"type":"session:start","payload":{"deviceId":"{{bridge.deviceId}}"}}`},
			{Action: "ws_receive", ConnectionID: "bridge", Type: "session:start", Timeout: 3},
			{Action: "ws_send", ConnectionID: "bridge", Message: `{"type":"session:created"}`},
			{Action: "ws_receive", ConnectionID: "web", Type: "session:created", Timeout: 3},
		},
	}
	se := newStepExecutionWithIdx(t, tc, f.wsIdx)
	result := se.runSteps()
	require.Equal(t, StepPassed, result.Status, "templated send must resolve and the exchange must complete; evidence=%v", result.Evidence)

	required := []project.VocabEdge{
		{FromRole: "web", ToRole: "bridge", Type: "session:start", Trigger: "message_handled"},
		{FromRole: "bridge", ToRole: "web", Type: "session:created", Trigger: "message_handled"},
	}
	exercised := exercisedEdgesMirror(tc, result.Evidence, required)
	require.Contains(t, exercised, "web|bridge|session:start", "deviceId resolved ⇒ DO relayed session:start to bridge")
	require.Contains(t, exercised, "bridge|web|session:created", "bridge replied and web received it")
}
```

> NOTE for the implementer: confirm `f.wsIdx.ActorPathParams` is a writable map field (it is — `WSProtocolIndex.ActorPathParams map[string]map[string]string`). If `newStepExecutionWithIdx` builds the executor from `f.wsIdx` such that `ActorPathParams` is read at send time, seeding it before constructing `se` is sufficient (it is — the executor holds the same `*WSProtocolIndex` pointer).

- [ ] **Step 2: Run the test against a live server**

Bring up open-agents (`make integration-openagents` reuses/starts :8989), then:
Run: `go test -tags=integration -run TestPathCoverage_LiveSendBodyTemplating -timeout=5m -v ./internal/head/agent/`
Expected: PASS — the web send resolves `{{bridge.deviceId}}`, the DO relays `session:start` to the bridge, the bridge replies `session:created`, and web receives it.

- [ ] **Step 3: Commit**

```bash
git add internal/head/agent/pathcoverage_live_integration_test.go
git -c user.name=binoctal -c user.email=binoctal@gmail.com \
  commit -m "test(agent): live send-body placeholder resolution completes two-role exchange"
```

---

### Task 7: Autonomous live proof + full verification + docs

**Files:**
- Modify: `cerberus-docs/technical/validation/2026-08-07-openagents-integration-test-report.md` (append the autonomous two-direction re-verification)

- [ ] **Step 1: Build at HEAD**

Run: `make build`
Expected: `build/cerberus` produced.

- [ ] **Step 2: Bring up open-agents and run autonomously**

Run: `make integration-openagents TEST=TestVocabularyDriven` (sanity; reuses/starts :8989) — green.
Then run a real autonomous run:
Run: `./build/cerberus run --config dogfood/ws-realtime/.cerberus/project.yaml --dir dogfood/ws-realtime --goal "Relay a session between web and bridge over the realtime WS service" 2>&1 | tee /tmp/auto.log | grep -E "coverage assessment|reqresp|session:start|Session Summary|unresolved placeholder"`

- [ ] **Step 3: Verify the autonomous result (honest)**

Inspect `/tmp/auto.log`. Required for success:
- The reqresp case (`ws-realtime-bridge-reqresp-session-start-session-created`) executes; its web `session:start` send is no longer dropped (the DO relays it because the payload now carries the bridge deviceId).
- The `coverage assessment` log line reports `gaps` **strictly less than 63** (at least the `web→bridge|session:start` edge newly exercised), ideally with the reqresp case verdict improved from the prior `correctness:0.05`.
- No `unresolved placeholder` error (the bridge actor was provisioned, so `{{bridge.deviceId}}` resolves).

If `coverage_pct` is unchanged or an `unresolved placeholder` appears, investigate WHY before claiming success (e.g. the bridge actor's path_params capture in `project.yaml` did not populate `ActorPathParams`, or the DO did not relay). Record the honest outcome regardless. Do NOT assert improvement unless the log shows it.

- [ ] **Step 4: Record the observed result**

Append a "WS send-body templating — autonomous re-verification (2026-08-10)" section to `cerberus-docs/technical/validation/2026-08-07-openagents-integration-test-report.md` with: the observed `coverage assessment` line (verbatim), the reqresp case verdict, whether both `session:start` and `session:created` edges were exercised, and any caveats. Contrast with the prior `1/63` baseline.

- [ ] **Step 5: Full verification**

Run: `make fmt && go vet ./... && go vet -tags=integration ./... && make lint && make test`
Expected: clean + PASS.

- [ ] **Step 6: Commit**

```bash
git add cerberus-docs/technical/validation/2026-08-07-openagents-integration-test-report.md
git -c user.name=binoctal -c user.email=binoctal@gmail.com \
  commit -m "docs(validation): WS send-body templating closes two-direction reqresp coverage"
```

---

## Self-Review Notes

- **Spec coverage:** D1 (resolver in executor, `wsEntry.credentialRef`) → Task 2. D2 (placeholder syntax) → Task 2 (regex + resolver + test). D3 (`request_payload` sibling declaration) → Tasks 1, 4, 5. D4 (`wsSendBody(typ, payload)`) → Task 3 (signature + all 4 call sites). D5 (validation) → Task 1. Success criteria (both directions, gaps < 63, zero regression) → Tasks 6, 7. All spec sections mapped.
- **Placeholder scan:** every code step shows real content. The "NOTE for the implementer" blocks name exactly what to confirm against current code (validator entry name, `store`'s single caller, `wsSendBody` call-site count) — confirm-the-real-shape instructions, not TBDs.
- **Type consistency:** `wsSendBody(typ string, payload map[string]string)` defined Task 3, used Task 4. `ProtocolRole.RequestPayload map[string]map[string]string` defined Task 1, used Tasks 4, 5. `resolveMessageBody(entry *wsEntry, msg string) (string, error)` defined + called Task 2. `wsEntry.credentialRef` + `store(..., credentialRef)` defined + wired Task 2. `validateProtocolRequestPayload(roleName, role)` defined + wired Task 1.
- **Zero-regression:** no `request_payload` ⇒ `wsSendBody` bare envelope (Task 3/4 tests); no `{{` in message ⇒ `resolveMessageBody` verbatim (Task 2 test); nil payload call sites byte-identical (Task 3).
- **Honest-verification risk:** Task 7 does NOT assume the gap count drops; it greps the real log and records the actual number, flagging `unresolved placeholder` or unchanged coverage as failure-to-investigate. The reliable two-direction proof is the Task 6 integration test.
