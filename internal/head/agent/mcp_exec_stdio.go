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
