// internal/mcp/protocol.go
// Package mcp implements the Model Context Protocol server for Cerberus.
package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// JSON-RPC 2.0 types.

type jsonRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      int           `json:"id"`
	Result  any           `json:"result,omitempty"`
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
	writer *bufio.Writer
	mu     sync.Mutex // protects writer for concurrent writes
}

func newConn(r io.Reader, w io.Writer) *conn {
	return &conn{reader: bufio.NewReader(r), writer: bufio.NewWriter(w)}
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

func (c *conn) writeResponse(resp jsonRPCResponse) {
	c.mu.Lock()
	defer c.mu.Unlock()
	data, err := json.Marshal(resp)
	if err != nil {
		// Best effort: send a generic error response so the client doesn't hang.
		data, _ = json.Marshal(jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      resp.ID,
			Error:   &jsonRPCError{Code: -32603, Message: "internal error"},
		})
	}
	data = append(data, '\n')
	_, _ = c.writer.Write(data)
	_ = c.writer.Flush()
}

func (c *conn) writeError(id int, code int, msg string) {
	c.writeResponse(jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &jsonRPCError{Code: code, Message: msg},
	})
}

// writeNotification sends a JSON-RPC notification (no id field) to the host.
func (c *conn) writeNotification(method string, params any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	msg := map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
	}
	if params != nil {
		msg["params"] = params
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	data = append(data, '\n')
	_, _ = c.writer.Write(data)
	_ = c.writer.Flush()
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
