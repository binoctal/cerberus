package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os/exec"
	"sync"
	"time"

	"github.com/binoctal/cerberus/internal/types"
	"go.uber.org/zap"
)

// MCPEndpoint describes a remote MCP server.
type MCPEndpoint struct {
	Name    string   // Server name for routing
	Address string   // "host:port" for TCP, "stdio" for subprocess
	Command string   // Binary path (stdio only)
	Args    []string // Binary arguments (stdio only)
}

// stdioConn holds a persistent subprocess connection for stdio MCP transport.
type stdioConn struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
}

// MCPExecutor calls MCP servers via JSON-RPC 2.0.
type MCPExecutor struct {
	endpoints      map[string]MCPEndpoint
	stdioProcesses map[string]*stdioConn
	mu             sync.Mutex
	logger         *zap.Logger
}

// NewMCPExecutor creates an MCP executor with named endpoints.
func NewMCPExecutor(endpoints map[string]MCPEndpoint, logger *zap.Logger) *MCPExecutor {
	if endpoints == nil {
		endpoints = make(map[string]MCPEndpoint)
	}
	return &MCPExecutor{
		endpoints:      endpoints,
		stdioProcesses: make(map[string]*stdioConn),
		logger:         logger,
	}
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

// send transmits the JSON-RPC request and reads a response.
// Uses TCP for address "host:port" and stdio subprocess for address "stdio".
func (e *MCPExecutor) send(ctx context.Context, ep MCPEndpoint, body []byte) ([]byte, error) {
	if ep.Address == "stdio" {
		return e.sendStdio(ctx, ep, body)
	}
	return e.sendTCP(ctx, ep, body)
}

// sendTCP transmits over a TCP connection (original behavior).
func (e *MCPExecutor) sendTCP(ctx context.Context, ep MCPEndpoint, body []byte) ([]byte, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", ep.Address)
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	body = append(body, '\n')
	if _, err := conn.Write(body); err != nil {
		return nil, err
	}
	reader := bufio.NewReader(conn)
	return reader.ReadBytes('\n')
}

// sendStdio transmits over a subprocess stdin/stdout pipe.
// Reuses an existing subprocess if one is already running for this server.
func (e *MCPExecutor) sendStdio(ctx context.Context, ep MCPEndpoint, body []byte) ([]byte, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	conn, ok := e.stdioProcesses[ep.Name]
	if !ok {
		// Start the subprocess.
		cmd := exec.CommandContext(ctx, ep.Command, ep.Args...)
		stdinPipe, err := cmd.StdinPipe()
		if err != nil {
			return nil, fmt.Errorf("mcp stdio: stdin pipe: %w", err)
		}
		stdoutPipe, err := cmd.StdoutPipe()
		if err != nil {
			return nil, fmt.Errorf("mcp stdio: stdout pipe: %w", err)
		}

		if err := cmd.Start(); err != nil {
			return nil, fmt.Errorf("mcp stdio: start %s: %w", ep.Command, err)
		}

		conn = &stdioConn{
			cmd:    cmd,
			stdin:  stdinPipe,
			stdout: bufio.NewReader(stdoutPipe),
		}
		e.stdioProcesses[ep.Name] = conn

		e.logger.Info("started MCP stdio subprocess",
			zap.String("server", ep.Name),
			zap.String("command", ep.Command),
			zap.Int("pid", cmd.Process.Pid),
		)
	}

	// Write request + newline.
	body = append(body, '\n')
	if _, err := conn.stdin.Write(body); err != nil {
		return nil, fmt.Errorf("mcp stdio: write: %w", err)
	}

	// Read newline-delimited response with timeout.
	type readResult struct {
		data []byte
		err  error
	}
	ch := make(chan readResult, 1)
	go func() {
		data, err := conn.stdout.ReadBytes('\n')
		ch <- readResult{data: data, err: err}
	}()

	select {
	case <-time.After(10 * time.Second):
		return nil, fmt.Errorf("mcp stdio: read timeout")
	case r := <-ch:
		return r.data, r.err
	}
}

// Close terminates all stdio subprocesses.
func (e *MCPExecutor) Close() {
	e.mu.Lock()
	defer e.mu.Unlock()

	for name, conn := range e.stdioProcesses {
		_ = conn.stdin.Close()
		if err := conn.cmd.Process.Kill(); err != nil {
			e.logger.Warn("failed to kill MCP stdio process",
				zap.String("server", name),
				zap.Error(err),
			)
		}
		_ = conn.cmd.Wait()
		delete(e.stdioProcesses, name)
	}
}
