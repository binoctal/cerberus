// internal/mcp/server.go
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/store"
)

// Version is set at build time via -ldflags. Default matches the latest release.
var Version = "0.5.0"

// Server implements the MCP server for Cerberus.
type Server struct {
	store    *store.Store
	logger   *zap.Logger
	mu       sync.Mutex
	sessions map[string]*runningSession
}

type runningSession struct {
	progress chan SessionProgress
	cancel   context.CancelFunc
	gate     *MCPGate
}

// NewServer creates a new MCP server.
func NewServer(s *store.Store, logger *zap.Logger) *Server {
	return &Server{
		store:    s,
		logger:   logger,
		sessions: make(map[string]*runningSession),
	}
}

// RecoverOrphanSessions marks all "running" sessions as "interrupted".
// Called on MCP server startup to handle crashes from previous runs.
func (srv *Server) RecoverOrphanSessions(ctx context.Context) {
	sessions, err := srv.store.ListSessions(ctx, 1000)
	if err != nil {
		return
	}
	for _, sess := range sessions {
		if sess.Status == "running" {
			if err := srv.store.UpdateSessionStatus(ctx, sess.ID, "interrupted"); err != nil {
				srv.logger.Error("update interrupted status", zap.Error(err))
			}
			srv.logger.Info("recovered orphan session", zap.String("id", sess.ID))
		}
	}
}

// Serve starts the MCP server, reading from r and writing to w.
func (srv *Server) Serve(ctx context.Context, r io.Reader, w io.Writer) error {
	srv.RecoverOrphanSessions(ctx)
	c := newConn(r, w)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		req, err := c.readRequest()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			srv.logger.Error("read request", zap.Error(err))
			continue
		}
		srv.handleRequest(ctx, c, req)
	}
}

func (srv *Server) handleRequest(ctx context.Context, c *conn, req jsonRPCRequest) {
	switch req.Method {
	case "initialize":
		c.writeResponse(jsonRPCResponse{
			JSONRPC: "2.0", ID: req.ID,
			Result: map[string]any{
				"protocolVersion": "2024-11-05",
				"capabilities":    map[string]any{"tools": map[string]any{}, "notifications": map[string]any{}},
				"serverInfo":      map[string]any{"name": "cerberus", "version": Version},
			},
		})
	case "notifications/initialized":
		// No response for notifications.
	case "tools/list":
		c.writeResponse(jsonRPCResponse{
			JSONRPC: "2.0", ID: req.ID,
			Result: listToolsResult{Tools: srv.listTools()},
		})
	case "tools/call":
		srv.handleToolCall(ctx, c, req)
	default:
		c.writeError(req.ID, -32601, fmt.Sprintf("method not found: %s", req.Method))
	}
}

func (srv *Server) handleToolCall(ctx context.Context, c *conn, req jsonRPCRequest) {
	paramsBytes, err := json.Marshal(req.Params)
	if err != nil {
		c.writeError(req.ID, -32602, "invalid params")
		return
	}
	var params toolCallParams
	if err := json.Unmarshal(paramsBytes, &params); err != nil {
		c.writeError(req.ID, -32602, "invalid params")
		return
	}

	var result callToolResult
	switch params.Name {
	case "cerberus_run":
		result = srv.handleRun(ctx, c, params.Arguments)
	case "cerberus_status":
		result = srv.handleStatus(params.Arguments)
	case "cerberus_report":
		result = srv.handleReport(params.Arguments)
	case "cerberus_decide":
		result = srv.handleDecide(params.Arguments)
	case "cerberus_cancel":
		result = srv.handleCancel(params.Arguments)
	default:
		result = errorResult(fmt.Sprintf("unknown tool: %s", params.Name))
	}

	c.writeResponse(jsonRPCResponse{
		JSONRPC: "2.0", ID: req.ID, Result: result,
	})
}
