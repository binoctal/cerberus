package agent

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"time"

	"go.uber.org/zap"
)

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

	timeout := e.readTimeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	select {
	case <-time.After(timeout):
		// The reader goroutine is still blocked on conn.stdout. Close the conn
		// so that goroutine unblocks (pipe close → ReadBytes returns) and evict
		// it so the next call dials a fresh subprocess instead of racing two
		// readers on the same bufio.Reader.
		e.closeStdioLocked(ep.Name)
		return nil, fmt.Errorf("mcp stdio: read timeout")
	case r := <-ch:
		return r.data, r.err
	}
}

// closeStdioLocked kills the subprocess behind name, waits for it, and removes
// the conn from the cache. Caller must hold e.mu. Closing stdin unblocks any
// goroutine still parked on conn.stdout.ReadBytes.
func (e *MCPExecutor) closeStdioLocked(name string) {
	conn, ok := e.stdioProcesses[name]
	if !ok {
		return
	}
	_ = conn.stdin.Close()
	if conn.cmd.Process != nil {
		if err := conn.cmd.Process.Kill(); err != nil {
			e.logger.Warn("failed to kill MCP stdio process",
				zap.String("server", name),
				zap.Error(err),
			)
		}
	}
	_ = conn.cmd.Wait()
	delete(e.stdioProcesses, name)
}

// Close terminates all stdio subprocesses.
func (e *MCPExecutor) Close() {
	e.mu.Lock()
	defer e.mu.Unlock()

	for name := range e.stdioProcesses {
		e.closeStdioLocked(name)
	}
}
