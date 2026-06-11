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

func (c *conn) writeResponse(resp jsonRPCResponse) {
	data, err := json.Marshal(resp)
	if err != nil {
		return
	}
	data = append(data, '\n')
	_, _ = c.writer.Write(data)
}

func (c *conn) writeError(id int, code int, msg string) {
	c.writeResponse(jsonRPCResponse{
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
