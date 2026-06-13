// internal/mcp/server_test.go
package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/escalation"
	"github.com/binoctal/cerberus/internal/store"
)

func setupMCPServer(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	s, err := store.New(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	ctx := context.Background()
	require.NoError(t, store.RunMigrations(ctx, s.DB(), "../../migrations"))
	srv := NewServer(s, zap.NewNop())
	return srv, s
}

// sendRPC is a test helper that writes a JSON-RPC request and reads the response.
func sendRPC(t *testing.T, srv *Server, req jsonRPCRequest) jsonRPCResponse {
	t.Helper()
	data, err := json.Marshal(req)
	require.NoError(t, err)
	in := strings.NewReader(string(data) + "\n")
	var out bytes.Buffer
	err = srv.handleConn(in, &out)
	require.NoError(t, err)
	var resp jsonRPCResponse
	require.NoError(t, json.Unmarshal(out.Bytes(), &resp))
	return resp
}

// -------------------------------------------------------
// Protocol layer tests
// -------------------------------------------------------

func TestProtocol_ReadWriteRoundtrip(t *testing.T) {
	in := strings.NewReader(`{"jsonrpc":"2.0","id":42,"method":"ping","params":{"key":"val"}}` + "\n")
	var out bytes.Buffer
	c := newConn(in, &out)

	req, err := c.readRequest()
	require.NoError(t, err)
	assert.Equal(t, "2.0", req.JSONRPC)
	assert.Equal(t, 42, req.ID)
	assert.Equal(t, "ping", req.Method)

	resp := jsonRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: "pong"}
	c.writeResponse(resp)

	var got jsonRPCResponse
	require.NoError(t, json.Unmarshal(out.Bytes(), &got))
	assert.Equal(t, "pong", got.Result)
	assert.Equal(t, 42, got.ID)
}

func TestProtocol_ReadRequest_InvalidJSON(t *testing.T) {
	in := strings.NewReader("this is not json\n")
	var out bytes.Buffer
	c := newConn(in, &out)

	_, err := c.readRequest()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unmarshal")
}

func TestProtocol_ReadRequest_EOF(t *testing.T) {
	in := strings.NewReader("")
	var out bytes.Buffer
	c := newConn(in, &out)

	_, err := c.readRequest()
	assert.Error(t, err)
}

func TestProtocol_WriteError(t *testing.T) {
	var out bytes.Buffer
	c := newConn(strings.NewReader(""), &out)

	c.writeError(7, -32600, "invalid request")

	var resp jsonRPCResponse
	require.NoError(t, json.Unmarshal(out.Bytes(), &resp))
	assert.Equal(t, "2.0", resp.JSONRPC)
	assert.Equal(t, 7, resp.ID)
	require.NotNil(t, resp.Error)
	assert.Equal(t, -32600, resp.Error.Code)
	assert.Equal(t, "invalid request", resp.Error.Message)
}

func TestProtocol_WriteResponse_MarshalError(t *testing.T) {
	// Channels cannot be marshaled to JSON, forcing the marshal-error path.
	var out bytes.Buffer
	c := newConn(strings.NewReader(""), &out)

	resp := jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      1,
		Result:  make(chan int), // not JSON-serializable
	}
	c.writeResponse(resp)

	// Should still produce valid JSON (the fallback error response).
	var got jsonRPCResponse
	require.NoError(t, json.Unmarshal(out.Bytes(), &got))
	assert.Equal(t, "2.0", got.JSONRPC)
	assert.Equal(t, 1, got.ID)
	require.NotNil(t, got.Error)
	assert.Equal(t, -32603, got.Error.Code)
}

func TestTextResult(t *testing.T) {
	r := textResult("hello world")
	assert.False(t, r.IsError)
	require.Len(t, r.Content, 1)
	assert.Equal(t, "text", r.Content[0].Type)
	assert.Equal(t, "hello world", r.Content[0].Text)
}

func TestErrorResult(t *testing.T) {
	r := errorResult("something broke")
	assert.True(t, r.IsError)
	require.Len(t, r.Content, 1)
	assert.Equal(t, "text", r.Content[0].Type)
	assert.Equal(t, "something broke", r.Content[0].Text)
}

// -------------------------------------------------------
// Server handleRequest tests
// -------------------------------------------------------

func TestServer_HandleInitialize(t *testing.T) {
	srv, _ := setupMCPServer(t)
	resp := sendRPC(t, srv, jsonRPCRequest{JSONRPC: "2.0", ID: 1, Method: "initialize"})

	assert.Equal(t, "2.0", resp.JSONRPC)
	assert.Equal(t, 1, resp.ID)
	assert.Nil(t, resp.Error)

	result, ok := resp.Result.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "2024-11-05", result["protocolVersion"])

	capabilities, ok := result["capabilities"].(map[string]any)
	require.True(t, ok)
	assert.Contains(t, capabilities, "tools")

	serverInfo, ok := result["serverInfo"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "cerberus", serverInfo["name"])
	assert.Equal(t, "0.5.0", serverInfo["version"])
}

func TestServer_HandleToolsList(t *testing.T) {
	srv, _ := setupMCPServer(t)
	resp := sendRPC(t, srv, jsonRPCRequest{JSONRPC: "2.0", ID: 2, Method: "tools/list"})

	assert.Nil(t, resp.Error)

	// Decode the result as listToolsResult.
	data, err := json.Marshal(resp.Result)
	require.NoError(t, err)
	var result listToolsResult
	require.NoError(t, json.Unmarshal(data, &result))
	assert.Len(t, result.Tools, 5)

	names := make(map[string]bool)
	for _, tool := range result.Tools {
		names[tool.Name] = true
		assert.NotEmpty(t, tool.Description)
		assert.NotNil(t, tool.InputSchema)
	}
	assert.True(t, names["cerberus_run"])
	assert.True(t, names["cerberus_status"])
	assert.True(t, names["cerberus_report"])
	assert.True(t, names["cerberus_decide"])
	assert.True(t, names["cerberus_cancel"])
}

func TestServer_HandleUnknownMethod(t *testing.T) {
	srv, _ := setupMCPServer(t)
	resp := sendRPC(t, srv, jsonRPCRequest{JSONRPC: "2.0", ID: 3, Method: "nonexistent/method"})

	assert.Equal(t, 3, resp.ID)
	require.NotNil(t, resp.Error)
	assert.Equal(t, -32601, resp.Error.Code)
	assert.Contains(t, resp.Error.Message, "method not found")
}

func TestServer_HandleNotificationInitialized(t *testing.T) {
	// notifications/initialized should produce no output (no response expected).
	srv, _ := setupMCPServer(t)
	data, err := json.Marshal(jsonRPCRequest{JSONRPC: "2.0", ID: 0, Method: "notifications/initialized"})
	require.NoError(t, err)
	in := strings.NewReader(string(data) + "\n")
	var out bytes.Buffer
	err = srv.handleConn(in, &out)
	require.NoError(t, err)
	assert.Empty(t, out.Bytes())
}

// -------------------------------------------------------
// tools/call handler tests
// -------------------------------------------------------

func TestServer_HandleToolCall_UnknownTool(t *testing.T) {
	srv, _ := setupMCPServer(t)
	params, _ := json.Marshal(map[string]any{
		"name":      "cerberus_bogus",
		"arguments": map[string]any{},
	})
	input := fmt.Sprintf(`{"jsonrpc":"2.0","id":10,"method":"tools/call","params":%s}`+"\n", string(params))
	in := strings.NewReader(input)
	var out bytes.Buffer
	err := srv.handleConn(in, &out)
	require.NoError(t, err)

	var resp jsonRPCResponse
	require.NoError(t, json.Unmarshal(out.Bytes(), &resp))
	assert.Nil(t, resp.Error) // MCP returns isError in result, not JSON-RPC error

	data, _ := json.Marshal(resp.Result)
	var result callToolResult
	require.NoError(t, json.Unmarshal(data, &result))
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content[0].Text, "unknown tool")
}

func TestServer_HandleToolCall_InvalidParams(t *testing.T) {
	srv, _ := setupMCPServer(t)
	// Params is a string instead of an object — should fail unmarshal.
	input := `{"jsonrpc":"2.0","id":11,"method":"tools/call","params":"not-an-object"}` + "\n"
	in := strings.NewReader(input)
	var out bytes.Buffer
	err := srv.handleConn(in, &out)
	require.NoError(t, err)

	var resp jsonRPCResponse
	require.NoError(t, json.Unmarshal(out.Bytes(), &resp))
	require.NotNil(t, resp.Error)
	assert.Equal(t, -32602, resp.Error.Code)
}

func TestServer_HandleToolCall_MissingParams(t *testing.T) {
	srv, _ := setupMCPServer(t)
	// No params field — the server gets empty toolCallParams, hits the
	// "unknown tool" branch (empty name), and returns an error result.
	input := `{"jsonrpc":"2.0","id":12,"method":"tools/call"}` + "\n"
	in := strings.NewReader(input)
	var out bytes.Buffer
	err := srv.handleConn(in, &out)
	require.NoError(t, err)

	var resp jsonRPCResponse
	require.NoError(t, json.Unmarshal(out.Bytes(), &resp))
	data, _ := json.Marshal(resp.Result)
	var result callToolResult
	require.NoError(t, json.Unmarshal(data, &result))
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content[0].Text, "unknown tool")
}

func TestServer_HandleStatus_MissingSessionID(t *testing.T) {
	srv, _ := setupMCPServer(t)
	params, _ := json.Marshal(map[string]any{
		"name":      "cerberus_status",
		"arguments": map[string]any{},
	})
	input := fmt.Sprintf(`{"jsonrpc":"2.0","id":20,"method":"tools/call","params":%s}`+"\n", string(params))
	in := strings.NewReader(input)
	var out bytes.Buffer
	err := srv.handleConn(in, &out)
	require.NoError(t, err)

	var resp jsonRPCResponse
	require.NoError(t, json.Unmarshal(out.Bytes(), &resp))
	data, _ := json.Marshal(resp.Result)
	var result callToolResult
	require.NoError(t, json.Unmarshal(data, &result))
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content[0].Text, "session_id is required")
}

func TestServer_HandleStatus_SessionNotFound(t *testing.T) {
	srv, _ := setupMCPServer(t)
	params, _ := json.Marshal(map[string]any{
		"name":      "cerberus_status",
		"arguments": map[string]any{"session_id": "nonexistent"},
	})
	input := fmt.Sprintf(`{"jsonrpc":"2.0","id":21,"method":"tools/call","params":%s}`+"\n", string(params))
	in := strings.NewReader(input)
	var out bytes.Buffer
	err := srv.handleConn(in, &out)
	require.NoError(t, err)

	var resp jsonRPCResponse
	require.NoError(t, json.Unmarshal(out.Bytes(), &resp))
	data, _ := json.Marshal(resp.Result)
	var result callToolResult
	require.NoError(t, json.Unmarshal(data, &result))
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content[0].Text, "session not found")
}

func TestServer_HandleStatus_CompletedSession(t *testing.T) {
	srv, s := setupMCPServer(t)
	ctx := context.Background()

	// Create a completed session directly in the store.
	sess, err := s.CreateSession(ctx, "run", "test goal", "project")
	require.NoError(t, err)
	require.NoError(t, s.UpdateSessionStatus(ctx, sess.ID, "completed"))

	params, _ := json.Marshal(map[string]any{
		"name":      "cerberus_status",
		"arguments": map[string]any{"session_id": sess.ID},
	})
	input := fmt.Sprintf(`{"jsonrpc":"2.0","id":22,"method":"tools/call","params":%s}`+"\n", string(params))
	in := strings.NewReader(input)
	var out bytes.Buffer
	err = srv.handleConn(in, &out)
	require.NoError(t, err)

	var resp jsonRPCResponse
	require.NoError(t, json.Unmarshal(out.Bytes(), &resp))
	data, _ := json.Marshal(resp.Result)
	var result callToolResult
	require.NoError(t, json.Unmarshal(data, &result))
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content[0].Text, "completed")
	assert.Contains(t, result.Content[0].Text, sess.ID)
}

func TestServer_HandleReport_MissingSessionID(t *testing.T) {
	srv, _ := setupMCPServer(t)
	params, _ := json.Marshal(map[string]any{
		"name":      "cerberus_report",
		"arguments": map[string]any{},
	})
	input := fmt.Sprintf(`{"jsonrpc":"2.0","id":30,"method":"tools/call","params":%s}`+"\n", string(params))
	in := strings.NewReader(input)
	var out bytes.Buffer
	err := srv.handleConn(in, &out)
	require.NoError(t, err)

	var resp jsonRPCResponse
	require.NoError(t, json.Unmarshal(out.Bytes(), &resp))
	data, _ := json.Marshal(resp.Result)
	var result callToolResult
	require.NoError(t, json.Unmarshal(data, &result))
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content[0].Text, "session_id is required")
}

func TestServer_HandleReport_SessionNotFound(t *testing.T) {
	srv, _ := setupMCPServer(t)
	params, _ := json.Marshal(map[string]any{
		"name":      "cerberus_report",
		"arguments": map[string]any{"session_id": "does-not-exist"},
	})
	input := fmt.Sprintf(`{"jsonrpc":"2.0","id":31,"method":"tools/call","params":%s}`+"\n", string(params))
	in := strings.NewReader(input)
	var out bytes.Buffer
	err := srv.handleConn(in, &out)
	require.NoError(t, err)

	var resp jsonRPCResponse
	require.NoError(t, json.Unmarshal(out.Bytes(), &resp))
	data, _ := json.Marshal(resp.Result)
	var result callToolResult
	require.NoError(t, json.Unmarshal(data, &result))
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content[0].Text, "session not found")
}

func TestServer_HandleReport_CompletedSession(t *testing.T) {
	srv, s := setupMCPServer(t)
	ctx := context.Background()

	sess, err := s.CreateSession(ctx, "run", "test report goal", "project")
	require.NoError(t, err)
	require.NoError(t, s.UpdateSessionStatus(ctx, sess.ID, "completed"))

	params, _ := json.Marshal(map[string]any{
		"name":      "cerberus_report",
		"arguments": map[string]any{"session_id": sess.ID},
	})
	input := fmt.Sprintf(`{"jsonrpc":"2.0","id":32,"method":"tools/call","params":%s}`+"\n", string(params))
	in := strings.NewReader(input)
	var out bytes.Buffer
	err = srv.handleConn(in, &out)
	require.NoError(t, err)

	var resp jsonRPCResponse
	require.NoError(t, json.Unmarshal(out.Bytes(), &resp))
	data, _ := json.Marshal(resp.Result)
	var result callToolResult
	require.NoError(t, json.Unmarshal(data, &result))
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content[0].Text, sess.ID)
	assert.Contains(t, result.Content[0].Text, "test report goal")
}

func TestServer_HandleDecide_MissingRequiredParams(t *testing.T) {
	srv, _ := setupMCPServer(t)

	// Missing action.
	params, _ := json.Marshal(map[string]any{
		"name":      "cerberus_decide",
		"arguments": map[string]any{"session_id": "some-id"},
	})
	input := fmt.Sprintf(`{"jsonrpc":"2.0","id":40,"method":"tools/call","params":%s}`+"\n", string(params))
	in := strings.NewReader(input)
	var out bytes.Buffer
	err := srv.handleConn(in, &out)
	require.NoError(t, err)

	var resp jsonRPCResponse
	require.NoError(t, json.Unmarshal(out.Bytes(), &resp))
	data, _ := json.Marshal(resp.Result)
	var result callToolResult
	require.NoError(t, json.Unmarshal(data, &result))
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content[0].Text, "session_id and action are required")
}

func TestServer_HandleDecide_InvalidAction(t *testing.T) {
	srv, _ := setupMCPServer(t)
	params, _ := json.Marshal(map[string]any{
		"name":      "cerberus_decide",
		"arguments": map[string]any{"session_id": "some-id", "action": "explode"},
	})
	input := fmt.Sprintf(`{"jsonrpc":"2.0","id":41,"method":"tools/call","params":%s}`+"\n", string(params))
	in := strings.NewReader(input)
	var out bytes.Buffer
	err := srv.handleConn(in, &out)
	require.NoError(t, err)

	var resp jsonRPCResponse
	require.NoError(t, json.Unmarshal(out.Bytes(), &resp))
	data, _ := json.Marshal(resp.Result)
	var result callToolResult
	require.NoError(t, json.Unmarshal(data, &result))
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content[0].Text, "invalid action")
	assert.Contains(t, result.Content[0].Text, "explode")
}

func TestServer_HandleDecide_SessionNotFound(t *testing.T) {
	srv, _ := setupMCPServer(t)
	params, _ := json.Marshal(map[string]any{
		"name":      "cerberus_decide",
		"arguments": map[string]any{"session_id": "nonexistent", "action": "continue"},
	})
	input := fmt.Sprintf(`{"jsonrpc":"2.0","id":42,"method":"tools/call","params":%s}`+"\n", string(params))
	in := strings.NewReader(input)
	var out bytes.Buffer
	err := srv.handleConn(in, &out)
	require.NoError(t, err)

	var resp jsonRPCResponse
	require.NoError(t, json.Unmarshal(out.Bytes(), &resp))
	data, _ := json.Marshal(resp.Result)
	var result callToolResult
	require.NoError(t, json.Unmarshal(data, &result))
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content[0].Text, "session not found or already completed")
}

func TestServer_HandleDecide_Success(t *testing.T) {
	srv, _ := setupMCPServer(t)
	gate := NewMCPGate()

	// Manually register a running session with an MCPGate.
	srv.mu.Lock()
	srv.sessions["test-decide-session"] = &runningSession{
		progress: make(chan SessionProgress, 16),
		cancel:   func() {},
		gate:     gate,
	}
	srv.mu.Unlock()
	defer func() {
		srv.mu.Lock()
		delete(srv.sessions, "test-decide-session")
		srv.mu.Unlock()
	}()

	// Block gate.Check in a goroutine so we can test SendDecision via the tool.
	done := make(chan escalation.Decision, 1)
	go func() {
		d := gate.Check(context.Background(), escalation.Event{Type: "budget_warning", Message: "80%"})
		done <- d
	}()

	// Wait for the pending event to be registered.
	require.Eventually(t, func() bool {
		return gate.PendingEvent() != nil
	}, time.Second, 10*time.Millisecond)

	params, _ := json.Marshal(map[string]any{
		"name":      "cerberus_decide",
		"arguments": map[string]any{"session_id": "test-decide-session", "action": "abort"},
	})
	input := fmt.Sprintf(`{"jsonrpc":"2.0","id":43,"method":"tools/call","params":%s}`+"\n", string(params))
	in := strings.NewReader(input)
	var out bytes.Buffer
	err := srv.handleConn(in, &out)
	require.NoError(t, err)

	var resp jsonRPCResponse
	require.NoError(t, json.Unmarshal(out.Bytes(), &resp))
	data, _ := json.Marshal(resp.Result)
	var result callToolResult
	require.NoError(t, json.Unmarshal(data, &result))
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content[0].Text, "decision_acknowledged")

	// Verify the decision actually reached the gate.
	select {
	case d := <-done:
		assert.Equal(t, "abort", d.Action)
	case <-time.After(time.Second):
		t.Fatal("decision was not received by gate")
	}
}

func TestServer_HandleCancel_MissingSessionID(t *testing.T) {
	srv, _ := setupMCPServer(t)
	params, _ := json.Marshal(map[string]any{
		"name":      "cerberus_cancel",
		"arguments": map[string]any{},
	})
	input := fmt.Sprintf(`{"jsonrpc":"2.0","id":50,"method":"tools/call","params":%s}`+"\n", string(params))
	in := strings.NewReader(input)
	var out bytes.Buffer
	err := srv.handleConn(in, &out)
	require.NoError(t, err)

	var resp jsonRPCResponse
	require.NoError(t, json.Unmarshal(out.Bytes(), &resp))
	data, _ := json.Marshal(resp.Result)
	var result callToolResult
	require.NoError(t, json.Unmarshal(data, &result))
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content[0].Text, "session_id is required")
}

func TestServer_HandleCancel_NonexistentSession(t *testing.T) {
	srv, _ := setupMCPServer(t)
	params, _ := json.Marshal(map[string]any{
		"name":      "cerberus_cancel",
		"arguments": map[string]any{"session_id": "nonexistent"},
	})
	input := fmt.Sprintf(`{"jsonrpc":"2.0","id":51,"method":"tools/call","params":%s}`+"\n", string(params))
	in := strings.NewReader(input)
	var out bytes.Buffer
	err := srv.handleConn(in, &out)
	require.NoError(t, err)

	var resp jsonRPCResponse
	require.NoError(t, json.Unmarshal(out.Bytes(), &resp))
	data, _ := json.Marshal(resp.Result)
	var result callToolResult
	require.NoError(t, json.Unmarshal(data, &result))
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content[0].Text, "session not found or already completed")
}

func TestServer_HandleCancel_ActiveSession(t *testing.T) {
	srv, _ := setupMCPServer(t)

	cancelled := false
	srv.mu.Lock()
	srv.sessions["cancel-me"] = &runningSession{
		progress: make(chan SessionProgress, 16),
		cancel: func() {
			cancelled = true
		},
		gate: NewMCPGate(),
	}
	srv.mu.Unlock()

	params, _ := json.Marshal(map[string]any{
		"name":      "cerberus_cancel",
		"arguments": map[string]any{"session_id": "cancel-me"},
	})
	input := fmt.Sprintf(`{"jsonrpc":"2.0","id":52,"method":"tools/call","params":%s}`+"\n", string(params))
	in := strings.NewReader(input)
	var out bytes.Buffer
	err := srv.handleConn(in, &out)
	require.NoError(t, err)

	var resp jsonRPCResponse
	require.NoError(t, json.Unmarshal(out.Bytes(), &resp))
	data, _ := json.Marshal(resp.Result)
	var result callToolResult
	require.NoError(t, json.Unmarshal(data, &result))
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content[0].Text, "cancelled")
	assert.True(t, cancelled, "cancel function should have been called")

	// Session should be removed from the map.
	srv.mu.Lock()
	_, exists := srv.sessions["cancel-me"]
	srv.mu.Unlock()
	assert.False(t, exists, "session should be removed after cancel")
}

func TestServer_HandleRun_MissingParams(t *testing.T) {
	srv, _ := setupMCPServer(t)

	// Missing goal.
	params, _ := json.Marshal(map[string]any{
		"name":      "cerberus_run",
		"arguments": map[string]any{"url": "http://localhost:3000"},
	})
	input := fmt.Sprintf(`{"jsonrpc":"2.0","id":60,"method":"tools/call","params":%s}`+"\n", string(params))
	in := strings.NewReader(input)
	var out bytes.Buffer
	err := srv.handleConn(in, &out)
	require.NoError(t, err)

	var resp jsonRPCResponse
	require.NoError(t, json.Unmarshal(out.Bytes(), &resp))
	data, _ := json.Marshal(resp.Result)
	var result callToolResult
	require.NoError(t, json.Unmarshal(data, &result))
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content[0].Text, "goal and url are required")
}

func TestServer_HandleRun_MissingURL(t *testing.T) {
	srv, _ := setupMCPServer(t)

	params, _ := json.Marshal(map[string]any{
		"name":      "cerberus_run",
		"arguments": map[string]any{"goal": "test something"},
	})
	input := fmt.Sprintf(`{"jsonrpc":"2.0","id":61,"method":"tools/call","params":%s}`+"\n", string(params))
	in := strings.NewReader(input)
	var out bytes.Buffer
	err := srv.handleConn(in, &out)
	require.NoError(t, err)

	var resp jsonRPCResponse
	require.NoError(t, json.Unmarshal(out.Bytes(), &resp))
	data, _ := json.Marshal(resp.Result)
	var result callToolResult
	require.NoError(t, json.Unmarshal(data, &result))
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content[0].Text, "goal and url are required")
}

func TestServer_HandleRun_EmptyGoalAndURL(t *testing.T) {
	srv, _ := setupMCPServer(t)

	params, _ := json.Marshal(map[string]any{
		"name":      "cerberus_run",
		"arguments": map[string]any{"goal": "", "url": ""},
	})
	input := fmt.Sprintf(`{"jsonrpc":"2.0","id":62,"method":"tools/call","params":%s}`+"\n", string(params))
	in := strings.NewReader(input)
	var out bytes.Buffer
	err := srv.handleConn(in, &out)
	require.NoError(t, err)

	var resp jsonRPCResponse
	require.NoError(t, json.Unmarshal(out.Bytes(), &resp))
	data, _ := json.Marshal(resp.Result)
	var result callToolResult
	require.NoError(t, json.Unmarshal(data, &result))
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content[0].Text, "goal and url are required")
}

// -------------------------------------------------------
// Status with pending escalation event
// -------------------------------------------------------

func TestServer_HandleStatus_PendingEvent(t *testing.T) {
	srv, _ := setupMCPServer(t)
	gate := NewMCPGate()

	srv.mu.Lock()
	srv.sessions["escalating-session"] = &runningSession{
		progress: make(chan SessionProgress, 16),
		cancel:   func() {},
		gate:     gate,
	}
	srv.mu.Unlock()
	defer func() {
		srv.mu.Lock()
		delete(srv.sessions, "escalating-session")
		srv.mu.Unlock()
	}()

	// Start a Check to set a pending event.
	go gate.Check(context.Background(), escalation.Event{
		Type:    "destructive_risk",
		Message: "about to delete data",
		Data:    map[string]any{"table": "users"},
	})

	// Wait for pending event to appear.
	require.Eventually(t, func() bool {
		return gate.PendingEvent() != nil
	}, time.Second, 10*time.Millisecond)

	params, _ := json.Marshal(map[string]any{
		"name":      "cerberus_status",
		"arguments": map[string]any{"session_id": "escalating-session"},
	})
	input := fmt.Sprintf(`{"jsonrpc":"2.0","id":70,"method":"tools/call","params":%s}`+"\n", string(params))
	in := strings.NewReader(input)
	var out bytes.Buffer
	err := srv.handleConn(in, &out)
	require.NoError(t, err)

	var resp jsonRPCResponse
	require.NoError(t, json.Unmarshal(out.Bytes(), &resp))
	data, _ := json.Marshal(resp.Result)
	var result callToolResult
	require.NoError(t, json.Unmarshal(data, &result))
	assert.False(t, result.IsError)

	var progress SessionProgress
	require.NoError(t, json.Unmarshal([]byte(result.Content[0].Text), &progress))
	assert.Equal(t, "pending_decision", progress.Status)
	require.NotNil(t, progress.Event)
	assert.Equal(t, "destructive_risk", progress.Event.Type)
	assert.Equal(t, "about to delete data", progress.Event.Message)

	// Clean up the blocked gate.
	gate.SendDecision(escalation.EscalationContinue)
}

// -------------------------------------------------------
// Decide with payload
// -------------------------------------------------------

func TestServer_HandleDecide_WithPayload(t *testing.T) {
	srv, _ := setupMCPServer(t)
	gate := NewMCPGate()

	srv.mu.Lock()
	srv.sessions["payload-session"] = &runningSession{
		progress: make(chan SessionProgress, 16),
		cancel:   func() {},
		gate:     gate,
	}
	srv.mu.Unlock()
	defer func() {
		srv.mu.Lock()
		delete(srv.sessions, "payload-session")
		srv.mu.Unlock()
	}()

	done := make(chan escalation.Decision, 1)
	go func() {
		d := gate.Check(context.Background(), escalation.Event{Type: "target_unreachable", Message: "host down"})
		done <- d
	}()

	require.Eventually(t, func() bool {
		return gate.PendingEvent() != nil
	}, time.Second, 10*time.Millisecond)

	params, _ := json.Marshal(map[string]any{
		"name": "cerberus_decide",
		"arguments": map[string]any{
			"session_id": "payload-session",
			"action":     "continue",
			"payload":    "http://backup:3000",
		},
	})
	input := fmt.Sprintf(`{"jsonrpc":"2.0","id":80,"method":"tools/call","params":%s}`+"\n", string(params))
	in := strings.NewReader(input)
	var out bytes.Buffer
	err := srv.handleConn(in, &out)
	require.NoError(t, err)

	var resp jsonRPCResponse
	require.NoError(t, json.Unmarshal(out.Bytes(), &resp))
	data, _ := json.Marshal(resp.Result)
	var result callToolResult
	require.NoError(t, json.Unmarshal(data, &result))
	assert.False(t, result.IsError)

	select {
	case d := <-done:
		assert.Equal(t, "continue", d.Action)
		assert.Equal(t, "http://backup:3000", d.Payload)
	case <-time.After(time.Second):
		t.Fatal("decision not received")
	}
}

// -------------------------------------------------------
// Serve lifecycle
// -------------------------------------------------------

func TestServer_Serve_ContextCancellation(t *testing.T) {
	srv, _ := setupMCPServer(t)
	ctx, cancel := context.WithCancel(context.Background())

	// Provide data followed by EOF, then cancel before the next read completes.
	// The Serve loop reads the first line, writes response, then loops back.
	// The second read hits EOF — but we want to test ctx cancellation path,
	// so we need the second read to block. Use a pipe for the second read.
	//
	// Simpler approach: write one line + close write end (causing EOF on read),
	// then cancel context. The loop will see EOF before ctx.Done, so test
	// the alternate: write data via pipe and cancel while blocking on next read.
	pr, pw := io.Pipe()
	var out bytes.Buffer

	done := make(chan error, 1)
	go func() {
		done <- srv.Serve(ctx, pr, &out)
	}()

	// Write one request but don't close the pipe — next read blocks.
	_, _ = pw.Write([]byte(`{"jsonrpc":"2.0","id":99,"method":"initialize"}` + "\n"))

	// Give time for the request to be processed and the loop to re-enter readRequest.
	time.Sleep(100 * time.Millisecond)

	// Now cancel — but readRequest is blocking on ReadBytes.
	// The Serve loop's select won't fire because readRequest hasn't returned.
	// Cancel the context; the select at the top of the next iteration will catch it,
	// but we need readRequest to return first. Close the pipe to make it return EOF.
	cancel()
	_ = pw.Close()

	select {
	case err := <-done:
		// Either ctx.Canceled or nil (EOF was read first) — both are acceptable.
		if err != nil {
			assert.Equal(t, context.Canceled, err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve should exit")
	}
}

// -------------------------------------------------------
// RecoverOrphanSessions edge cases
// -------------------------------------------------------

func TestServer_RecoverOrphanSessions_NoOrphans(t *testing.T) {
	srv, s := setupMCPServer(t)
	ctx := context.Background()

	// Create a completed session (not running).
	sess, err := s.CreateSession(ctx, "run", "goal", "project")
	require.NoError(t, err)
	require.NoError(t, s.UpdateSessionStatus(ctx, sess.ID, "completed"))

	srv.RecoverOrphanSessions(ctx)

	recovered, err := s.GetSession(ctx, sess.ID)
	require.NoError(t, err)
	assert.Equal(t, "completed", recovered.Status, "completed sessions should not be changed")
}

func TestServer_RecoverOrphanSessions_MultipleRunning(t *testing.T) {
	srv, s := setupMCPServer(t)
	ctx := context.Background()

	var ids []string
	for i := 0; i < 3; i++ {
		sess, err := s.CreateSession(ctx, "run", fmt.Sprintf("goal-%d", i), "project")
		require.NoError(t, err)
		require.NoError(t, s.UpdateSessionStatus(ctx, sess.ID, "running"))
		ids = append(ids, sess.ID)
	}

	srv.RecoverOrphanSessions(ctx)

	for _, id := range ids {
		recovered, err := s.GetSession(ctx, id)
		require.NoError(t, err)
		assert.Equal(t, "interrupted", recovered.Status)
	}
}

// -------------------------------------------------------
// ListTools tool definitions validation
// -------------------------------------------------------

func TestServer_ListTools_SchemasHaveRequired(t *testing.T) {
	srv, _ := setupMCPServer(t)
	tools := srv.listTools()

	for _, tool := range tools {
		schema, ok := tool.InputSchema.(map[string]any)
		require.True(t, ok, "tool %s should have object schema", tool.Name)
		assert.Equal(t, "object", schema["type"])

		required, ok := schema["required"].([]string)
		require.True(t, ok, "tool %s should have required fields", tool.Name)
		assert.NotEmpty(t, required, "tool %s should have at least one required field", tool.Name)

		props, ok := schema["properties"].(map[string]any)
		require.True(t, ok, "tool %s should have properties", tool.Name)

		// Every required field should exist in properties.
		for _, field := range required {
			assert.Contains(t, props, field, "tool %s: required field %s should be in properties", tool.Name, field)
		}
	}
}

// -------------------------------------------------------
// Phase 1: Streaming notifications
// -------------------------------------------------------

func TestProtocol_WriteNotification_Format(t *testing.T) {
	var out bytes.Buffer
	c := newConn(strings.NewReader(""), &out)

	params := map[string]any{
		"session_id": "abc-123",
		"status":     "running",
		"phase":      "scout",
	}
	c.writeNotification("notifications/progress", params)

	// Parse the output — should be valid JSON without "id" field.
	var msg map[string]any
	require.NoError(t, json.Unmarshal(out.Bytes(), &msg))
	assert.Equal(t, "2.0", msg["jsonrpc"])
	assert.Equal(t, "notifications/progress", msg["method"])
	assert.Nil(t, msg["id"], "notification must not have an id field")

	p, ok := msg["params"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "abc-123", p["session_id"])
	assert.Equal(t, "running", p["status"])
}

func TestProtocol_WriteNotification_NilParams(t *testing.T) {
	var out bytes.Buffer
	c := newConn(strings.NewReader(""), &out)

	c.writeNotification("some/method", nil)

	var msg map[string]any
	require.NoError(t, json.Unmarshal(out.Bytes(), &msg))
	assert.Equal(t, "2.0", msg["jsonrpc"])
	assert.Equal(t, "some/method", msg["method"])
	// params should be absent when nil.
	_, hasParams := msg["params"]
	assert.False(t, hasParams, "params should be absent when nil")
}

func TestProtocol_ConcurrentWrites(t *testing.T) {
	var out bytes.Buffer
	c := newConn(strings.NewReader(""), &out)

	// Hammer writeResponse and writeNotification from multiple goroutines.
	// If the mutex is missing, this will likely panic or produce corrupt JSON.
	const n = 100
	var wg sync.WaitGroup
	wg.Add(n)
	for i := range n {
		go func(id int) {
			defer wg.Done()
			if id%2 == 0 {
				c.writeResponse(jsonRPCResponse{
					JSONRPC: "2.0", ID: id, Result: "ok",
				})
			} else {
				c.writeNotification("test/event", map[string]any{"id": id})
			}
		}(i)
	}
	wg.Wait()

	// Every line in the output should be valid JSON.
	lines := bytes.Split(bytes.TrimSpace(out.Bytes()), []byte{'\n'})
	assert.Len(t, lines, n)
	for _, line := range lines {
		var msg map[string]any
		assert.NoError(t, json.Unmarshal(line, &msg), "line should be valid JSON: %s", string(line))
	}
}

func TestServer_HandleInitialize_NotificationsCapability(t *testing.T) {
	srv, _ := setupMCPServer(t)
	resp := sendRPC(t, srv, jsonRPCRequest{JSONRPC: "2.0", ID: 1, Method: "initialize"})

	result, ok := resp.Result.(map[string]any)
	require.True(t, ok)
	capabilities, ok := result["capabilities"].(map[string]any)
	require.True(t, ok)
	assert.Contains(t, capabilities, "notifications", "should declare notifications capability")
}
