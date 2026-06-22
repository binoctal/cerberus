package agent

import (
	"bufio"
	"io"
	"os/exec"
	"sync"
	"time"

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
	readTimeout    time.Duration // 0 = default 10s stdio read timeout
}
