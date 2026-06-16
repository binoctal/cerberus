# MCP Server Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add MCP Server to Cerberus so Claude Code can trigger tests, poll progress, and handle critical escalation events — without leaving the CLI.

**Architecture:** New `internal/mcp/` package implements MCP JSON-RPC protocol over stdin/stdout. Agent executor gets 4 escalation checkpoints injected via `EscalationGate` interface. `cerberus init` auto-writes `.claude/settings.json`. Existing code behavior unchanged — `NoOpEscalationGate` used in CLI mode.

**Tech Stack:** Go 1.25, MCP JSON-RPC 2.0 protocol (no external library — hand-rolled ~150 lines), existing Cerberus internals.

---

## File Structure

| Action | File | Responsibility |
|--------|------|----------------|
| Create | `internal/mcp/protocol.go` | JSON-RPC 2.0 types + stdin/stdout read/write |
| Create | `internal/mcp/server.go` | MCP Server: tool registration, request dispatch |
| Create | `internal/mcp/tools.go` | 5 tool definitions + handler implementations |
| Create | `internal/mcp/escalation.go` | `MCPEscalationGate` — channel-based pause/resume |
| Create | `internal/mcp/types.go` | Shared types for MCP layer (progress, report, etc.) |
| Create | `internal/escalation/gate.go` | `EscalationGate` interface + `EscalationEvent` + `EscalationDecision` + `NoOpEscalationGate` |
| Modify | `internal/head/agent/executor.go` | Add `gate escalation.Gate` field + 4 checkpoint calls |
| Modify | `internal/session/lifecycle.go` | Pass `EscalationGate` to Agent head construction |
| Modify | `cmd/cerberus/main.go` | Add `mcpCmd()`, enhance `initCmd()` |
| Create | `internal/mcp/server_test.go` | Tests for MCP server |
| Create | `internal/mcp/escalation_test.go` | Tests for escalation gate |
| Create | `internal/escalation/gate_test.go` | Tests for gate interface + NoOp |

---

### Task 1: Escalation Gate Interface

**Files:**
- Create: `internal/escalation/gate.go`
- Create: `internal/escalation/gate_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/escalation/gate_test.go
package escalation

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNoOpGate_AlwaysContinues(t *testing.T) {
	gate := NoOpGate{}
	decision := gate.Check(context.Background(), EscalationEvent{
		Type:    "budget_warning",
		Message: "80% budget used",
	})
	assert.Equal(t, EscalationContinue, decision.Action)
}

func TestNoOpGate_AllEventTypes(t *testing.T) {
	gate := NoOpGate{}
	types := []string{"budget_warning", "systemic_failure", "destructive_risk", "target_unreachable"}
	for _, et := range types {
		decision := gate.Check(context.Background(), EscalationEvent{Type: et})
		assert.Equal(t, EscalationContinue, decision.Action, "event type: %s", et)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/escalation/... -v`
Expected: FAIL — package doesn't exist

- [ ] **Step 3: Write implementation**

```go
// internal/escalation/gate.go
// Package escalation defines the escalation interface for critical event handling.
package escalation

import "context"

// Decision actions returned by an EscalationGate.
const (
	DecisionContinue = "continue"
	DecisionAbort    = "abort"
	DecisionSkipCase = "skip_case"
)

// EscalationGate is called at critical points during session execution.
// Implementations may block (e.g. wait for user input via MCP) or return immediately.
type Gate interface {
	Check(ctx context.Context, event Event) Decision
}

// Event describes a critical situation that the AI cannot safely decide on its own.
type Event struct {
	Type      string         `json:"type"`
	Message   string         `json:"message"`
	SessionID string         `json:"session_id"`
	Data      map[string]any `json:"data,omitempty"`
}

// Decision is the user's (or default) response to an escalation event.
type Decision struct {
	Action  string `json:"action"`
	Payload string `json:"payload,omitempty"`
}

// EscalationContinue is the default "proceed autonomously" decision.
var EscalationContinue = Decision{Action: DecisionContinue}

// NoOpGate always returns "continue" — used in CLI mode where no MCP is active.
type NoOpGate struct{}

func (NoOpGate) Check(_ context.Context, _ Event) Decision {
	return EscalationContinue
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/escalation/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/escalation/
git commit -m "feat(escalation): add EscalationGate interface with NoOp implementation"
```

---

### Task 2: Escalation Checkpoints in Agent Executor

**Files:**
- Modify: `internal/head/agent/executor.go`
- Create: `internal/head/agent/executor_test.go` additions (if needed)

- [ ] **Step 1: Write the failing test**

Add to a new test file `internal/head/agent/gate_test.go`:

```go
// internal/head/agent/gate_test.go
package agent

import (
	"context"
	"sync"
	"testing"

	"github.com/binoctal/cerberus/internal/escalation"
	"github.com/binoctal/cerberus/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// recordingGate records all events and returns continue.
type recordingGate struct {
	mu     sync.Mutex
	events []escalation.Event
}

func (r *recordingGate) Check(_ context.Context, event escalation.Event) escalation.Decision {
	r.mu.Lock()
	r.events = append(r.events, event)
	r.mu.Unlock()
	return escalation.EscalationContinue
}

func (r *recordingGate) Events() []escalation.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]escalation.Event{}, r.events...)
}

func setupGateTest(t *testing.T) (*ReActLoop, *recordingGate, *store.Store) {
	t.Helper()
	s, err := store.New(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })
	gate := &recordingGate{}
	engine := NewRuleEngine("http://localhost:9999", nil)
	httpExec := NewHTTPActionExecutor("http://localhost:9999", zap.NewNop())
	loop := NewReActLoopWithGate(nil, s, engine, httpExec, DefaultReActConfig(), gate, zap.NewNop())
	return loop, gate, s
}

func TestExecutePlan_TracksConsecutiveFailures(t *testing.T) {
	loop, gate, s := setupGateTest(t)
	ctx := context.Background()

	// Need migrations for traces.
	require.NoError(t, store.RunMigrations(ctx, s.DB(), "../../migrations"))

	plan := &TestPlan{
		Goal:       "test systemic failure",
		ProjectURL: "http://localhost:9999",
		Cases: []TestCase{
			{ID: "tc-1", Name: "fail 1", Target: "/api/fail", Method: "GET", Expectation: "should fail"},
			{ID: "tc-2", Name: "fail 2", Target: "/api/fail", Method: "GET", Expectation: "should fail"},
			{ID: "tc-3", Name: "fail 3", Target: "/api/fail", Method: "GET", Expectation: "should fail"},
			{ID: "tc-4", Name: "fail 4", Target: "/api/fail", Method: "GET", Expectation: "should fail"},
			{ID: "tc-5", Name: "fail 5", Target: "/api/fail", Method: "GET", Expectation: "should fail"},
		},
	}

	results, err := loop.ExecutePlan(ctx, plan, "test-session")
	assert.NoError(t, err)
	assert.Len(t, results, 5)

	// Should have triggered systemic_failure escalation after consecutive failures.
	events := gate.Events()
	hasSystemicFailure := false
	for _, e := range events {
		if e.Type == "systemic_failure" {
			hasSystemicFailure = true
		}
	}
	assert.True(t, hasSystemicFailure, "expected systemic_failure escalation after 5 consecutive failures")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/head/agent/ -run TestExecutePlan_TracksConsecutiveFailures -v`
Expected: FAIL — `NewReActLoopWithGate` not defined

- [ ] **Step 3: Modify executor.go — add gate field and constructor**

Add to `internal/head/agent/executor.go`:

1. Add import for `"github.com/binoctal/cerberus/internal/escalation"`.
2. Add `gate escalation.Gate` field to `ReActLoop` struct.
3. Add `NewReActLoopWithGate` constructor that accepts a gate.
4. Modify existing `NewReActLoop` to call `NewReActLoopWithGate` with `escalation.NoOpGate{}`.
5. In `ExecutePlan`, after each `executeStep` result, check for consecutive failures and call `r.gate.Check()` with `systemic_failure` event when threshold (5) is reached.
6. In `executeStep`, before `r.executor.Execute`, check action for destructive methods and call `r.gate.Check()` with `destructive_risk` event.

Changes to `ReActLoop` struct:

```go
type ReActLoop struct {
	driver   *ai.Driver
	store    *store.Store
	engine   *RuleEngine
	executor ActionExecutor
	recovery recoverer
	config   ReActConfig
	gate     escalation.Gate
	logger   *zap.Logger
}
```

New constructor:

```go
func NewReActLoopWithGate(
	driver *ai.Driver,
	store *store.Store,
	engine *RuleEngine,
	executor ActionExecutor,
	config ReActConfig,
	gate escalation.Gate,
	logger *zap.Logger,
) *ReActLoop {
	return &ReActLoop{
		driver:   driver,
		store:    store,
		engine:   engine,
		executor: executor,
		recovery: NewRecovery(driver, store, config, logger),
		config:   config,
		gate:     gate,
		logger:   logger,
	}
}
```

Update existing constructor:

```go
func NewReActLoop(
	driver *ai.Driver,
	store *store.Store,
	engine *RuleEngine,
	executor ActionExecutor,
	config ReActConfig,
	logger *zap.Logger,
) *ReActLoop {
	return NewReActLoopWithGate(driver, store, engine, executor, config, escalation.NoOpGate{}, logger)
}
```

Systemic failure tracking in `ExecutePlan`:

```go
func (r *ReActLoop) ExecutePlan(ctx context.Context, plan *TestPlan, sessionID string) ([]StepResult, error) {
	var results []StepResult
	consecutiveFailures := 0
	for i := range plan.Cases {
		tc := &plan.Cases[i]
		r.logger.Info("executing test case",
			zap.String("case_id", tc.ID),
			zap.String("target", tc.Target),
		)
		result := r.executeStep(ctx, tc, sessionID)
		results = append(results, result)

		if result.Status == StepFailed || result.Status == StepSkipped {
			consecutiveFailures++
		} else {
			consecutiveFailures = 0
		}

		if consecutiveFailures >= 5 {
			decision := r.gate.Check(ctx, escalation.Event{
				Type:      "systemic_failure",
				Message:   fmt.Sprintf("%d consecutive test failures — target may be down", consecutiveFailures),
				SessionID: sessionID,
				Data:      map[string]any{"consecutive_failures": consecutiveFailures, "last_case": tc.ID},
			})
			if decision.Action == escalation.DecisionAbort {
				r.logger.Info("session aborted by user decision", zap.String("reason", "systemic_failure"))
				return results, fmt.Errorf("session aborted: systemic failure, user chose to stop")
			}
			// "continue" resets counter; user chose to keep going.
			consecutiveFailures = 0
		}

		r.logger.Info("test case completed",
			zap.String("case_id", tc.ID),
			zap.String("status", string(result.Status)),
			zap.Int("attempts", result.Attempts),
		)
	}
	return results, nil
}
```

Destructive risk check in `executeStep`, before `r.executor.Execute` calls (both rule engine path and ReAct path):

```go
// In executeStep, after the action is determined (rule engine or steer), before execution:
if isDestructive(action) {
	decision := r.gate.Check(ctx, escalation.Event{
		Type:      "destructive_risk",
		Message:   fmt.Sprintf("Action %s %s may modify or delete data", action.Method, action.Target),
		SessionID: sessionID,
		Data:      map[string]any{"action_type": string(action.Type), "method": action.Method, "target": action.Target},
	})
	if decision.Action == escalation.DecisionSkipCase {
		return StepResult{
			TestCase: tc, Status: StepSkipped, TraceID: traceID,
			Attempts: 0, Duration: time.Since(start),
			Error: fmt.Errorf("skipped: destructive action, user chose to skip"),
		}
	}
}
```

Add helper:

```go
func isDestructive(action Action) bool {
	m := strings.ToUpper(action.Method)
	return m == "DELETE" || m == "DROP" || strings.Contains(strings.ToUpper(action.Target), "/delete") || strings.Contains(strings.ToUpper(action.Target), "/drop")
}
```

Also add `import "strings"` if not already present.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/head/agent/ -run TestExecutePlan_TracksConsecutiveFailures -v`
Expected: PASS

- [ ] **Step 5: Run all existing agent tests to verify no regression**

Run: `go test ./internal/head/agent/... -v`
Expected: All PASS — `NoOpGate` used by default, existing behavior unchanged.

- [ ] **Step 6: Commit**

```bash
git add internal/head/agent/executor.go internal/head/agent/gate_test.go
git commit -m "feat(agent): add escalation checkpoints for systemic failure and destructive risk"
```

---

### Task 3: MCP Protocol Layer

**Files:**
- Create: `internal/mcp/protocol.go`
- Create: `internal/mcp/types.go`

- [ ] **Step 1: Write implementation**

```go
// internal/mcp/protocol.go
// Package mcp implements the Model Context Protocol server for Cerberus.
package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
)

// JSON-RPC 2.0 types.

type jsonRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Result  any    `json:"result,omitempty"`
	Error   *jsonRPCError `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type toolDefinition struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema any    `json:"inputSchema"`
}

type toolCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

type listToolsResult struct {
	Tools []toolDefinition `json:"tools"`
}

type callToolResult struct {
	Content []toolContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

type toolContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// conn wraps stdin/stdout for JSON-RPC communication.
type conn struct {
	reader *bufio.Reader
	writer io.Writer
}

func newConn(r io.Reader, w io.Writer) *conn {
	return &conn{reader: bufio.NewReader(r), writer: w}
}

func (c *conn) readRequest() (jsonRPCRequest, error) {
	line, err := c.reader.ReadBytes('\n')
	if err != nil {
		return jsonRPCRequest{}, fmt.Errorf("read: %w", err)
	}
	var req jsonRPCRequest
	if err := json.Unmarshal(line, &req); err != nil {
		return jsonRPCRequest{}, fmt.Errorf("unmarshal: %w", err)
	}
	return req, nil
}

func (c *conn) writeResponse(resp jsonRPCResponse) error {
	data, err := json.Marshal(resp)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	data = append(data, '\n')
	_, err = c.writer.Write(data)
	return err
}

func (c *conn) writeError(id int, code int, msg string) error {
	return c.writeResponse(jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &jsonRPCError{Code: code, Message: msg},
	})
}

func textResult(text string) callToolResult {
	return callToolResult{
		Content: []toolContent{{Type: "text", Text: text}},
	}
}

func errorResult(msg string) callToolResult {
	return callToolResult{
		Content: []toolContent{{Type: "text", Text: msg}},
		IsError: true,
	}
}
```

```go
// internal/mcp/types.go
package mcp

// SessionProgress tracks the state of a running test session.
type SessionProgress struct {
	SessionID string `json:"session_id"`
	Phase     string `json:"phase"` // "scout" | "agent" | "examiner" | "completed" | "failed" | "pending_decision"
	Completed int    `json:"completed"`
	Total     int    `json:"total"`
	Status    string `json:"status"`
	Event     *PendingEvent `json:"event,omitempty"`
}

// PendingEvent describes a critical event awaiting user decision.
type PendingEvent struct {
	Type    string         `json:"type"`
	Message string         `json:"message"`
	Data    map[string]any `json:"data,omitempty"`
}

// RunParams is the input for cerberus_run tool.
type RunParams struct {
	Goal string `json:"goal"`
	URL  string `json:"url"`
}

// DecideParams is the input for cerberus_decide tool.
type DecideParams struct {
	SessionID string `json:"session_id"`
	Action    string `json:"action"`
	Payload   string `json:"payload,omitempty"`
}

// ReportEntry is a single test result in the report.
type ReportEntry struct {
	CaseID       string  `json:"case_id"`
	Name         string  `json:"name"`
	Target       string  `json:"target"`
	Status       string  `json:"status"`
	Confidence   float64 `json:"confidence"`
	Reasoning    string  `json:"reasoning,omitempty"`
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/mcp/...`
Expected: success

- [ ] **Step 3: Commit**

```bash
git add internal/mcp/protocol.go internal/mcp/types.go
git commit -m "feat(mcp): add JSON-RPC protocol layer and shared types"
```

---

### Task 4: MCP Server Core

**Files:**
- Create: `internal/mcp/server.go`
- Create: `internal/mcp/server_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/mcp/server_test.go
package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/binoctal/cerberus/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func setupMCPServer(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	s, err := store.New(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	require.NoError(t, store.RunMigrations(ctx, s.DB(), "../../migrations"))
	srv := NewServer(s, zap.NewNop())
	return srv, s
}

func TestServer_ListTools(t *testing.T) {
	srv, _ := setupMCPServer(t)
	tools := srv.listTools()
	names := make(map[string]bool)
	for _, tool := range tools {
		names[tool.Name] = true
	}
	assert.True(t, names["cerberus_run"])
	assert.True(t, names["cerberus_status"])
	assert.True(t, names["cerberus_report"])
	assert.True(t, names["cerberus_decide"])
	assert.True(t, names["cerberus_cancel"])
}

func TestServer_HandleListTools(t *testing.T) {
	srv, _ := setupMCPServer(t)
	input := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}` + "\n"
	in := strings.NewReader(input)
	var out bytes.Buffer

	err := srv.handleConn(in, &out)
	require.NoError(t, err)

	var resp jsonRPCResponse
	require.NoError(t, json.Unmarshal(out.Bytes(), &resp))
	assert.Equal(t, "2.0", resp.JSONRPC)
	assert.Equal(t, 1, resp.ID)
	assert.Nil(t, resp.Error)
}

func TestServer_HandleCancelNonexistentSession(t *testing.T) {
	srv, _ := setupMCPServer(t)
	params, _ := json.Marshal(map[string]any{
		"name": "cerberus_cancel",
		"arguments": map[string]any{"session_id": "nonexistent"},
	})
	input := fmt.Sprintf(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":%s}`+"\n", string(params))
	in := strings.NewReader(input)
	var out bytes.Buffer

	err := srv.handleConn(in, &out)
	require.NoError(t, err)

	var resp jsonRPCResponse
	require.NoError(t, json.Unmarshal(out.Bytes(), &resp))
	assert.Nil(t, resp.Error)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/mcp/ -v -run TestServer`
Expected: FAIL — `Server` type not defined

- [ ] **Step 3: Write implementation**

```go
// internal/mcp/server.go
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"

	"github.com/binoctal/cerberus/internal/store"
	"go.uber.org/zap"
)

// Server implements the MCP server for Cerberus.
type Server struct {
	store  *store.Store
	logger *zap.Logger
	mu     sync.Mutex
	sessions map[string]*runningSession
}

type runningSession struct {
	progress chan SessionProgress
	cancel   context.CancelFunc
}

// NewServer creates a new MCP server.
func NewServer(s *store.Store, logger *zap.Logger) *Server {
	return &Server{
		store:    s,
		logger:   logger,
		sessions: make(map[string]*runningSession),
	}
}

// Serve starts the MCP server, reading from r and writing to w.
func (srv *Server) Serve(ctx context.Context, r io.Reader, w io.Writer) error {
	c := newConn(r, w)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		req, err := c.readRequest()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			srv.logger.Error("read request", zap.Error(err))
			continue
		}
		srv.handleRequest(ctx, c, req)
	}
}

// handleConn handles a single connection (for testing).
func (srv *Server) handleConn(r io.Reader, w io.Writer) error {
	c := newConn(r, w)
	req, err := c.readRequest()
	if err != nil {
		return err
	}
	srv.handleRequest(context.Background(), c, req)
	return nil
}

func (srv *Server) handleRequest(ctx context.Context, c *conn, req jsonRPCRequest) {
	switch req.Method {
	case "initialize":
		c.writeResponse(jsonRPCResponse{
			JSONRPC: "2.0", ID: req.ID,
			Result: map[string]any{
				"protocolVersion": "2024-11-05",
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "cerberus", "version": "0.1.0"},
			},
		})
	case "notifications/initialized":
		// No response needed for notifications.
	case "tools/list":
		c.writeResponse(jsonRPCResponse{
			JSONRPC: "2.0", ID: req.ID,
			Result: listToolsResult{Tools: srv.listTools()},
		})
	case "tools/call":
		srv.handleToolCall(ctx, c, req)
	default:
		c.writeError(req.ID, -32601, fmt.Sprintf("method not found: %s", req.Method))
	}
}

func (srv *Server) handleToolCall(ctx context.Context, c *conn, req jsonRPCRequest) {
	paramsBytes, err := json.Marshal(req.Params)
	if err != nil {
		c.writeError(req.ID, -32602, "invalid params")
		return
	}
	var params toolCallParams
	if err := json.Unmarshal(paramsBytes, &params); err != nil {
		c.writeError(req.ID, -32602, "invalid params")
		return
	}

	var result callToolResult
	switch params.Name {
	case "cerberus_run":
		result = srv.handleRun(ctx, params.Arguments)
	case "cerberus_status":
		result = srv.handleStatus(params.Arguments)
	case "cerberus_report":
		result = srv.handleReport(params.Arguments)
	case "cerberus_decide":
		result = srv.handleDecide(params.Arguments)
	case "cerberus_cancel":
		result = srv.handleCancel(params.Arguments)
	default:
		result = errorResult(fmt.Sprintf("unknown tool: %s", params.Name))
	}

	c.writeResponse(jsonRPCResponse{
		JSONRPC: "2.0", ID: req.ID, Result: result,
	})
}

// listTools returns the tool definitions.
func (srv *Server) listTools() []toolDefinition {
	return []toolDefinition{
		{
			Name:        "cerberus_run",
			Description: "Start a Cerberus test session. Returns session_id immediately. After calling, periodically call cerberus_status to check progress. Stop when status is 'completed' or 'failed'.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"goal": map[string]any{"type": "string", "description": "Test goal, e.g. 'Test all API endpoints'"},
					"url":  map[string]any{"type": "string", "description": "Target base URL, e.g. 'http://localhost:3000'"},
				},
				"required": []string{"goal", "url"},
			},
		},
		{
			Name:        "cerberus_status",
			Description: "Poll the progress of a running test session.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"session_id": map[string]any{"type": "string", "description": "Session ID from cerberus_run"},
				},
				"required": []string{"session_id"},
			},
		},
		{
			Name:        "cerberus_report",
			Description: "Get the final test report for a completed session.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"session_id": map[string]any{"type": "string", "description": "Session ID"},
				},
				"required": []string{"session_id"},
			},
		},
		{
			Name:        "cerberus_decide",
			Description: "Provide a user decision for a pending escalation event.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"session_id": map[string]any{"type": "string", "description": "Session ID"},
					"action":     map[string]any{"type": "string", "description": "Decision: 'continue', 'abort', or 'skip_case'"},
					"payload":    map[string]any{"type": "string", "description": "Optional extra info, e.g. new URL for unreachable targets"},
				},
				"required": []string{"session_id", "action"},
			},
		},
		{
			Name:        "cerberus_cancel",
			Description: "Cancel a running test session.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"session_id": map[string]any{"type": "string", "description": "Session ID to cancel"},
				},
				"required": []string{"session_id"},
			},
		},
	}
}

// handleRun starts a test session (MCP mode — returns session_id immediately).
// Actual execution is a stub for now; full integration in Task 5.
func (srv *Server) handleRun(ctx context.Context, args map[string]any) callToolResult {
	goal, _ := args["goal"].(string)
	url, _ := args["url"].(string)
	if goal == "" || url == "" {
		return errorResult("goal and url are required")
	}

	// Create session in store.
	sess, err := srv.store.CreateSession(ctx, "run", goal, "")
	if err != nil {
		return errorResult(fmt.Sprintf("create session: %v", err))
	}

	// Track as running.
	progress := make(chan SessionProgress, 16)
	runCtx, cancel := context.WithCancel(ctx)
	srv.mu.Lock()
	srv.sessions[sess.ID] = &runningSession{progress: progress, cancel: cancel}
	srv.mu.Unlock()

	// Send initial progress.
	progress <- SessionProgress{SessionID: sess.ID, Phase: "scout", Status: "running"}

	// TODO: wire to session.Run() in Task 5.
	// For now, mark as completed.
	go func() {
		defer cancel()
		<-runCtx.Done()
	}()

	b, _ := json.Marshal(map[string]string{"session_id": sess.ID, "status": "running"})
	return textResult(string(b))
}

func (srv *Server) handleStatus(args map[string]any) callToolResult {
	sessionID, _ := args["session_id"].(string)
	if sessionID == "" {
		return errorResult("session_id is required")
	}
	srv.mu.Lock()
	rs, ok := srv.sessions[sessionID]
	srv.mu.Unlock()
	if !ok {
		// Check store for completed sessions.
		sess, err := srv.store.GetSession(context.Background(), sessionID)
		if err != nil {
			return errorResult("session not found")
		}
		b, _ := json.Marshal(SessionProgress{
			SessionID: sess.ID,
			Status:    sess.Status,
		})
		return textResult(string(b))
	}

	// Read latest progress (non-blocking).
	var latest SessionProgress
	for {
		select {
		case p := <-rs.progress:
			latest = p
		default:
			if latest.SessionID == "" {
				latest = SessionProgress{SessionID: sessionID, Status: "running"}
			}
			goto done
		}
	}
done:
	b, _ := json.Marshal(latest)
	return textResult(string(b))
}

func (srv *Server) handleReport(args map[string]any) callToolResult {
	sessionID, _ := args["session_id"].(string)
	if sessionID == "" {
		return errorResult("session_id is required")
	}
	sess, err := srv.store.GetSession(context.Background(), sessionID)
	if err != nil {
		return errorResult("session not found")
	}
	b, _ := json.MarshalIndent(sess, "", "  ")
	return textResult(string(b))
}

func (srv *Server) handleDecide(args map[string]any) callToolResult {
	sessionID, _ := args["session_id"].(string)
	action, _ := args["action"].(string)
	if sessionID == "" || action == "" {
		return errorResult("session_id and action are required")
	}
	// Decision routing will be wired in Task 5 via escalation channel.
	return textResult(fmt.Sprintf(`{"session_id":"%s","decision_acknowledged":true}`, sessionID))
}

func (srv *Server) handleCancel(args map[string]any) callToolResult {
	sessionID, _ := args["session_id"].(string)
	if sessionID == "" {
		return errorResult("session_id is required")
	}
	srv.mu.Lock()
	rs, ok := srv.sessions[sessionID]
	if ok {
		rs.cancel()
		delete(srv.sessions, sessionID)
	}
	srv.mu.Unlock()
	if !ok {
		return errorResult("session not found or already completed")
	}
	return textResult(fmt.Sprintf(`{"session_id":"%s","status":"cancelled"}`, sessionID))
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/mcp/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/mcp/server.go internal/mcp/server_test.go
git commit -m "feat(mcp): add MCP server with 5 tool handlers"
```

---

### Task 5: MCP Escalation Gate

**Files:**
- Create: `internal/mcp/escalation.go`
- Create: `internal/mcp/escalation_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/mcp/escalation_test.go
package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/binoctal/cerberus/internal/escalation"
	"github.com/stretchr/testify/assert"
)

func TestMCPGate_BlocksUntilDecision(t *testing.T) {
	gate := NewMCPGate()

	done := make(chan escalation.Decision, 1)
	go func() {
		d := gate.Check(context.Background(), escalation.Event{
			Type:    "budget_warning",
			Message: "80% budget used",
		})
		done <- d
	}()

	// Gate should block — no decision yet.
	select {
	case <-done:
		t.Fatal("gate should block until decision is sent")
	case <-time.After(50 * time.Millisecond):
	}

	// Send decision.
	gate.SendDecision(escalation.Decision{Action: escalation.DecisionAbort})

	// Now it should unblock.
	select {
	case d := <-done:
		assert.Equal(t, escalation.DecisionAbort, d.Action)
	case <-time.After(1 * time.Second):
		t.Fatal("gate should unblock after decision is sent")
	}
}

func TestMCPGate_PendingEvent(t *testing.T) {
	gate := NewMCPGate()
	assert.Nil(t, gate.PendingEvent())

	go gate.Check(context.Background(), escalation.Event{Type: "systemic_failure", Message: "5 consecutive failures"})

	// Wait for event to be pending.
	time.Sleep(50 * time.Millisecond)

	evt := gate.PendingEvent()
	assert.NotNil(t, evt)
	assert.Equal(t, "systemic_failure", evt.Type)

	// Clean up.
	gate.SendDecision(escalation.EscalationContinue)
}

func TestMCPGate_ContinueByDefault(t *testing.T) {
	gate := NewMCPGate()
	// Not blocked — no pending event, returns continue.
	gate.SendDecision(escalation.EscalationContinue)
	// Verify it doesn't panic.
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/mcp/ -run TestMCPGate -v`
Expected: FAIL — `NewMCPGate` not defined

- [ ] **Step 3: Write implementation**

```go
// internal/mcp/escalation.go
package mcp

import (
	"context"
	"sync"

	"github.com/binoctal/cerberus/internal/escalation"
)

// MCPGate implements escalation.Gate for MCP mode.
// It blocks on Check() until SendDecision() is called from the MCP tool handler.
type MCPGate struct {
	mu       sync.Mutex
	pending  *escalation.Event
	decideCh chan escalation.Decision
}

// NewMCPGate creates an escalation gate for MCP mode.
func NewMCPGate() *MCPGate {
	return &MCPGate{
		decideCh: make(chan escalation.Decision, 1),
	}
}

// Check blocks until a decision is sent via SendDecision.
func (g *MCPGate) Check(ctx context.Context, event escalation.Event) escalation.Decision {
	g.mu.Lock()
	g.pending = &event
	g.mu.Unlock()

	select {
	case d := <-g.decideCh:
		g.mu.Lock()
		g.pending = nil
		g.mu.Unlock()
		return d
	case <-ctx.Done():
		g.mu.Lock()
		g.pending = nil
		g.mu.Unlock()
		return escalation.EscalationContinue
	}
}

// SendDecision sends a user decision to unblock a pending Check().
func (g *MCPGate) SendDecision(d escalation.Decision) {
	g.decideCh <- d
}

// PendingEvent returns the current escalation event waiting for a decision, or nil.
func (g *MCPGate) PendingEvent() *escalation.Event {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.pending
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/mcp/ -run TestMCPGate -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/mcp/escalation.go internal/mcp/escalation_test.go
git commit -m "feat(mcp): add MCPGate — channel-based escalation with blocking Check"
```

---

### Task 6: Wire MCP Server to Session.Run()

**Files:**
- Modify: `internal/mcp/server.go` — replace stub `handleRun` with real session execution
- Modify: `internal/session/lifecycle.go` — add `Gate` field to Session

- [ ] **Step 1: Add Gate to Session struct**

In `internal/session/lifecycle.go`, add `Gate escalation.Gate` field to `Session` struct:

```go
import "github.com/binoctal/cerberus/internal/escalation"

type Session struct {
	ID        string
	Mode      Mode
	Goal      string
	Config    *project.Config
	Store     *store.Store
	Driver    *ai.Driver
	Logger    *zap.Logger
	StartedAt time.Time
	DeepPlan  bool
	Gate      escalation.Gate
}
```

Update `NewSession` to accept gate:

```go
func NewSession(ctx context.Context, mode Mode, goal string, cfg *project.Config,
	s *store.Store, client llm.Client, logger *zap.Logger, gate escalation.Gate) (*Session, error) {
	// ... existing code ...
	if gate == nil {
		gate = escalation.NoOpGate{}
	}
	sess.Gate = gate
	// ... rest unchanged ...
}
```

In `Run()`, pass gate to ReActLoop:

```go
loop := agent.NewReActLoopWithGate(s.Driver, s.Store, engine, httpExec, config, s.Gate, s.Logger)
```

- [ ] **Step 2: Update cmd/cerberus/main.go callers**

In `runCmd()` and `verifyCmd()`, update `NewSession` calls to pass `nil` (will use NoOpGate default):

```go
sess, err := session.NewSession(ctx, session.ModeRun, goalFlag, projCfg, s, client, logger, nil)
```

```go
sess, err := session.NewSession(ctx, session.ModeVerify, goalFlag, projCfg, s, client, logger, nil)
```

- [ ] **Step 3: Update handleRun in server.go**

Replace the stub `handleRun` with real session execution:

```go
func (srv *Server) handleRun(ctx context.Context, args map[string]any) callToolResult {
	goal, _ := args["goal"].(string)
	url, _ := args["url"].(string)
	if goal == "" || url == "" {
		return errorResult("goal and url are required")
	}

	sess, err := srv.store.CreateSession(ctx, "run", goal, "")
	if err != nil {
		return errorResult(fmt.Sprintf("create session: %v", err))
	}

	progress := make(chan SessionProgress, 16)
	runCtx, cancel := context.WithCancel(ctx)
	mcpGate := NewMCPGate()

	srv.mu.Lock()
	srv.sessions[sess.ID] = &runningSession{progress: progress, cancel: cancel}
	srv.mu.Unlock()

	go func() {
		defer cancel()
		defer func() {
			srv.mu.Lock()
			delete(srv.sessions, sess.ID)
			srv.mu.Unlock()
		}()

		progress <- SessionProgress{SessionID: sess.ID, Phase: "scout", Status: "running", Completed: 0, Total: 0}

		// Load project config.
		projCfg := project.DefaultConfig()
		projCfg.Services = []project.ServiceConfig{{Name: "web", URL: url}}
		projCfg.Settings.AIBudget.Model = "mock" // TODO: read from env/cerberus config

		// Create LLM client.
		client, err := llm.NewClient("", "") // TODO: read from env
		if err != nil {
			progress <- SessionProgress{SessionID: sess.ID, Status: "failed"}
			return
		}

		// Run session with MCP gate.
		testSess, err := session.NewSession(runCtx, session.ModeRun, goal, &projCfg, srv.store, client, srv.logger, mcpGate)
		if err != nil {
			progress <- SessionProgress{SessionID: sess.ID, Status: "failed"}
			return
		}

		if err := testSess.Run(runCtx); err != nil {
			progress <- SessionProgress{SessionID: sess.ID, Status: "failed"}
			return
		}

		progress <- SessionProgress{SessionID: sess.ID, Status: "completed"}
	}()

	b, _ := json.Marshal(map[string]string{"session_id": sess.ID, "status": "running"})
	return textResult(string(b))
}
```

- [ ] **Step 4: Wire handleDecide to MCPGate**

Update `handleDecide` to find the gate for the session and send decision:

```go
func (srv *Server) handleDecide(args map[string]any) callToolResult {
	sessionID, _ := args["session_id"].(string)
	action, _ := args["action"].(string)
	payload, _ := args["payload"].(string)
	if sessionID == "" || action == "" {
		return errorResult("session_id and action are required")
	}
	srv.mu.Lock()
	rs, ok := srv.sessions[sessionID]
	srv.mu.Unlock()
	if !ok {
		return errorResult("session not found or already completed")
	}
	rs.gate.SendDecision(escalation.Decision{Action: action, Payload: payload})
	return textResult(fmt.Sprintf(`{"session_id":"%s","decision_acknowledged":true}`, sessionID))
}
```

Add `gate *MCPGate` field to `runningSession`:

```go
type runningSession struct {
	progress chan SessionProgress
	cancel   context.CancelFunc
	gate     *MCPGate
}
```

Set it in `handleRun`:

```go
srv.sessions[sess.ID] = &runningSession{progress: progress, cancel: cancel, gate: mcpGate}
```

- [ ] **Step 5: Update handleStatus to report pending events**

In `handleStatus`, after getting `rs`, check for pending escalation:

```go
// In handleStatus, after reading latest progress:
if evt := rs.gate.PendingEvent(); evt != nil {
	latest.Status = "pending_decision"
	latest.Event = &PendingEvent{Type: evt.Type, Message: evt.Message, Data: evt.Data}
}
```

- [ ] **Step 6: Run all tests**

Run: `go test ./... -v -count=1`
Expected: All PASS

- [ ] **Step 7: Commit**

```bash
git add internal/session/lifecycle.go internal/mcp/server.go cmd/cerberus/main.go
git commit -m "feat(mcp): wire MCP server to session.Run() with escalation gate"
```

---

### Task 7: MCP Subcommand + Init Enhancement

**Files:**
- Modify: `cmd/cerberus/main.go` — add `mcpCmd()`, enhance `initCmd()`

- [ ] **Step 1: Add mcpCmd**

Add new subcommand to `cmd/cerberus/main.go`:

```go
func mcpCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Start MCP server (for Claude Code integration)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.Load()
			logger, _ := zap.NewProduction()
			defer logger.Sync()

			dbPath := cfg.DBPath
			if dbFlag != "" {
				dbPath = dbFlag
			}

			s, err := store.New(dbPath)
			if err != nil {
				return fmt.Errorf("open database: %w", err)
			}
			defer s.Close()

			ctx := context.Background()
			if err := store.RunMigrations(ctx, s.DB(), cfg.MigrationDir); err != nil {
				return fmt.Errorf("run migrations: %w", err)
			}

			srv := mcp.NewServer(s, logger)
			return srv.Serve(ctx, os.Stdin, os.Stdout)
		},
	}
}
```

Add import for `"github.com/binoctal/cerberus/internal/mcp"` and `"os"`.

Register in `main()`:

```go
rootCmd.AddCommand(initCmd(), runCmd(), verifyCmd(), serveCmd(), mcpCmd())
```

- [ ] **Step 2: Enhance initCmd to write .claude/settings.json**

Add to the end of `initCmd()`'s `RunE`, after existing logic:

```go
// Configure MCP server in .claude/settings.json.
claudeDir := ".claude"
if err := os.MkdirAll(claudeDir, 0755); err == nil {
	settingsPath := claudeDir + "/settings.json"
	mcpConfig := map[string]any{
		"mcpServers": map[string]any{
			"cerberus": map[string]any{
				"command": "cerberus",
				"args":    []string{"mcp"},
			},
		},
	}

	existing, readErr := os.ReadFile(settingsPath)
	if readErr == nil {
		// Merge into existing.
		var existingMap map[string]any
		if json.Unmarshal(existing, &existingMap) == nil {
			if ms, ok := existingMap["mcpServers"].(map[string]any); ok {
				if _, exists := ms["cerberus"]; !exists {
					ms["cerberus"] = mcpConfig["mcpServers"].(map[string]any)["cerberus"]
				}
				// Already exists — skip (idempotent).
			} else {
				existingMap["mcpServers"] = mcpConfig["mcpServers"]
			}
			mcpConfig = existingMap
		}
	}

	data, _ := json.MarshalIndent(mcpConfig, "", "  ")
	if err := os.WriteFile(settingsPath, data, 0644); err == nil {
		fmt.Println("✓ Configured .claude/settings.json for MCP integration")
	}
}
```

Add import for `"encoding/json"`.

- [ ] **Step 3: Verify it builds**

Run: `go build ./cmd/cerberus/...`
Expected: success

- [ ] **Step 4: Run all tests**

Run: `go test ./... -v -count=1`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/cerberus/main.go
git commit -m "feat(cli): add mcp subcommand and auto-configure .claude/settings.json on init"
```

---

### Task 8: Crash Recovery + Final Integration Test

**Files:**
- Create: `internal/mcp/integration_test.go`
- Modify: `internal/mcp/server.go` — add crash recovery on startup

- [ ] **Step 1: Add crash recovery**

Add method to `Server`:

```go
// RecoverOrphanSessions marks all "running" sessions as "interrupted".
// Called on MCP server startup to handle crashes from previous runs.
func (srv *Server) RecoverOrphanSessions(ctx context.Context) {
	sessions, err := srv.store.ListSessions(ctx)
	if err != nil {
		return
	}
	for _, sess := range sessions {
		if sess.Status == "running" {
			_ = srv.store.UpdateSessionStatus(ctx, sess.ID, "interrupted")
			srv.logger.Info("recovered orphan session", zap.String("id", sess.ID))
		}
	}
}
```

Call in `Serve()` before the main loop:

```go
func (srv *Server) Serve(ctx context.Context, r io.Reader, w io.Writer) error {
	srv.RecoverOrphanSessions(ctx)
	// ... existing code ...
}
```

- [ ] **Step 2: Write integration test**

```go
// internal/mcp/integration_test.go
package mcp

import (
	"context"
	"testing"

	"github.com/binoctal/cerberus/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestServer_CrashRecovery(t *testing.T) {
	s, err := store.New(":memory:")
	require.NoError(t, err)
	defer s.Close()
	ctx := context.Background()
	require.NoError(t, store.RunMigrations(ctx, s.DB(), "../../migrations"))

	// Create a "running" session (simulating crash).
	sess, err := s.CreateSession(ctx, "run", "test goal", "project")
	require.NoError(t, err)

	srv := NewServer(s, zap.NewNop())
	srv.RecoverOrphanSessions(ctx)

	// The orphan should be marked as interrupted.
	recovered, err := s.GetSession(ctx, sess.ID)
	require.NoError(t, err)
	assert.Equal(t, "interrupted", recovered.Status)
}
```

- [ ] **Step 3: Run all tests**

Run: `go test ./... -v -count=1`
Expected: All PASS

- [ ] **Step 4: Commit**

```bash
git add internal/mcp/server.go internal/mcp/integration_test.go
git commit -m "feat(mcp): add crash recovery for orphan sessions on startup"
```

---

### Task 9: Final Verification

- [ ] **Step 1: Run full test suite with race detector**

Run: `go test -v -race ./...`
Expected: All PASS, no races

- [ ] **Step 2: Run linter**

Run: `make lint` or `golangci-lint run ./...`
Expected: No errors

- [ ] **Step 3: Build binary**

Run: `make build`
Expected: `bin/cerberus` created successfully

- [ ] **Step 4: Verify mcp subcommand exists**

Run: `./bin/cerberus --help`
Expected: `mcp` listed as available subcommand

- [ ] **Step 5: Final commit (if any lint fixes needed)**

```bash
git add -A
git commit -m "chore: lint fixes and final verification"
```
