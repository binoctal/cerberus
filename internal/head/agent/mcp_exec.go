package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/binoctal/cerberus/internal/types"
	"go.uber.org/zap"
)

// MCPEndpoint describes a remote MCP server.
type MCPEndpoint struct {
	Name    string
	Address string // e.g. "localhost:8080"
}

// MCPExecutor calls MCP servers via JSON-RPC 2.0.
type MCPExecutor struct {
	endpoints map[string]MCPEndpoint
	logger    *zap.Logger
}

// NewMCPExecutor creates an MCP executor with named endpoints.
func NewMCPExecutor(endpoints map[string]MCPEndpoint, logger *zap.Logger) *MCPExecutor {
	if endpoints == nil {
		endpoints = make(map[string]MCPEndpoint)
	}
	return &MCPExecutor{endpoints: endpoints, logger: logger}
}

// Execute dispatches MCP calls.
func (e *MCPExecutor) Execute(ctx context.Context, action types.TypedAction) types.ExecutorResult {
	start := time.Now()
	a, ok := action.(types.MCPCallAction)
	if !ok {
		return types.ErrorResult{Err: fmt.Sprintf("mcp executor: unsupported action %T", action)}
	}

	ep, found := e.endpoints[a.Server]
	if !found {
		return types.MCPResult{OK: false, Err: fmt.Sprintf("unknown MCP server: %s", a.Server), Latency: time.Since(start)}
	}

	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      start.UnixNano(),
		"method":  a.Method,
		"params":  a.Params,
	}
	reqBody, _ := json.Marshal(req)

	respBody, err := e.send(ctx, ep, reqBody)
	if err != nil {
		return types.MCPResult{OK: false, Err: err.Error(), Latency: time.Since(start)}
	}

	var resp struct {
		Result any `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return types.MCPResult{OK: false, Err: err.Error(), Latency: time.Since(start)}
	}
	if resp.Error != nil {
		return types.MCPResult{OK: false, Err: resp.Error.Message, Latency: time.Since(start)}
	}

	resultJSON, _ := json.Marshal(resp.Result)
	return types.MCPResult{OK: true, Body: string(resultJSON), Latency: time.Since(start)}
}

func (e *MCPExecutor) send(ctx context.Context, ep MCPEndpoint, body []byte) ([]byte, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", ep.Address)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(10 * time.Second))
	body = append(body, '\n')
	if _, err := conn.Write(body); err != nil {
		return nil, err
	}
	reader := bufio.NewReader(conn)
	return reader.ReadBytes('\n')
}
