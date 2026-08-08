# Autonomous WS Message-Edge Coverage Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make an autonomous `cerberus run` against the `ws-realtime` dogfood measure objective >0 message-edge path coverage by exercising real two-role request-response exchanges.

**Architecture:** (A) a deterministic two-role request-response case generator in scout driven by a new `ProtocolRole.Responses` declaration; (B) two small cerberus runtime features — a provisioning-only authflow (static token + captured path params) and role-param templating from captured path params; (C) a ws-realtime dogfood rework declaring the bridge role, a `/ws/{userId}` path template, provisioning hooks, and representative response mappings.

**Tech Stack:** Go 1.25, table-driven tests, existing scout/agent/project packages.

**Spec:** `cerberus-docs/superpowers/specs/2026-08-08-autonomous-ws-message-coverage-design.md`

## Global Constraints

- Module: `github.com/binoctal/cerberus`, Go 1.25, no CGo.
- Commit author: `binoctal <binoctal@gmail.com>`, NO `Co-Authored-By`.
- Code comments and commit messages in English; match existing comment density.
- All docs go in `cerberus-docs/`, never `docs/`.
- Zero regression: protocols without `Responses`/provisioning produce byte-identical case generation and auth resolution.
- Verify honestly: live claims require a real run; never assert >0 from unit tests alone.

---

## File Structure

- `internal/project/protocol_schema.go` — add `Responses` to `ProtocolRole`.
- `internal/project/validate_protocol.go` — validate `Responses` (non-empty type tokens).
- `internal/project/protocol_schema_test.go` (or `validate_protocol_test.go`) — schema/validation test.
- `internal/head/scout/ws_cases.go` — new `wsRequestResponseCases` generator + wiring into `wsCasesForService`.
- `internal/head/scout/ws_cases_test.go` — generator unit test.
- `internal/head/agent/authflow.go` — B1: optional `TokenFrom` (static token + captured PathParams).
- `internal/head/agent/authflow_test.go` — B1 unit test.
- `internal/head/agent/websocket.go` — B2: role-param `{name}` templating from path params.
- `internal/head/agent/websocket_test.go` (or appropriate) — B2 unit test.
- `dogfood/ws-realtime/.cerberus/protocols/open-agents.yaml` — add `bridge` role + `responses`.
- `dogfood/ws-realtime/.cerberus/project.yaml` — `/ws/{userId}` URL + web/bridge actors with provisioning.
- `internal/head/agent/pathcoverage_live_integration_test.go` (extend) or a new `//go:build integration` test — live two-role exchange proof.

---

### Task 1: `ProtocolRole.Responses` schema field + validation

**Files:**
- Modify: `internal/project/protocol_schema.go`
- Modify: `internal/project/validate_protocol.go`
- Test: `internal/project/validate_protocol_test.go`

**Interfaces:**
- Produces: `ProtocolRole.Responses map[string]string` (`yaml:"responses,omitempty"`, JSON `responses,omitempty`) — `received_type → reply_type`. Empty/absent ⇒ no responses (backward-compatible).

- [ ] **Step 1: Write the failing test**

Append to `internal/project/validate_protocol_test.go` (match the file's existing test style; if it builds a `Protocol` via a helper, reuse it, else construct inline):

```go
func TestValidateProtocolRole_Responses(t *testing.T) {
	mk := func(responses map[string]string) *Protocol {
		return &Protocol{
			TypePath: "type",
			Auth:     &ProtocolAuth{Strategy: "query", Param: "token"},
			Roles: map[string]*ProtocolRole{
				"web":    {Params: map[string]string{"type": "web"}},
				"bridge": {Params: map[string]string{"type": "bridge"}, Responses: responses},
			},
		}
	}

	if err := validateProtocol(mk(map[string]string{"session:start": "session:created"})); err != nil {
		t.Fatalf("valid responses must pass: %v", err)
	}
	if err := validateProtocol(mk(map[string]string{"": "session:created"})); err == nil {
		t.Fatalf("empty received_type key must fail")
	}
	if err := validateProtocol(mk(map[string]string{"session:start": ""})); err == nil {
		t.Fatalf("empty reply_type value must fail")
	}
}
```

> NOTE for the implementer: confirm the package's top-level protocol validator name (read `validate_protocol.go` — it is likely `validateProtocol(*Protocol) error` or reached via `Validate(*Config)`). The test must call the SAME validator the config loader uses so the check actually gates loading. Adjust the call site to match; the assertions (valid passes; empty key/value fail) are the requirement.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/project/ -run TestValidateProtocolRole_Responses -v`
Expected: FAIL — `Responses` field undefined (compile error) or validation absent.

- [ ] **Step 3: Add the field**

In `internal/project/protocol_schema.go`, add to `ProtocolRole` (after `Handshake`):

```go
	// Responses maps a received message type to the reply type this role's test
	// driver sends in response (received_type → reply_type). Drives the
	// deterministic two-role request-response case generator. Empty ⇒ this role
	// is never driven as a responder (backward-compatible).
	Responses map[string]string `yaml:"responses,omitempty" json:"responses,omitempty"`
```

- [ ] **Step 4: Add validation**

In `internal/project/validate_protocol.go`, add a validator and call it from the role-validation path (locate where roles are validated — e.g. a `validateProtocolRole` or inline in `validateProtocol`; call this for each role):

```go
// validateProtocolResponses checks a role's responses map: both received_type
// (key) and reply_type (value) must be non-empty type tokens. A reply_type with
// no matching declared edge is NOT an error (the request edge is still exercised).
func validateProtocolResponses(roleName string, role *ProtocolRole) error {
	for recv, reply := range role.Responses {
		if recv == "" {
			return fmt.Errorf("roles[%q].responses: received_type key is empty", roleName)
		}
		if reply == "" {
			return fmt.Errorf("roles[%q].responses[%q]: reply_type value is empty", roleName, recv)
		}
	}
	return nil
}
```

Wire it into the existing per-role validation loop (the same loop that calls `paramCollision`). If roles are validated inline in `validateProtocol`, add `if err := validateProtocolResponses(name, role); err != nil { return err }` inside the `for name, role := range p.Roles` loop.

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/project/ -run TestValidateProtocolRole_Responses -v`
Expected: PASS.

- [ ] **Step 6: Run the package (no regression)**

Run: `go test ./internal/project/`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/project/protocol_schema.go internal/project/validate_protocol.go internal/project/validate_protocol_test.go
git -c user.name=binoctal -c user.email=binoctal@gmail.com \
  commit -m "feat(project): ProtocolRole.Responses declaration + validation"
```

---

### Task 2: `wsRequestResponseCases` generator (scout)

**Files:**
- Modify: `internal/head/scout/ws_cases.go`
- Test: `internal/head/scout/ws_cases_test.go`

**Interfaces:**
- Consumes: `project.Service` (with `.Protocol.Roles[*].Responses`, `.Vocabulary.Edges`, `.URL`, `.Name`); existing helpers `wsCaseID`, `sanitizeTypeID`, `wsSendBody`.
- Produces: `wsRequestResponseCases(svc project.Service) ([]agent.TestCase, map[string]bool)` — the cases plus the set of role names they connect (for dedup by `wsCasesForService`). Each case's `Steps` are the 6-step two-role exchange; `Target = svc.URL`.

- [ ] **Step 1: Write the failing test**

Append to `internal/head/scout/ws_cases_test.go`:

```go
func TestWsRequestResponseCases(t *testing.T) {
	svc := project.Service{
		Name: "realtime",
		URL:  "http://localhost:8989/ws/{userId}",
		Protocol: &project.Protocol{
			TypePath: "type",
			Roles: map[string]*project.ProtocolRole{
				"web":    {Params: map[string]string{"type": "web"}},
				"bridge": {Params: map[string]string{"type": "bridge"},
					Responses: map[string]string{"session:start": "session:created"}},
			},
		},
		Vocabulary: &project.Vocabulary{Edges: []project.VocabEdge{
			{FromRole: "web", ToRole: "bridge", Type: "session:start", Trigger: "message_handled"},
			{FromRole: "bridge", ToRole: "web", Type: "session:created", Trigger: "message_handled"},
		}},
	}
	cases, connected := wsRequestResponseCases(svc)
	require.Len(t, cases, 1, "one response pair ⇒ one case")
	c := cases[0]
	require.Len(t, c.Steps, 6)
	// requester(web) connect, bridge connect, web send session:start,
	// bridge receive session:start, bridge send session:created, web receive session:created.
	assert.Equal(t, "ws_connect", c.Steps[0].Action)
	assert.Equal(t, "web", c.Steps[0].Role)
	assert.Equal(t, "ws_connect", c.Steps[1].Action)
	assert.Equal(t, "bridge", c.Steps[1].Role)
	assert.Equal(t, "ws_send", c.Steps[2].Action)
	assert.Contains(t, c.Steps[2].Message, "session:start")
	assert.Equal(t, "ws_receive", c.Steps[3].Action)
	assert.Equal(t, "bridge", c.Steps[3].ConnectionID)
	assert.Equal(t, "session:start", c.Steps[3].Type)
	assert.Equal(t, "ws_send", c.Steps[4].Action)
	assert.Contains(t, c.Steps[4].Message, "session:created")
	assert.Equal(t, "ws_receive", c.Steps[5].Action)
	assert.Equal(t, "web", c.Steps[5].ConnectionID)
	assert.Equal(t, "session:created", c.Steps[5].Type)
	assert.True(t, connected["web"] && connected["bridge"], "both roles connected")
}

func TestWsRequestResponseCases_NoneWhenNoResponses(t *testing.T) {
	svc := project.Service{Name: "s", URL: "u", Protocol: &project.Protocol{
		Roles: map[string]*project.ProtocolRole{"web": {}, "bridge": {}},
	}}
	cases, _ := wsRequestResponseCases(svc)
	assert.Empty(t, cases, "no Responses declared ⇒ no request-response cases")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/head/scout/ -run TestWsRequestResponseCases -v`
Expected: FAIL — `wsRequestResponseCases` undefined.

- [ ] **Step 3: Implement the generator**

In `internal/head/scout/ws_cases.go`, add (the file already imports `fmt`, `slices`, `maps`, `project`, `agent`):

```go
// wsRequestResponseCases emits deterministic two-role request-response cases for
// roles that declare a Responses map (received_type → reply_type). For each
// pair (T → T') on role R, the requester is the FromRole of a declared vocab
// edge (From→R, T); the case drives: requester connect, R connect, requester
// send T, R receive T, R send T', requester receive T'. Step "R receive T"
// exercises edge (From→R, T) and step "requester receive T'" exercises
// (R→From, T') under receive-driven pathCoverage. Pure; no LLM. Returns the
// cases and the roles they connect (for dedup). No cases when fewer than 2
// roles, no Vocabulary, no Responses, or no declared request edge.
func wsRequestResponseCases(svc project.Service) ([]agent.TestCase, map[string]bool) {
	var cases []agent.TestCase
	connected := map[string]bool{}
	if svc.Protocol == nil || len(svc.Protocol.Roles) < 2 || svc.Vocabulary == nil {
		return cases, connected
	}
	// (ToRole, Type) → first declared FromRole (the requester for that request).
	requesterOf := map[string]string{}
	for _, e := range svc.Vocabulary.Edges {
		k := e.ToRole + "|" + e.Type
		if _, ok := requesterOf[k]; !ok {
			requesterOf[k] = e.FromRole
		}
	}
	for _, rName := range slices.Sorted(maps.Keys(svc.Protocol.Roles)) {
		role := svc.Protocol.Roles[rName]
		if role == nil || len(role.Responses) == 0 {
			continue
		}
		for _, recvType := range slices.Sorted(maps.Keys(role.Responses)) {
			sendType := role.Responses[recvType]
			requester := requesterOf[rName+"|"+recvType]
			if requester == "" || requester == rName {
				continue // no declared request edge from a different role
			}
			cases = append(cases, agent.TestCase{
				ID:      wsCaseID(svc.Name, rName, "reqresp-"+sanitizeTypeID(recvType)+"-"+sanitizeTypeID(sendType)),
				Name:    fmt.Sprintf("%s %s replies %s to %s", svc.Name, rName, sendType, recvType),
				Service: svc.Name, Target: svc.URL, Action: "ws_flow",
				Expectation: fmt.Sprintf("%s role %s receives %s and replies %s", svc.Name, rName, recvType, sendType),
				Priority:    0.8,
				Steps: []agent.TestStep{
					{Action: "ws_connect", ConnectionID: requester, Role: requester},
					{Action: "ws_connect", ConnectionID: rName, Role: rName},
					{Action: "ws_send", ConnectionID: requester, Message: wsSendBody(recvType)},
					{Action: "ws_receive", ConnectionID: rName, Type: recvType, Timeout: 3},
					{Action: "ws_send", ConnectionID: rName, Message: wsSendBody(sendType)},
					{Action: "ws_receive", ConnectionID: requester, Type: sendType, Timeout: 3},
				},
			})
			connected[requester] = true
			connected[rName] = true
		}
	}
	return cases, connected
}
```

> NOTE for the implementer: confirm `wsSendBody`, `wsCaseID`, `sanitizeTypeID` signatures in `ws_cases.go` (they are used by `wsStepsCase`/`wsRelayCases` — reuse verbatim). If `wsSendBody` takes only a type string, the call above is correct.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/head/scout/ -run TestWsRequestResponseCases -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/head/scout/ws_cases.go internal/head/scout/ws_cases_test.go
git -c user.name=binoctal -c user.email=binoctal@gmail.com \
  commit -m "feat(scout): wsRequestResponseCases two-role exchange generator"
```

---

### Task 3: Wire `wsRequestResponseCases` into `wsCasesForService`

**Files:**
- Modify: `internal/head/scout/ws_cases.go` (`wsCasesForService`)

**Interfaces:**
- Consumes: `wsRequestResponseCases` (Task 2).
- Produces: request-response cases emitted by `WSCasesCovered`, with their connected roles participating in the existing per-role dedup.

- [ ] **Step 1: Write the failing test**

Append to `internal/head/scout/ws_cases_test.go` a test that drives `WSCasesCovered` with a config whose protocol declares a `Responses` pair and asserts the emitted cases include the 6-step request-response case (and that the requester/bridge roles are deduped so no redundant single-conn connect is emitted for them):

```go
func TestWSCasesCovered_EmitsRequestResponse(t *testing.T) {
	cfg := &project.Config{Services: []project.Service{{
		Name: "realtime", URL: "http://localhost:8989/ws/{userId}",
		Protocol: &project.Protocol{TypePath: "type", Roles: map[string]*project.ProtocolRole{
			"web":    {Params: map[string]string{"type": "web"}},
			"bridge": {Params: map[string]string{"type": "bridge"}, Responses: map[string]string{"session:start": "session:created"}},
		}},
		Vocabulary: &project.Vocabulary{Edges: []project.VocabEdge{
			{FromRole: "web", ToRole: "bridge", Type: "session:start", Trigger: "message_handled"},
			{FromRole: "bridge", ToRole: "web", Type: "session:created", Trigger: "message_handled"},
		}},
	}}}
	cases := WSCasesCovered(cfg, "goal", map[string]map[string]bool{}, map[string]map[string]string{})
	found := false
	for _, c := range cases {
		if len(c.Steps) == 6 && c.Steps[0].Action == "ws_connect" && c.Steps[0].Role == "web" &&
			c.Steps[3].Action == "ws_receive" && c.Steps[3].Type == "session:start" {
			found = true
		}
	}
	assert.True(t, found, "WSCasesCovered must emit the two-role request-response case")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/head/scout/ -run TestWSCasesCovered_EmitsRequestResponse -v`
Expected: FAIL — the request-response case is not emitted (generator not wired in).

- [ ] **Step 3: Wire it in**

In `wsCasesForService` (around line 61), after `relayCoexistence` builds `cases`/`connected`, emit the request-response cases and merge their connected roles into the dedup set. Concretely, replace the opening of `wsCasesForService` so it reads (add the two lines marked `// NEW`):

```go
func wsCasesForService(svc project.Service, goal string, svcCovered map[string]bool, svcCovering map[string]string) []agent.TestCase {
	relayCases, relayCovered, _ := wsRelayCases(svc)
	relayEmitted, relaySignals, relayConnected := relayCoexistence(relayCases, svcCovered, svcCovering, relayCovered)
	cases := relayEmitted
	// NEW: two-role request-response cases (declared via role.Responses).
	rrCases, rrConnected := wsRequestResponseCases(svc)
	cases = append(cases, rrCases...)
	// Iterate roles in sorted name order so the returned slice is deterministic
	// across runs regardless of map iteration order.
	for _, roleName := range slices.Sorted(maps.Keys(svc.Protocol.Roles)) {
		if svcCovered[roleName] {
			continue
		}
		// NEW: skip roles already connected by a request-response case.
		if rrConnected[roleName] {
			continue
		}
		role := svc.Protocol.Roles[roleName]
		if ex, ok := wsExchangeFromGoal(goal); ok {
			cases = append(cases, wsStepsCase(svc, roleName, role, ex))
			continue
		}
		if relayConnected[roleName] {
			continue
		}
		cases = append(cases, wsFlowConnectCase(svc, roleName, role, goal, relaySignals))
	}
	return cases
}
```

> NOTE for the implementer: read the CURRENT `wsCasesForService` body before editing — match its exact current form, inserting only the `rrCases`/`rrConnected` emission and the `rrConnected[roleName]` skip. Do not alter the relay/exchange/connect logic otherwise.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/head/scout/ -run 'TestWSCasesCovered|TestWsRequestResponseCases' -v`
Expected: PASS (new test + Task 2 tests).

- [ ] **Step 5: Run the scout package (no regression)**

Run: `go test ./internal/head/scout/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/head/scout/ws_cases.go internal/head/scout/ws_cases_test.go
git -c user.name=binoctal -c user.email=binoctal@gmail.com \
  commit -m "feat(scout): emit two-role request-response cases via WSCasesCovered"
```

---

### Task 4: B1 — provisioning-only authflow (optional `TokenFrom`)

**Files:**
- Modify: `internal/head/agent/authflow.go`
- Test: `internal/head/agent/authflow_test.go`

**Interfaces:**
- Consumes: `actor.Credentials.Token` (static), `AuthFlow.Login`, `AuthFlow.PathParams`, `AuthFlow.TokenFrom`.
- Produces: when `AuthFlow.TokenFrom == ""`, `ResolveAuthHeader` uses `actor.Credentials.Token` as `AuthResult.RawToken` but still runs the login and captures `PathParams`. When `TokenFrom != ""`, behavior is unchanged.

- [ ] **Step 1: Write the failing test**

Append to `internal/head/agent/authflow_test.go` (match the file's existing `TestResolveAuthHeader_*` style — it stubs the login HTTP via a test server or mock client; mirror it):

```go
func TestResolveAuthHeader_ProvisioningOnly_StaticTokenPlusPathParams(t *testing.T) {
	// TokenFrom empty ⇒ use static Credentials.Token, but still run login to
	// capture PathParams. Provision a fake /api/dev/setup response carrying userId.
	body := `{"config":{"userId":"user_42","deviceToken":"tok_dev"}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	actor := project.Actor{
		Name:        "web-actor",
		Credentials: project.Credentials{Token: "demo_token"},
		Auth: &project.AuthFlow{
			Login:     project.AuthLogin{Method: "POST", Path: "/api/dev/setup"},
			TokenFrom: "", // provisioning-only: static token + captured path params
			PathParams: map[string]string{
				"userId": "config.userId",
			},
		},
	}
	res, err := ResolveAuthHeader(context.Background(), srv.URL, actor)
	require.NoError(t, err)
	assert.Equal(t, "demo_token", res.RawToken, "empty TokenFrom ⇒ static Credentials.Token is used")
	require.NotNil(t, res.PathParams)
	assert.Equal(t, "user_42", res.PathParams["userId"], "login still runs to capture path params")
}
```

> NOTE for the implementer: confirm the `Actor`/`Credentials`/`AuthFlow` field names and the test's HTTP-stub pattern against the existing `authflow_test.go` tests. The requirement: empty `TokenFrom` ⇒ `RawToken == actor.Credentials.Token` AND `PathParams["userId"] == "user_42"` (login still ran). If the existing tests inject an `http.Client` via a package var, use that instead of `httptest.NewServer`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/head/agent/ -run TestResolveAuthHeader_ProvisioningOnly -v`
Expected: FAIL — with empty `TokenFrom`, `ResolveAuthHeader` returns an error (token resolution fails) today.

- [ ] **Step 3: Implement B1**

In `internal/head/agent/authflow.go`, `ResolveAuthHeader` currently extracts the token from `af.TokenFrom` and errors if missing. Change it so an empty `TokenFrom` falls back to `actor.Credentials.Token`. Locate the token-extraction line (near where `AuthResult.RawToken = token` is set, after reading the response JSON via `af.TokenFrom`). Restructure to:

```go
	// Token resolution: an explicit token_from dot-path reads the login
	// response; an EMPTY token_from makes this a provisioning-only flow — the
	// static Credentials.Token is used, but login still runs to capture
	// PathParams (e.g. a provisioned userId for URL templating).
	token := actor.Credentials.Token
	if af.TokenFrom != "" {
		t, ok := jsonDotPath(respBody, af.TokenFrom)
		if !ok {
			return nil, fmt.Errorf("auth flow: token_from %q not found in login response", af.TokenFrom)
		}
		token = t
	}
```

(Use the file's EXISTING JSON dot-path helper/name for the `af.TokenFrom` lookup — read the current code to get the exact helper, e.g. it may inline `gjson` or a local `dotPath` function. The requirement: when `TokenFrom == ""`, `token` stays `actor.Credentials.Token`; otherwise it is resolved exactly as today.) Keep the rest (PathParams capture, return `&AuthResult{RawToken: token, PathParams: pathParams, ...}`) unchanged.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/head/agent/ -run TestResolveAuthHeader -v`
Expected: PASS (new test + existing authflow tests).

- [ ] **Step 5: Run the package (no regression)**

Run: `go test ./internal/head/agent/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/head/agent/authflow.go internal/head/agent/authflow_test.go
git -c user.name=binoctal -c user.email=binoctal@gmail.com \
  commit -m "feat(agent): provisioning-only authflow (static token + captured path params)"
```

---

### Task 5: B2 — role-param `{name}` templating from captured path params

**Files:**
- Modify: `internal/head/agent/websocket.go`
- Test: `internal/head/agent/websocket_test.go` (or the file that tests `injectRoleDiscriminators`/`resolveRoleParamValue`)

**Interfaces:**
- Consumes: the resolved actor's captured `PathParams` via `e.pathParamsFor(credentialRef)`; existing `roleParamUUIDSentinel`.
- Produces: role discriminator param values may carry `{name}` placeholders resolved from the actor's path params. `{name}` resolves only when `name` is a captured path param; otherwise the literal is left intact. The uuid sentinel still works.

- [ ] **Step 1: Write the failing test**

Find the existing test for `resolveRoleParamValue` / `injectRoleDiscriminators` (grep `resolveRoleParamValue` in `*_test.go`). Add/append:

```go
func TestResolveRoleParamValue_PathParamTemplate(t *testing.T) {
	params := map[string]string{"deviceId": "device_abc"}
	// {name} resolves from path params …
	assert.Equal(t, "device_abc", resolveRoleParamValue("{deviceId}", params))
	// … the uuid sentinel still generates a fresh uuid …
	assert.NotEqual(t, roleParamUUIDSentinel, resolveRoleParamValue(roleParamUUIDSentinel, params))
	// … an unknown {name} (not a captured param) is left literal …
	assert.Equal(t, "{unknown}", resolveRoleParamValue("{unknown}", params))
	// … and a plain literal is unchanged.
	assert.Equal(t, "bridge", resolveRoleParamValue("bridge", params))
}
```

> NOTE for the implementer: the current `resolveRoleParamValue(v string) string` takes ONE argument. This task changes its signature to `resolveRoleParamValue(v string, pathParams map[string]string) string`, so update ALL existing call sites/tests of `resolveRoleParamValue` to pass the second arg (use `nil` where no path params apply). Confirm the call site inside `injectRoleDiscriminators` is updated in Step 3.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/head/agent/ -run TestResolveRoleParamValue_PathParamTemplate -v`
Expected: FAIL — signature mismatch / templating absent.

- [ ] **Step 3: Implement B2**

Change `resolveRoleParamValue` to accept path params and resolve `{name}`:

```go
// resolveRoleParamValue resolves a role discriminator param value: the uuid
// sentinel yields a fresh uuid; a "{name}" placeholder resolves from the
// actor's captured path params (provisioning); an unknown placeholder or plain
// literal is returned unchanged.
func resolveRoleParamValue(v string, pathParams map[string]string) string {
	if v == roleParamUUIDSentinel {
		return uuid.NewString()
	}
	if len(v) >= 2 && v[0] == '{' && v[len(v)-1] == '}' {
		name := v[1 : len(v)-1]
		if val, ok := pathParams[name]; ok {
			return val
		}
	}
	return v
}
```

Update `injectRoleDiscriminators` to receive and pass the path params. Its current signature is `injectRoleDiscriminators(opts *websocket.DialOptions, role *project.ProtocolRole, roleParams map[string]string, dialURL string) string`. Add a `pathParams map[string]string` parameter and pass it through:

```go
func injectRoleDiscriminators(opts *websocket.DialOptions, role *project.ProtocolRole, roleParams map[string]string, dialURL string, pathParams map[string]string) string {
	for k, v := range roleParams {
		dialURL = setQueryParam(dialURL, k, resolveRoleParamValue(v, pathParams))
	}
	// … (rest unchanged: headers, subprotocols) …
}
```

Update the single call site in `doConnect` (around line 463) to pass the resolved actor's path params:

```go
	dialURL = injectRoleDiscriminators(opts, role, roleParams, dialURL, e.pathParamsFor(credentialRef))
```

(`credentialRef` is already resolved earlier in `doConnect`, lines ~424–441; `e.pathParamsFor` already exists.)

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/head/agent/ -run 'TestResolveRoleParamValue|TestInjectRoleDiscriminators' -v`
Expected: PASS.

- [ ] **Step 5: Run the package (no regression)**

Run: `go test ./internal/head/agent/ && go vet ./internal/head/agent/`
Expected: PASS, vet clean (confirms ALL call sites of the changed signature are updated).

- [ ] **Step 6: Commit**

```bash
git add internal/head/agent/websocket.go internal/head/agent/websocket_test.go
git -c user.name=binoctal -c user.email=binoctal@gmail.com \
  commit -m "feat(agent): role-param {name} templating from captured path params"
```

---

### Task 6: ws-realtime dogfood rework (bridge role + path template + provisioning)

**Files:**
- Modify: `dogfood/ws-realtime/.cerberus/protocols/open-agents.yaml`
- Modify: `dogfood/ws-realtime/.cerberus/project.yaml`

**Interfaces:**
- Consumes: Tasks 1, 4, 5 (Responses schema, provisioning-only authflow, role-param templating).
- Produces: a dogfood config where an autonomous run connects web (`demo_token`) + bridge (provisioned `deviceToken`/`deviceId`) to the same `/ws/{userId}` DO and runs a two-role exchange.

> NOTE for the implementer: the dev server runs on :8989. `/api/dev/setup` returns `{"config":{"userId":...,"deviceId":...,"deviceToken":...}}` and is deterministic per server session (same `userId` each call), so web-actor and bridge-actor each provisioning independently receive the same `userId`. The web role authenticates with the static `demo_token` dev backdoor (the server checks `token === 'demo_token'` for web). Confirm `path_params` dot-paths against the actual `/api/dev/setup` response shape (`config.userId`, `config.deviceId`, `config.deviceToken`).

- [ ] **Step 1: Rewrite the protocol**

Replace `dogfood/ws-realtime/.cerberus/protocols/open-agents.yaml` with:

```yaml
framing: json
type_path: type
auth:
  strategy: query
  param: token
  credential_ref: web-actor
roles:
  web:
    credential_ref: web-actor
    params:
      type: web
    handshake:
      # device:online is the signal the DO pushes to web when a bridge joins.
      await_type: device:online
      timeout: 5
      optional: true
  bridge:
    credential_ref: bridge-actor
    params:
      type: bridge
      deviceId: "{deviceId}"   # templated from bridge-actor's provisioned path params (Task 5)
    responses:
      session:start: session:created   # Task 2 generator drives this exchange
```

- [ ] **Step 2: Rewrite project.yaml**

Replace `dogfood/ws-realtime/.cerberus/project.yaml` with:

```yaml
project:
  name: ws-realtime-dogfood
services:
  - name: realtime
    url: http://localhost:8989/ws/{userId}   # {userId} templated from web-actor's provisioned path param
    protocol_ref: open-agents
actors:
  - name: web-actor
    credentials:
      token: demo_token   # static dev backdoor (TokenFrom empty ⇒ provisioning-only, Task 4)
    auth:
      login:
        method: POST
        path: /api/dev/setup
        body: {}
      # token_from omitted on purpose: provisioning-only flow (static token + captured userId)
      path_params:
        userId: config.userId
  - name: bridge-actor
    credentials: {}   # token comes from the login response (deviceToken)
    auth:
      login:
        method: POST
        path: /api/dev/setup
        body: {}
      token_from: config.deviceToken
      path_params:
        userId: config.userId
        deviceId: config.deviceId
settings:
  max_duration: 8m
  confidence_threshold: 0.7
  ai_budget:
    session_total_tokens: 80000
    per_call_limit: 10000
```

- [ ] **Step 3: Validate the config loads**

Run: `go build -o /tmp/cerberus ./cmd/cerberus && /tmp/cerberus run --config dogfood/ws-realtime/.cerberus/project.yaml --dir dogfood/ws-realtime --goal "smoke" 2>&1 | head -5`
Expected: the run STARTS (session created) with NO config/validation errors. (It need not complete; this step only confirms the schema/provisioning wiring parses and the protocol validates.) Kill it once the session is created.

> If validation rejects `body: {}` (empty map) or the `auth` shape, consult existing actor/auth examples in the repo (`grep -rn "auth:" dogfood */.cerberus/*.yaml`) and adjust the literal to match — the requirement is a working POST `/api/dev/setup` with empty body.

- [ ] **Step 4: Commit**

```bash
git add dogfood/ws-realtime/.cerberus/protocols/open-agents.yaml dogfood/ws-realtime/.cerberus/project.yaml
git -c user.name=binoctal -c user.email=binoctal@gmail.com \
  commit -m "feat(dogfood): ws-realtime bridge role + provisioning + /ws/{userId} path"
```

---

### Task 7: Live integration test — two-role exchange → pathCoverage >0

**Files:**
- Modify (or extend): `internal/head/agent/pathcoverage_live_integration_test.go` (already `//go:build integration`, package `agent`)

**Interfaces:**
- Consumes: Tasks 1–3 (the exchange exists), the receive-driven `pathCoverage` (predecessor change), `setupOpenAgents` (provisions + binds web=demo_token, bridge=deviceToken).
- Produces: a `//go:build integration` test proving a REAL open-agents two-role message exchange exercises a `message_handled` edge ⇒ pathCoverage >0 over real evidence.

> Rationale: this test hand-drives a known-good two-role exchange (the way `TestSessionStartRoundTrip` does) and asserts the receive-driven attribution counts it. It is the reliable proof the measurement works on real message exchanges; the autonomous run (Task 8) depends on the deterministic generator + dogfood config end-to-end.

- [ ] **Step 1: Write the test**

Append to `internal/head/agent/pathcoverage_live_integration_test.go` (it already imports `testing`, `require`, `project`; reuse the inline receive-driven mirror already in that file, factored into a helper if helpful):

```go
// TestPathCoverage_LiveTwoRoleExchange proves a REAL open-agents two-role
// message_handled exchange (web sends session:start, bridge receives and replies
// session:created, web receives it) exercises a message_handled edge under the
// receive-driven attribution ⇒ pathCoverage > 0. The web's session:start needs
// the bridge's deviceId, so the send carries f.deviceId.
func TestPathCoverage_LiveTwoRoleExchange(t *testing.T) {
	f := setupOpenAgents(t, false)
	tc := &TestCase{
		ID:     "tc-tworoletexchange",
		Target: "ws://localhost:8989/ws/" + f.userId,
		Steps: []TestStep{
			{Action: "ws_connect", Role: "web", ConnectionID: "web"},
			{Action: "ws_connect", Role: "bridge", ConnectionID: "bridge"},
			{Action: "ws_receive", ConnectionID: "web", Type: "device:online", Timeout: 3},
			{Action: "ws_send", ConnectionID: "web", Message: fmt.Sprintf(`{"type":"session:start","payload":{"deviceId":%q}}`, f.deviceId)},
			{Action: "ws_receive", ConnectionID: "bridge", Type: "session:start", Timeout: 3},
			{Action: "ws_send", ConnectionID: "bridge", Message: `{"type":"session:created"}`},
			{Action: "ws_receive", ConnectionID: "web", Type: "session:created", Timeout: 3},
		},
	}
	se := newStepExecutionWithIdx(t, tc, f.wsIdx)
	result := se.runSteps()
	require.Equal(t, StepPassed, result.Status, "two-role exchange must complete; evidence=%v", result.Evidence)

	// Representative required surface including both message_handled edges.
	required := []project.VocabEdge{
		{FromRole: "web", ToRole: "bridge", Type: "session:start", Trigger: "message_handled"},
		{FromRole: "bridge", ToRole: "web", Type: "session:created", Trigger: "message_handled"},
		{FromRole: "bridge", ToRole: "web", Type: "device:online", Trigger: "message_handled"},
	}
	// Reuse the receive-driven mirror already defined in this file (see
	// TestPathCoverage_LiveOpenAgentsRelay). If it is inline there, extract a
	// shared helper exercisedEdgesMirror(tc, evidence, required) and call it.
	exercised := exercisedEdgesMirror(tc, result.Evidence, required)
	pct := float64(len(exercised)) / float64(len(required))
	t.Logf("two-role exchange: exercised=%v path_coverage=%.3f", exercised, pct)
	// session:start (web→bridge) is exercised (bridge received it); session:created
	// (bridge→web) is exercised (web received it). device:online may or may not be
	// in the real required surface; assert the two message_handled exchange edges.
	require.Contains(t, exercised, "web|bridge|session:start")
	require.Contains(t, exercised, "bridge|web|session:created")
	require.Greater(t, pct, 0.0)
}
```

> NOTE for the implementer: the existing `TestPathCoverage_LiveOpenAgentsRelay` in this file has an INLINE receive-driven attribution block. Extract it into a helper `exercisedEdgesMirror(tc *TestCase, evidence []agent.Evidence, required []project.VocabEdge) map[string]bool` (build connRole from `tc.Steps`, attribute matched receives to required edges), and use it in BOTH tests. This removes duplication and is the clean factoring the change deserves. Add `"fmt"` to the imports if not present.

- [ ] **Step 2: Run the test against a live server**

Bring up open-agents (`make integration-openagents` reuses/starts :8989), then:
Run: `go test -tags=integration -run TestPathCoverage_LiveTwoRoleExchange -timeout=5m -v ./internal/head/agent/`
Expected: PASS — `exercised` contains both exchange edges, `path_coverage > 0`.

- [ ] **Step 3: Commit**

```bash
git add internal/head/agent/pathcoverage_live_integration_test.go
git -c user.name=binoctal -c user.email=binoctal@gmail.com \
  commit -m "test(agent): live two-role exchange exercises message_handled edges (pathCoverage>0)"
```

---

### Task 8: Autonomous live proof + full verification + docs

**Files:**
- Modify: `cerberus-docs/technical/validation/2026-08-07-openagents-integration-test-report.md` (append the autonomous >0 result)

- [ ] **Step 1: Build at HEAD**

Run: `make build`
Expected: `build/cerberus` produced.

- [ ] **Step 2: Bring up open-agents and run autonomously**

Run: `make integration-openagents TEST=TestVocabularyDriven` (sanity; reuses/starts :8989) — green.
Then run a real autonomous run:
Run: `./build/cerberus run --config dogfood/ws-realtime/.cerberus/project.yaml --dir dogfood/ws-realtime --goal "Relay a session between web and bridge over the realtime WS service" 2>&1 | tee /tmp/auto.log | grep -E "coverage assessment|coverage not applicable|reqresp|session:start|Session Summary"`

- [ ] **Step 3: Verify the autonomous result (honest)**

Inspect `/tmp/auto.log`. Required for success:
- The `coverage assessment` log line reports `coverage_pct > 0` (at least one message_handled edge exercised), with `reached:false` (PathThreshold=1.0) and a gap list; OR `coverage not applicable` is NOT present (the session measured path coverage).
- A `reqresp-...` case ran (from `wsRequestResponseCases`).

If `coverage_pct` is 0, investigate WHY no message_handled edge was exercised (e.g. the deterministic exchange case failed because `session:start` needs a payload the minimal `wsSendBody` does not provide). Record the honest outcome in the validation report regardless: success ⇒ objective >0; partial ⇒ document which exchanges work and which need payload templates (follow-up). Do NOT assert >0 unless the log shows it.

- [ ] **Step 4: Record the observed result**

Append an "Autonomous WS message coverage — re-verification" section to `cerberus-docs/technical/validation/2026-08-07-openagents-integration-test-report.md` with the observed coverage log line, which edges were exercised, and any payload/provisioning caveats discovered.

- [ ] **Step 5: Full verification**

Run: `make fmt && go vet ./... && go vet -tags=integration ./... && make lint && make test`
Expected: clean + PASS.

- [ ] **Step 6: Commit**

```bash
git add cerberus-docs/technical/validation/2026-08-07-openagents-integration-test-report.md
git -c user.name=binoctal -c user.email=binoctal@gmail.com \
  commit -m "docs(validation): autonomous WS message-edge coverage result"
```

---

## Self-Review Notes

- **Spec coverage:** Decision 1 (Responses on protocol role) → Tasks 1, 2, 3, 6. Decision 2 (provisioning via dev determinism + B1/B2) → Tasks 4, 5, 6. Decision 3 (wsRequestResponseCases) → Tasks 2, 3. Testing/Success criteria → Tasks 7, 8. All spec sections mapped to tasks.
- **Placeholder scan:** every code step shows real content. The "NOTE for the implementer" blocks name exactly what to confirm against current code (validator name, helper signatures, JSON dot-path helper, test HTTP-stub pattern) — these are confirm-the-real-shape instructions, not TBDs.
- **Type consistency:** `wsRequestResponseCases(svc) ([]agent.TestCase, map[string]bool)` defined in Task 2, consumed identically in Task 3. `resolveRoleParamValue(v, pathParams)` + `injectRoleDiscriminators(..., pathParams)` signature change defined in Task 5, call site updated in Task 5. `ProtocolRole.Responses map[string]string` defined Task 1, used Task 2/6.
- **Zero-regression:** no `Responses` ⇒ `wsRequestResponseCases` returns empty (Task 2 test); no provisioning ⇒ authflow/role-param unchanged paths (Tasks 4/5 preserve existing behavior).
- **Honest-verification risk:** Task 8 does NOT assume autonomous >0; it investigates and records the real outcome, flagging payload-template needs if the minimal exchange fails. The reliable proof is the Task 7 integration test.
