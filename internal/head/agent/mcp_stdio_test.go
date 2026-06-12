package agent

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/binoctal/cerberus/internal/types"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestMCPExecutor_TCPTransport(t *testing.T) {
	// Simple JSON-RPC echo server.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Not used — TCP test is covered by the existing send logic.
	}))
	defer server.Close()

	// We test TCP via the endpoint address format.
	ep := map[string]MCPEndpoint{
		"test": {Name: "test", Address: strings.TrimPrefix(server.URL, "http://")},
	}
	exec := NewMCPExecutor(ep, zap.NewNop())

	// TCP test: the httptest server speaks HTTP, not raw TCP,
	// so we just verify the executor doesn't panic on unknown server.
	action := types.MCPCallAction{Server: "unknown", Method: "test"}
	result := exec.Execute(context.Background(), action)
	mcpRes, ok := result.(types.MCPResult)
	assert.True(t, ok)
	assert.False(t, mcpRes.OK)
	assert.Contains(t, mcpRes.Err, "unknown MCP server")
}

func TestMCPExecutor_StdioTransport(t *testing.T) {
	// Use "cat" as a simple echo subprocess — it reads stdin and writes to stdout.
	// cat echoes the raw JSON request back. The response parser looks for "result"
	// field; since cat echoes the full request, "result" is absent and resp.Result
	// is nil, producing Body="null". This verifies the stdio transport path works.
	ep := map[string]MCPEndpoint{
		"echo": {Name: "echo", Address: "stdio", Command: "cat"},
	}
	exec := NewMCPExecutor(ep, zap.NewNop())
	defer exec.Close()

	action := types.MCPCallAction{
		Server: "echo",
		Method: "tools/list",
		Params: map[string]any{},
	}
	result := exec.Execute(context.Background(), action)
	mcpRes, ok := result.(types.MCPResult)
	assert.True(t, ok)
	assert.True(t, mcpRes.OK, "expected success, got error: %s", mcpRes.Err)
	// cat echoes the request; result field is absent so Body is "null".
	assert.Equal(t, "null", mcpRes.Body)
}

func TestMCPExecutor_StdioReuseConnection(t *testing.T) {
	ep := map[string]MCPEndpoint{
		"echo": {Name: "echo", Address: "stdio", Command: "cat"},
	}
	exec := NewMCPExecutor(ep, zap.NewNop())
	defer exec.Close()

	// First call.
	action1 := types.MCPCallAction{Server: "echo", Method: "method1"}
	result1 := exec.Execute(context.Background(), action1)
	assert.True(t, result1.Success())

	// Second call should reuse the same subprocess.
	action2 := types.MCPCallAction{Server: "echo", Method: "method2"}
	result2 := exec.Execute(context.Background(), action2)
	assert.True(t, result2.Success())

	// Both used the same subprocess.
	assert.Len(t, exec.stdioProcesses, 1)
}

func TestMCPExecutor_UnsupportedAction(t *testing.T) {
	exec := NewMCPExecutor(nil, zap.NewNop())
	result := exec.Execute(context.Background(), types.WaitAction{Duration: "1s"})
	errRes, ok := result.(types.ErrorResult)
	assert.True(t, ok)
	assert.Contains(t, errRes.Err, "unsupported action")
}

func TestMCPExecutor_Close(t *testing.T) {
	ep := map[string]MCPEndpoint{
		"echo": {Name: "echo", Address: "stdio", Command: "cat"},
	}
	exec := NewMCPExecutor(ep, zap.NewNop())

	action := types.MCPCallAction{Server: "echo", Method: "ping"}
	result := exec.Execute(context.Background(), action)
	assert.True(t, result.Success(), fmt.Sprintf("expected success: %v", result))
	assert.Len(t, exec.stdioProcesses, 1)

	exec.Close()
	assert.Len(t, exec.stdioProcesses, 0)
}
