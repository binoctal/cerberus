package agent

import "go.uber.org/zap"

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
