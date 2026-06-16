package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/binoctal/cerberus/internal/types"
)

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

// send transmits the JSON-RPC request and reads a response.
// Uses TCP for address "host:port" and stdio subprocess for address "stdio".
func (e *MCPExecutor) send(ctx context.Context, ep MCPEndpoint, body []byte) ([]byte, error) {
	if ep.Address == "stdio" {
		return e.sendStdio(ctx, ep, body)
	}
	return e.sendTCP(ctx, ep, body)
}
