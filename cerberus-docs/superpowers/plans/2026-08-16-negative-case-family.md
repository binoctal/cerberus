# Negative / Exception Case Family Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Declarative `violations:` in protocol files + a deterministic scout generator + two executor primitives, so the open-agents rejection semantics (oversize, rate limit, missing-route, auth boundaries) are tested.

**Architecture:** SUT facts live in the hand-authored protocol yaml (`violations:` section, validated at config load). A deterministic generator (`violationCases`) expands each declaration into a ws_flow/http case. The executor gains only what declarations cannot express today: a close-code assertion action (`ws_expect_close`) and an oversize payload intrinsic (`{{pad:N}}`).

**Tech Stack:** Go 1.25, `coder/websocket` (already a dependency), yaml.v3, existing scout/session test idioms (testify).

**Spec:** `cerberus-docs/superpowers/specs/2026-08-16-negative-case-family-design.md`

## Global Constraints

- Commit author `binoctal <binoctal@gmail.com>`, NO Co-Authored-By. English comments/commit messages.
- cerberus stays SUT-generic: no open-agents constant may appear in `internal/` source (only in `dogfood/*/cerberus` yaml).
- No claims-ledger binding for violation cases (spec Non-goals).
- Branch `feat/negative-case-family` from main; dogfood work on the branch, NOT a worktree.
- Rate-limit case shape decision (spec): the burst is generator-expanded send steps; pacing between 1s windows uses an `ExpectAbsent` receive as the pacer — NO executor loop/sleep construct.
- Test command: `go test ./internal/... -run '<TestName>'`; full gate `make test`.

---

### Task 1: Violation schema + validator

**Files:**
- Create: `internal/project/violations_schema.go`
- Modify: `internal/project/protocol_schema.go` (add `Violations` field to `Protocol`)
- Modify: `internal/project/validate_protocol.go` (wire `validateViolations` into `ValidateProtocol`)
- Test: `internal/project/violations_schema_test.go`

**Interfaces:**
- Produces: `type Violation struct {ID, Family, Role string; Trigger ViolationTrigger; Expect ViolationExpect}` with yaml tags `id,family,role,trigger,expect`; `ViolationTrigger{Bytes int yaml:"bytes"; Messages int yaml:"messages"; Windows int yaml:"windows"; Type string yaml:"type"; OmitFields []string yaml:"omit_fields"; Method string yaml:"method"; Path string yaml:"path"; DropHeaders []string yaml:"drop_headers"}`; `ViolationExpect{FrameType string yaml:"frame_type"; Code string yaml:"code"; CloseCode int yaml:"close_code"; HTTPStatus int yaml:"http_status"}`. Family constants: `ViolationFamilyOversize = "oversize"`, `ViolationFamilyRateLimit = "rate_limit"`, `ViolationFamilyRouteMissing = "route_missing"`, `ViolationFamilyHTTPAuth = "http_auth"`.
- `Protocol.Violations []Violation yaml:"violations,omitempty"` (add next to `Batches` in `internal/project/protocol_schema.go`).

- [ ] **Step 1: Write the failing test** — `internal/project/violations_schema_test.go`:

```go
package project

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func validViolationsProtocol() *Protocol {
	return &Protocol{
		Roles: map[string]*ProtocolRole{
			"web":    {CredentialRef: "web"},
			"bridge": {CredentialRef: "b1"},
		},
		Violations: []Violation{
			{ID: "oversize-message", Family: ViolationFamilyOversize, Role: "web",
				Trigger: ViolationTrigger{Bytes: 1048577, Type: "chat:message"},
				Expect:  ViolationExpect{CloseCode: 1009}},
			{ID: "bridge-rate-limit", Family: ViolationFamilyRateLimit, Role: "bridge",
				Trigger: ViolationTrigger{Messages: 220, Windows: 6, Type: "chat:message"},
				Expect:  ViolationExpect{FrameType: "error", Code: "RATE_LIMIT_EXCEEDED", CloseCode: 1008}},
			{ID: "missing-device-id", Family: ViolationFamilyRouteMissing, Role: "web",
				Trigger: ViolationTrigger{Type: "session:start", OmitFields: []string{"deviceId"}},
				Expect:  ViolationExpect{FrameType: "error", Code: "MISSING_DEVICE_ID"}},
			{ID: "csrf-no-origin", Family: ViolationFamilyHTTPAuth, Role: "web",
				Trigger: ViolationTrigger{Method: "POST", Path: "/api/dev/setup", DropHeaders: []string{"Origin"}},
				Expect:  ViolationExpect{HTTPStatus: 403}},
		},
	}
}

func TestValidateViolations(t *testing.T) {
	t.Run("valid matrix passes", func(t *testing.T) {
		assert.NoError(t, ValidateProtocol(validViolationsProtocol(), []Actor{{Name: "web"}, {Name: "b1"}}))
	})
	t.Run("unknown family rejected", func(t *testing.T) {
		p := validViolationsProtocol()
		p.Violations[0].Family = "nonsense"
		assert.ErrorContains(t, ValidateProtocol(p, nil), "violations[0].family")
	})
	t.Run("role must be declared", func(t *testing.T) {
		p := validViolationsProtocol()
		p.Violations[0].Role = "ghost"
		assert.ErrorContains(t, ValidateProtocol(p, nil), `violations[0].role "ghost" does not match`)
	})
	t.Run("oversize needs bytes and close_code", func(t *testing.T) {
		p := validViolationsProtocol()
		p.Violations[0].Trigger.Bytes = 0
		assert.ErrorContains(t, ValidateProtocol(p, nil), "violations[0].trigger.bytes")
	})
	t.Run("rate_limit needs messages, windows, frame and close", func(t *testing.T) {
		p := validViolationsProtocol()
		p.Violations[1].Trigger.Windows = 0
		assert.ErrorContains(t, ValidateProtocol(p, nil), "violations[1].trigger.windows")
		p = validViolationsProtocol()
		p.Violations[1].Expect.Code = ""
		assert.ErrorContains(t, ValidateProtocol(p, nil), "violations[1].expect.code")
	})
	t.Run("route_missing needs type, omit_fields, frame+code", func(t *testing.T) {
		p := validViolationsProtocol()
		p.Violations[2].Trigger.OmitFields = nil
		assert.ErrorContains(t, ValidateProtocol(p, nil), "violations[2].trigger.omit_fields")
	})
	t.Run("http_auth needs method, path, status", func(t *testing.T) {
		p := validViolationsProtocol()
		p.Violations[3].Trigger.Path = ""
		assert.ErrorContains(t, ValidateProtocol(p, nil), "violations[3].trigger.path")
	})
	t.Run("empty id rejected", func(t *testing.T) {
		p := validViolationsProtocol()
		p.Violations[0].ID = ""
		assert.ErrorContains(t, ValidateProtocol(p, nil), "violations[0].id")
	})
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/project/ -run TestValidateViolations`
Expected: FAIL — `Violation`/`ViolationTrigger` undefined.

- [ ] **Step 3: Implement** — create `internal/project/violations_schema.go`:

```go
package project

import "fmt"

// Violation families supported by the deterministic generator.
const (
	ViolationFamilyOversize     = "oversize"
	ViolationFamilyRateLimit    = "rate_limit"
	ViolationFamilyRouteMissing = "route_missing"
	ViolationFamilyHTTPAuth     = "http_auth"
)

var validViolationFamilies = map[string]bool{
	ViolationFamilyOversize: true, ViolationFamilyRateLimit: true,
	ViolationFamilyRouteMissing: true, ViolationFamilyHTTPAuth: true,
}

// Violation is one declared negative behavior: triggering it from Role must
// provoke Expect. SUT facts only — thresholds and codes live here, never in
// generator or executor source (cerberus stays SUT-generic).
type Violation struct {
	ID      string           `yaml:"id"`
	Family  string           `yaml:"family"`
	Role    string           `yaml:"role"`
	Trigger ViolationTrigger `yaml:"trigger"`
	Expect  ViolationExpect  `yaml:"expect"`
}

// ViolationTrigger carries the family-specific trigger fields; only the
// subset the family names is meaningful (validated).
type ViolationTrigger struct {
	Bytes       int      `yaml:"bytes"`        // oversize: payload size
	Messages    int      `yaml:"messages"`     // rate_limit: burst per window
	Windows     int      `yaml:"windows"`      // rate_limit: violating 1s windows
	Type        string   `yaml:"type"`         // frame type to send
	OmitFields  []string `yaml:"omit_fields"`  // route_missing: payload keys to drop
	Method      string   `yaml:"method"`       // http_auth
	Path        string   `yaml:"path"`         // http_auth
	DropHeaders []string `yaml:"drop_headers"` // http_auth: headers to drop
}

// ViolationExpect: FrameType+Code for error frames, CloseCode for closes,
// HTTPStatus for HTTP rejections. rate_limit carries both frame and close
// (first reaction then threshold close).
type ViolationExpect struct {
	FrameType  string `yaml:"frame_type"`
	Code       string `yaml:"code"`
	CloseCode  int    `yaml:"close_code"`
	HTTPStatus int    `yaml:"http_status"`
}

// validateViolations checks the declarations in isolation; caller wires it
// into ValidateProtocol next to validateProtocolHTTPTriggers.
func validateViolations(p *Protocol) error {
	for i, v := range p.Violations {
		prefix := fmt.Sprintf("violations[%d]", i)
		if v.ID == "" {
			return fmt.Errorf("%s.id is required", prefix)
		}
		if !validViolationFamilies[v.Family] {
			return fmt.Errorf("%s.family %q must be oversize, rate_limit, route_missing, or http_auth", prefix, v.Family)
		}
		if p.Roles[v.Role] == nil {
			return fmt.Errorf("%s.role %q does not match a declared role", prefix, v.Role)
		}
		switch v.Family {
		case ViolationFamilyOversize:
			if v.Trigger.Bytes <= 0 {
				return fmt.Errorf("%s.trigger.bytes is required for oversize", prefix)
			}
			if v.Expect.CloseCode == 0 {
				return fmt.Errorf("%s.expect.close_code is required for oversize", prefix)
			}
		case ViolationFamilyRateLimit:
			if v.Trigger.Messages <= 0 || v.Trigger.Windows <= 0 {
				return fmt.Errorf("%s.trigger.messages and .windows are required for rate_limit", prefix)
			}
			if v.Expect.FrameType == "" || v.Expect.Code == "" || v.Expect.CloseCode == 0 {
				return fmt.Errorf("%s.expect.frame_type, .code and .close_code are required for rate_limit", prefix)
			}
		case ViolationFamilyRouteMissing:
			if v.Trigger.Type == "" || len(v.Trigger.OmitFields) == 0 {
				return fmt.Errorf("%s.trigger.type and .omit_fields are required for route_missing", prefix)
			}
			if v.Expect.FrameType == "" || v.Expect.Code == "" {
				return fmt.Errorf("%s.expect.frame_type and .code are required for route_missing", prefix)
			}
		case ViolationFamilyHTTPAuth:
			if v.Trigger.Method == "" || v.Trigger.Path == "" {
				return fmt.Errorf("%s.trigger.method and .path are required for http_auth", prefix)
			}
			if v.Expect.HTTPStatus == 0 {
				return fmt.Errorf("%s.expect.http_status is required for http_auth", prefix)
			}
		}
	}
	return nil
}
```

Add to `Protocol` in `internal/project/protocol_schema.go` (after `Batches`):

```go
	// Violations declares the protocol's negative behaviors: trigger a
	// violation from a role and expect the declared rejection (error frame,
	// close code, or HTTP status). Hand-authored SUT facts; see the negative
	// case family design spec.
	Violations []Violation `yaml:"violations,omitempty"`
```

In `ValidateProtocol` (`internal/project/validate_protocol.go:179`), after the `validateProtocolHTTPTriggers` call:

```go
	if err := validateViolations(p); err != nil {
		return err
	}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/project/`
Expected: PASS (whole package — catches regressions in validation wiring).

- [ ] **Step 5: Commit**

```bash
git add internal/project/
git commit -m "feat(project): protocol violations schema + validation (negative case family)"
```

---

### Task 2: `{{pad:N}}` intrinsic in send-body templating

**Files:**
- Modify: `internal/head/agent/websocket.go` (`resolvePlaceholders`, ~line 777; regex at line 754)
- Test: `internal/head/agent/websocket_placeholder_test.go` (create; check first — if a placeholder test file already exists, append there)

**Interfaces:**
- Produces: `resolvePlaceholders` recognizes `{{pad:N}}` (N decimal bytes) anywhere in ws_send/http bodies and renders N `x` bytes. No signature change.

- [ ] **Step 1: Write the failing test**:

```go
package agent

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResolvePlaceholdersPad(t *testing.T) {
	out, err := resolvePlaceholders(nil, nil, "", `{"type":"chat:message","payload":{"text":"{{pad:100}}"}}`)
	assert.NoError(t, err)
	assert.Len(t, strings.Trim(out, `{"type":"chat:message","payload":{"text":"`), 100)
	assert.True(t, strings.HasPrefix(out, `{"type":"chat:message","payload":{"text":"xxxx`))
}

func TestResolvePlaceholdersPadZeroIsError(t *testing.T) {
	_, err := resolvePlaceholders(nil, nil, "", "{{pad:0}}")
	assert.ErrorContains(t, err, "pad")
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/head/agent/ -run TestResolvePlaceholdersPad`
Expected: FAIL — pad leaks through unresolved (or errors with "unresolved placeholder").

- [ ] **Step 3: Implement** — in `resolvePlaceholders` (websocket.go:777), inside the `ReplaceAllStringFunc` callback BEFORE the dot/param handling (a `:` token is neither a role param nor an own param):

```go
	// {{pad:N}} intrinsic: render N filler bytes (oversize-payload tests).
	if strings.HasPrefix(token, "pad:") {
		n, perr := strconv.Atoi(token[4:])
		if perr != nil || n <= 0 {
			if unresolved == "" {
				unresolved = match
			}
			return match
		}
		return strings.Repeat("x", n)
	}
```

Verify `strconv` is imported in websocket.go (add if missing). Note the regex `wsBodyPlaceholderRe = \{\{([A-Za-z0-9_.]+)\}\}` — `pad:100` contains `:` which the character class does NOT match, so the placeholder would not be seen at all. Extend the class: `\{\{([A-Za-z0-9_.:]+)\}\}` (colon only introduces intrinsics; role/param names never contain one).

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/head/agent/ -run 'Placeholder|TestResolve'`
Expected: PASS (existing placeholder tests included — the regex widening must not break `{{uuid}}` or role params).

- [ ] **Step 5: Commit**

```bash
git add internal/head/agent/
git commit -m "feat(agent): {{pad:N}} oversize payload intrinsic in body templating"
```

---

### Task 3: close-frame capture + `ws_expect_close` action

**Files:**
- Modify: `internal/types/actions_http.go` (add `WSExpectCloseAction` after `WSDisconnectAction` ~line 287)
- Modify: `internal/types/result_ws.go` (add `CloseCode int` to `WSResult`, after `MatchedCount`)
- Modify: `internal/head/agent/websocket.go`:
  - `wsEntry` struct (~line 61): add `closeCode int` + `closeReason string` fields
  - `readPump` (~line 185): extract close code from the read error
  - dispatch switch (~line 128): add `case types.WSExpectCloseAction`
  - new `doExpectClose` method next to `doReceive` (~line 1018)
- Modify: `internal/head/agent/types.go` (`TestStep` ~line 80): add `Code int` field (`json:"code,omitempty"`, comment: ws_expect_close expected close code)
- Modify: `internal/head/agent/execute_phases_steps.go` (`stepToAction` ~line 67): add the case
- Test: `internal/head/agent/ws_expect_close_test.go` (create)

**Interfaces:**
- Produces: `types.WSExpectCloseAction{ConnectionID string; Code int; Timeout int}`; `WSResult.CloseCode`; step action `"ws_expect_close"` with `TestStep.Code`.
- Consumes: `coder/websocket` `CloseError` (`errors.As(err, &ce)` → `ce.Code`, `ce.Reason`).

- [ ] **Step 1: Write the failing test** — `internal/head/agent/ws_expect_close_test.go`. Model it on the existing in-process fake-server tests in the package (find one: `grep -l 'httptest.NewServer' internal/head/agent/*_test.go` and mirror its upgrade/helper setup). Shape:

```go
package agent

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/types"
)

// The fake server closes the first connection with 1009 after receiving one frame.
func TestWSExpectClose_MatchesCode(t *testing.T) {
	srv := newFakeWSServer(t, func(entry *fakeWSConn) {
		entry.readOne(t) // wait for the trigger send
		entry.close(t, 1009, "too large")
	})
	defer srv.Close()
	e := newWebSocketExecutorForTest(t, srv.url()) // helper mirroring existing tests
	res := e.Execute(t.Context(), types.WSExpectCloseAction{
		ConnectionID: "c1", Code: 1009, Timeout: 5})
	require.True(t, res.Success())
	wr, ok := res.(types.WSResult)
	require.True(t, ok)
	require.Equal(t, 1009, wr.CloseCode)
}

func TestWSExpectClose_WrongCodeFails(t *testing.T) {
	// server closes 1008; expectation 1009 ⇒ step FAILS (mismatch is a finding)
	...same skeleton, require.False(t, res.Success())
}

func TestWSExpectClose_TimeoutWhenNoClose(t *testing.T) {
	// server stays silent; Timeout 1 ⇒ FAIL with "no close" evidence
	...require.False(t, res.Success()); require.Contains(t, res.Error(), "close")
}
```

NOTE: `newFakeWSServer`/`newWebSocketExecutorForTest` are names to CREATE in this test file by extracting the setup the package's existing ws tests use (read one first — e.g. the batch-decomposition or ExpectAbsent tests — and copy their server+executor construction). If a shared helper already exists, use it under its real name instead of creating a duplicate.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/head/agent/ -run TestWSExpectClose`
Expected: FAIL — `WSExpectCloseAction` undefined.

- [ ] **Step 3: Implement.**

`internal/types/actions_http.go`:

```go
// WSExpectCloseAction awaits the peer's close frame on a connection and
// asserts its status code — the deterministic rejection observable for
// oversize/policy violations (negative case family).
type WSExpectCloseAction struct {
	ConnectionID string `json:"connection_id"`
	Code         int    `json:"code"`    // expected close status code
	Timeout      int    `json:"timeout"` // seconds; 0 ⇒ executor default
}
```

`internal/types/result_ws.go` — inside `WSResult`, after `MatchedCount`:

```go
	// CloseCode is the peer's close status code observed by ws_expect_close
	// (zero for every other action).
	CloseCode int `json:"close_code,omitempty"`
```

`websocket.go` `wsEntry` fields (next to `pumpErr`):

```go
	closeCode   int    // peer close status captured by the pump (0 = none)
	closeReason string
```

`readPump` error branch:

```go
		mt, data, err := entry.conn.Read(entry.ctx)
		if err != nil {
			entry.pumpErr = err
			var ce websocket.CloseError
			if errors.As(err, &ce) {
				entry.closeCode = int(ce.Code)
				entry.closeReason = ce.Reason
			}
			return
		}
```

(`errors` import if missing; `websocket.CloseError.Code` is `websocket.StatusCode` — convert to int.)

Dispatch (~line 134):

```go
	case types.WSExpectCloseAction:
		return e.doExpectClose(ctx, a, start)
```

`doExpectClose` next to `doReceive`:

```go
// doExpectClose waits for the connection's pump to exit (peer close) within
// the timeout and asserts the captured close status code. A connection that
// never closes, or closes with a different code, FAILS — a missing rejection
// is a product finding, not a test error.
func (e *WebSocketExecutor) doExpectClose(ctx context.Context, a types.WSExpectCloseAction, start time.Time) types.ExecutorResult {
	entry := e.conn(a.ConnectionID)
	if entry == nil {
		return types.WSResult{OK: false, Err: "expect_close: unknown connection_id " + a.ConnectionID, Latency: time.Since(start)}
	}
	timeout := wsTimeout(a.Timeout)
	select {
	case <-entry.done:
	case <-time.After(timeout):
		return types.WSResult{OK: false, Err: fmt.Sprintf("expect_close: no close within %s (connection still open)", timeout), Latency: time.Since(start)}
	case <-ctx.Done():
		return types.WSResult{OK: false, Err: "expect_close: ctx done", Latency: time.Since(start)}
	}
	observed := entry.closeCode
	if observed != a.Code {
		return types.WSResult{OK: false, Err: fmt.Sprintf("expect_close: code %d, want %d (reason %q)", observed, a.Code, entry.closeReason), CloseCode: observed, Latency: time.Since(start)}
	}
	return types.WSResult{OK: true, CloseCode: observed, MatchedMessage: fmt.Sprintf("close %d %q", observed, entry.closeReason), Latency: time.Since(start)}
}
```

Use the package's REAL timeout helper name — check `doReceive` for how it converts `a.Timeout` seconds to duration (e.g. `time.Duration(a.Timeout) * time.Second` with a default constant) and mirror it exactly; do not invent a second idiom.

`internal/head/agent/types.go` `TestStep`, after `Timeout`:

```go
	// ws_expect_close: expected close status code (e.g. 1009).
	Code int `json:"code,omitempty"`
```

`execute_phases_steps.go` `stepToAction`:

```go
	case "ws_expect_close":
		return types.WSExpectCloseAction{ConnectionID: s.ConnectionID, Code: s.Code, Timeout: s.Timeout}, nil
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/head/agent/ -run 'TestWSExpectClose|TestStep'`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/types/ internal/head/agent/
git commit -m "feat(agent): ws_expect_close action — close-code assertion for negative cases"
```

---

### Task 4: `violationCases` generator

**Files:**
- Modify: `internal/head/scout/ws_cases.go` (new `violationCases` + wiring in `wsCasesForService`)
- Test: `internal/head/scout/ws_cases_test.go` (append)

**Interfaces:**
- Consumes: `project.Violation` (Task 1), `ws_expect_close` step + `{{pad:N}}` (Tasks 2-3), existing `wsSendBody`, `wsCaseID`.
- Produces: cases with IDs `ws-<svc>-<role>-<violation-id>`; emitted from `wsCasesForService` after `wsRelayCoverageCases`, BEFORE the per-role loop (so the per-role skip tables are not needed — violation cases open their own connections).

- [ ] **Step 1: Write the failing test** (append to `ws_cases_test.go`; reuse `realE2EFixture`-style literals):

```go
func violationFixture() *project.Config {
	return &project.Config{
		Services: []project.Service{{
			Name: "rt", URL: "http://x",
			Protocol: &project.Protocol{
				TypePath: "type",
				Roles: map[string]*project.ProtocolRole{
					"web":    {CredentialRef: "web"},
					"bridge": {CredentialRef: "b1"},
				},
				Violations: []project.Violation{
					{ID: "oversize-message", Family: project.ViolationFamilyOversize, Role: "web",
						Trigger: project.ViolationTrigger{Bytes: 1048577, Type: "chat:message"},
						Expect:  project.ViolationExpect{CloseCode: 1009}},
					{ID: "missing-device-id", Family: project.ViolationFamilyRouteMissing, Role: "web",
						Trigger: project.ViolationTrigger{Type: "session:start", OmitFields: []string{"deviceId"}},
						Expect:  project.ViolationExpect{FrameType: "error", Code: "MISSING_DEVICE_ID"}},
					{ID: "bridge-rate-limit", Family: project.ViolationFamilyRateLimit, Role: "bridge",
						Trigger: project.ViolationTrigger{Messages: 3, Windows: 2, Type: "chat:message"},
						Expect:  project.ViolationExpect{FrameType: "error", Code: "RATE_LIMIT_EXCEEDED", CloseCode: 1008}},
					{ID: "csrf-no-origin", Family: project.ViolationFamilyHTTPAuth, Role: "web",
						Trigger: project.ViolationTrigger{Method: "POST", Path: "/api/dev/setup", DropHeaders: []string{"Origin"}},
						Expect:  project.ViolationExpect{HTTPStatus: 403}},
				},
			},
		}},
	}
}

func TestViolationCases(t *testing.T) {
	cases := WSCases(violationFixture(), "")
	byID := map[string]agent.TestCase{}
	for _, c := range cases {
		byID[c.ID] = c
	}
	t.Run("oversize", func(t *testing.T) {
		c, ok := byID["ws-rt-web-oversize-message"]
		require.True(t, ok, "ids: %v", caseIDs(cases))
		require.Len(t, c.Steps, 3)
		assert.Equal(t, "ws_connect", c.Steps[0].Action)
		assert.Equal(t, "ws_send", c.Steps[1].Action)
		assert.Contains(t, c.Steps[1].Message, "{{pad:1048577}}")
		assert.Equal(t, "ws_expect_close", c.Steps[2].Action)
		assert.Equal(t, 1009, c.Steps[2].Code)
	})
	t.Run("route_missing", func(t *testing.T) {
		c, ok := byID["ws-rt-web-missing-device-id"]
		require.True(t, ok)
		require.Len(t, c.Steps, 3)
		assert.Contains(t, c.Steps[1].Message, "session:start")
		assert.NotContains(t, c.Steps[1].Message, "deviceId")
		assert.Equal(t, "ws_receive", c.Steps[2].Action)
		assert.Equal(t, "error", c.Steps[2].Type)
		assert.Equal(t, "MISSING_DEVICE_ID", c.Steps[2].Asserts["payload.code"])
	})
	t.Run("rate_limit", func(t *testing.T) {
		c, ok := byID["ws-rt-bridge-bridge-rate-limit"]
		require.True(t, ok)
		// 1 connect + windows*(messages sends + 1 pacer receive) + final frame
		// receive + close expect. messages=3, windows=2 ⇒ 1+2*4+1+1 = 11.
		require.Len(t, c.Steps, 11)
		sends, pacers := 0, 0
		for _, s := range c.Steps {
			switch s.Action {
			case "ws_send":
				sends++
			case "ws_receive":
				if s.ExpectAbsent {
					pacers++
				}
			}
		}
		assert.Equal(t, 6, sends)
		assert.Equal(t, 1, pacers) // only BETWEEN windows; none after the last
		last := c.Steps[len(c.Steps)-1]
		assert.Equal(t, "ws_expect_close", last.Action)
		assert.Equal(t, 1008, last.Code)
	})
	t.Run("http_auth", func(t *testing.T) {
		c, ok := byID["ws-rt-web-csrf-no-origin"]
		require.True(t, ok)
		require.Len(t, c.Steps, 1)
		assert.Equal(t, "http_request", c.Steps[0].Action)
		assert.Equal(t, "POST", c.Steps[0].Method)
		assert.Equal(t, "/api/dev/setup", c.Steps[0].URL)
		assert.Equal(t, 403, c.Steps[0].Status)
		assert.NotContains(t, c.Steps[0].Headers, "Origin")
	})
	t.Run("no claims binding", func(t *testing.T) {
		for _, c := range cases {
			assert.Empty(t, c.Claims, "violation case %s must not bind claims", c.ID)
		}
	})
}
```

NOTE: check `TestStep`'s HTTP status field name in `internal/head/agent/types.go` (~line 100: "expected response status") — use its REAL name in the test and generator, not `Status` if that is wrong.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/head/scout/ -run TestViolationCases`
Expected: FAIL — violation cases not emitted.

- [ ] **Step 3: Implement** — `internal/head/scout/ws_cases.go`:

```go
// violationCases emits one deterministic case per declared protocol
// violation (negative case family). Shapes:
//   - route_missing: connect → send (payload minus omitted fields) →
//     receive expect.frame_type asserting payload.code
//   - oversize: connect → send {{pad:N}} body → ws_expect_close
//   - rate_limit: connect → per window: messages sends + one ExpectAbsent
//     pacer receive (waits out the 1s fixed window); final window adds the
//     error-frame receive then ws_expect_close
//   - http_auth: one http_request step (dropped headers omitted, status
//     asserted)
// No claims binding (spec Non-goals). IDs: ws-<svc>-<role>-<id>.
func violationCases(svc project.Service) []agent.TestCase {
	if svc.Protocol == nil || len(svc.Protocol.Violations) == 0 {
		return nil
	}
	var cases []agent.TestCase
	for _, v := range svc.Protocol.Violations {
		tc := agent.TestCase{
			ID:      wsCaseID(svc.Name, v.Role, v.ID),
			Name:    fmt.Sprintf("%s %s must provoke %s", svc.Name, v.Role, v.ID),
			Service: svc.Name,
			Target:  svc.URL,
			Action:  "ws_flow",
			Expectation: fmt.Sprintf("%s: %s triggering %s is rejected (%s)",
				svc.Name, v.Role, v.ID, v.Family),
			Priority: 0.7,
		}
		switch v.Family {
		case project.ViolationFamilyRouteMissing:
			tc.Steps = []agent.TestStep{
				{Action: "ws_connect", ConnectionID: v.Role, Role: v.Role},
				{Action: "ws_send", ConnectionID: v.Role, Message: wsSendBody(v.Trigger.Type, nil)},
				{Action: "ws_receive", ConnectionID: v.Role, Type: v.Expect.FrameType,
					Asserts: map[string]any{"payload.code": v.Expect.Code}, Timeout: 10},
			}
		case project.ViolationFamilyOversize:
			tc.Steps = []agent.TestStep{
				{Action: "ws_connect", ConnectionID: v.Role, Role: v.Role},
				{Action: "ws_send", ConnectionID: v.Role,
					Message: wsSendBody(v.Trigger.Type, map[string]string{"text": "{{pad:" + strconv.Itoa(v.Trigger.Bytes) + "}}"}))},
				{Action: "ws_expect_close", ConnectionID: v.Role, Code: v.Expect.CloseCode, Timeout: 10},
			}
		case project.ViolationFamilyRateLimit:
			tc.Steps = append(tc.Steps, agent.TestStep{Action: "ws_connect", ConnectionID: v.Role, Role: v.Role})
			for w := 0; w < v.Trigger.Windows; w++ {
				for i := 0; i < v.Trigger.Messages; i++ {
					tc.Steps = append(tc.Steps, agent.TestStep{Action: "ws_send", ConnectionID: v.Role,
						Message: wsSendBody(v.Trigger.Type, nil)})
				}
				if w < v.Trigger.Windows-1 {
					// Pacer: wait out the 1s fixed window; nothing may match the
					// trigger type meanwhile (frames sent, not received).
					tc.Steps = append(tc.Steps, agent.TestStep{Action: "ws_receive", ConnectionID: v.Role,
						Type: v.Trigger.Type, ExpectAbsent: true, Timeout: 1})
				}
			}
			tc.Steps = append(tc.Steps,
				agent.TestStep{Action: "ws_receive", ConnectionID: v.Role, Type: v.Expect.FrameType,
					Asserts: map[string]any{"payload.code": v.Expect.Code}, Timeout: 10},
				agent.TestStep{Action: "ws_expect_close", ConnectionID: v.Role, Code: v.Expect.CloseCode, Timeout: 10})
		case project.ViolationFamilyHTTPAuth:
			tc.Steps = []agent.TestStep{{
				Action: "http_request", URL: v.Trigger.Path, Method: v.Trigger.Method,
				Role: v.Role, Status: v.Expect.HTTPStatus,
			}}
		default:
			continue // validation rejects unknown families; defensive
		}
		cases = append(cases, tc)
	}
	return cases
}
```

Wiring in `wsCasesForService`, after `rcCases` are appended (before the per-role loop):

```go
	cases = append(cases, violationCases(svc)...)
```

IMPORTANT checks while implementing: (a) `wsSendBody(v.Trigger.Type, nil)` for route_missing deliberately sends NO payload — verify against `wsSendBody` (it emits `{"type":...}` when payload is empty), which is exactly "field omitted"; (b) for http_auth the URL must resolve against the service base — look at how `wsHTTPTriggerCases` builds absolute URLs from a path and mirror it (the test asserts the step URL is the raw path only if that is what the existing convention does — adjust test to reality); (c) `TestStep.Status` field name — mirror the real one; (d) `strconv` import.

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/head/scout/`
Expected: PASS (whole package — the new cases must not disturb existing golden counts; if an existing test asserts the exact case count for a fixture WITHOUT violations, it stays green because violationCases returns nil there).

- [ ] **Step 5: Commit**

```bash
git add internal/head/scout/
git commit -m "feat(scout): violationCases deterministic generator for protocol violations"
```

---

### Task 5: dogfood declarations (probe → pin)

**Files:**
- Modify: `dogfood/ws-realtime/.cerberus/protocols/open-agents.yaml`

**Interfaces:**
- Consumes: Task 1 schema; Task 4 generator.
- Produces: the live violations corpus for ws-realtime.

- [ ] **Step 1: Probe the three unpinned rejection shapes against the live dev server** (start it like `scripts/integration-openagents.sh` does; tear down after):
  - CSRF: `curl -si -X POST http://localhost:8989/api/dev/setup -H 'Content-Type: application/json' -d '{}'` WITHOUT an Origin header → record the exact status line (`middleware/security.ts` returns JSON `CSRF_ERROR`; is it 403?).
  - JWT-without-exp: mint an HS256 token with `alg HS256`, payload `{"sub":"dev","exp":<now+10y>}` removed (no exp claim), secret `JWT_SECRET` from `.dev.vars` → `curl -si http://localhost:8989/api/sessions -H "Authorization: Bearer <token>"` → record status (does an exp-less token pass or 401?).
  - SEC-1 IDOR: with the dev user's JWT, request a resource owned by a different userId (e.g. `GET /api/sessions?userId=<other-uuid>` or a path-param variant — find the sharable route in `apps/api/src/routes/`) → record status (403 vs 404 vs leak).
  Record each result as a comment next to the declaration written in Step 2. If JWT-without-exp turns out NOT to be rejected (design allows it), record that as an open-agents FINDING (goes into the Task 6 findings note) and declare the violation only if the behavior is a rejection.

- [ ] **Step 2: Write the declarations** — append to `dogfood/ws-realtime/.cerberus/protocols/open-agents.yaml`:

```yaml
# Negative behaviors (negative case family spec 2026-08-16). Sources:
# room.ts:31-33 (size/close codes), room.ts:37+243-250 & rateLimiter.ts:17-18
# (limits), room.ts:439,450+543 (error frames), security.ts (CSRF).
violations:
  - id: oversize-message
    family: oversize
    role: web
    trigger: {bytes: 1048577, type: chat:message}
    expect: {close_code: 1009}
  - id: bridge-rate-limit
    family: rate_limit
    role: bridge
    trigger: {messages: 220, windows: 6, type: chat:message}
    expect: {frame_type: error, code: RATE_LIMIT_EXCEEDED, close_code: 1008}
  - id: missing-device-id
    family: route_missing
    role: web
    trigger: {type: session:start, omit_fields: [deviceId]}
    expect: {frame_type: error, code: MISSING_DEVICE_ID}
  - id: csrf-no-origin
    family: http_auth
    role: web
    trigger: {method: POST, path: /api/dev/setup, drop_headers: [Origin]}
    expect: {http_status: <probed-status>}
  # + JWT-without-exp / IDOR entries per Step 1 probe outcomes (only if the
  # probed behavior is a rejection; record findings otherwise)
```

Adjust `role:` values to the role names the ws-realtime protocol actually declares (check its `roles:` map first — `web`/`bridge` vs other names).

- [ ] **Step 3: Validate config loads**

Run: `cd dogfood/ws-realtime && ../../build/cerberus validate` (or the repo's real config-check command — check `cmd/cerberus` subcommands; if none, run a 5-second `cerberus run` and abort — validation errors surface at startup).
Expected: no violations[*] errors.

- [ ] **Step 4: Commit**

```bash
git add dogfood/ws-realtime/
git commit -m "feat(dogfood): ws-realtime violations declarations (probed against live api)"
```

---

### Task 6: Live validation run + findings note

**Files:**
- Create: `cerberus-docs/technical/dogfood/2026-08-16-negative-case-family.md`

- [ ] **Step 1: Full run** — dev server up, then:

```bash
make build
cd dogfood/ws-realtime && CERBERUS_MIGRATION_DIR=../../migrations ../../build/cerberus run
```

- [ ] **Step 2: Assert the negative family**:
  - every `ws-realtime-*-<violation-id>` case completes PASS (grep the log for each violation id);
  - health gates: `grep -c 'judge failed'`, `grep -c 'insufficient budget'`, `grep -c '"target":""'`, hallucinated-id pattern — all 0;
  - overall exit code: unchanged semantics (claims gate may still exit 3 for the emulated-only ws-relay claim — that is PRE-EXISTING and expected in ws-realtime; negatives must not add new red).

- [ ] **Step 3: If a negative case FAILS**: triage per spec Error-handling — a real missing rejection in open-agents is a FINDING, not a cerberus bug. Record it; only fix cerberus when the generator/executor is at fault.

- [ ] **Step 4: Write the findings note** `cerberus-docs/technical/dogfood/2026-08-16-negative-case-family.md`: run summary (case list + verdicts), probed rejection shapes with sources, any open-agents findings (incl. JWT/IDOR probe outcomes and the earlier 5 unfiled fidelity-ladder findings if still unfiled — port them here so they stop being oral tradition).

- [ ] **Step 5: Commit + wrap**

```bash
git add cerberus-docs/
git commit -m "docs: negative case family live-run findings"
make test
```

Then: memory extraction (violation facts, probe outcomes, run recipe deltas) and branch finish per finishing-a-development-branch.

---

## Self-Review (done at plan time)

- Spec coverage: schema/validator (T1), pad intrinsic (T2), close assertion (T3), generator (T4), dogfood wiring + probes (T5), live validation + error-handling policy (T6). Rate-limit two-stage semantics and pacer decision are encoded in T4's step math (test pins 1+2*4+1+1=11). ✓
- Placeholders: the three probe outcomes in T5 are investigation steps with recorded outputs (repo precedent: fidelity-ladder probes), not unspecified work; every code step carries its code. ✓
- Type consistency: `Violation`/`ViolationTrigger`/`ViolationExpect` field names identical in T1 test, T1 impl, T4 fixture/generator, T5 yaml. `WSExpectCloseAction{ConnectionID,Code,Timeout}` / `TestStep.Code` / `ws_expect_close` consistent across T3/T4. Two honesty markers left for the implementer: the real HTTP-status field name on TestStep (T4 note c) and the fake-WS-server helper extraction (T3 note). ✓
