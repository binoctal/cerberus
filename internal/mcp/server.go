// internal/mcp/server.go
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"

	"github.com/binoctal/cerberus/internal/config"
	"github.com/binoctal/cerberus/internal/escalation"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/session"
	"github.com/binoctal/cerberus/internal/store"
	"go.uber.org/zap"
)

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
			_ = srv.store.UpdateSessionStatus(ctx, sess.ID, "interrupted")
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

// handleConn handles a single request (for testing).
func (srv *Server) handleConn(r io.Reader, w io.Writer) error {
	c := newConn(r, w)
	req, err := c.readRequest()
	if err != nil {
		return err
	}
	srv.handleRequest(context.Background(), c, req)
	return nil
}

func (srv *Server) handleRequest(ctx context.Context, c *conn, req jsonRPCRequest) {
	switch req.Method {
	case "initialize":
		c.writeResponse(jsonRPCResponse{
			JSONRPC: "2.0", ID: req.ID,
			Result: map[string]any{
				"protocolVersion": "2024-11-05",
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "cerberus", "version": "0.1.0"},
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
		result = srv.handleRun(ctx, params.Arguments)
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

// listTools returns the 5 tool definitions.
func (srv *Server) listTools() []toolDefinition {
	return []toolDefinition{
		{
			Name:        "cerberus_run",
			Description: "Start a Cerberus test session. Returns session_id immediately. After calling, periodically call cerberus_status to check progress. Stop when status is 'completed' or 'failed'.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"goal": map[string]any{"type": "string", "description": "Test goal, e.g. 'Test all API endpoints'"},
					"url":  map[string]any{"type": "string", "description": "Target base URL, e.g. 'http://localhost:3000'"},
				},
				"required": []string{"goal", "url"},
			},
		},
		{
			Name:        "cerberus_status",
			Description: "Poll the progress of a running test session.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"session_id": map[string]any{"type": "string", "description": "Session ID from cerberus_run"},
				},
				"required": []string{"session_id"},
			},
		},
		{
			Name:        "cerberus_report",
			Description: "Get the final test report for a completed session.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"session_id": map[string]any{"type": "string", "description": "Session ID"},
				},
				"required": []string{"session_id"},
			},
		},
		{
			Name:        "cerberus_decide",
			Description: "Provide a user decision for a pending escalation event.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"session_id": map[string]any{"type": "string", "description": "Session ID"},
					"action":     map[string]any{"type": "string", "description": "Decision: 'continue', 'abort', or 'skip_case'"},
					"payload":    map[string]any{"type": "string", "description": "Optional extra info, e.g. new URL for unreachable targets"},
				},
				"required": []string{"session_id", "action"},
			},
		},
		{
			Name:        "cerberus_cancel",
			Description: "Cancel a running test session.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"session_id": map[string]any{"type": "string", "description": "Session ID to cancel"},
				},
				"required": []string{"session_id"},
			},
		},
	}
}

// handleRun creates a session, wires the MCP escalation gate, and starts
// real session execution in a background goroutine.
func (srv *Server) handleRun(ctx context.Context, args map[string]any) callToolResult {
	goal, _ := args["goal"].(string)
	url, _ := args["url"].(string)
	if goal == "" || url == "" {
		return errorResult("goal and url are required")
	}

	sess, err := srv.store.CreateSession(ctx, "run", goal, "")
	if err != nil {
		return errorResult(fmt.Sprintf("create session: %v", err))
	}

	progress := make(chan SessionProgress, 16)
	runCtx, cancel := context.WithCancel(ctx)
	mcpGate := NewMCPGate()

	srv.mu.Lock()
	srv.sessions[sess.ID] = &runningSession{progress: progress, cancel: cancel, gate: mcpGate}
	srv.mu.Unlock()

	go func() {
		defer cancel()
		defer func() {
			srv.mu.Lock()
			delete(srv.sessions, sess.ID)
			srv.mu.Unlock()
		}()

		progress <- SessionProgress{SessionID: sess.ID, Phase: "scout", Status: "running"}

		// Build project config for this run.
		projCfg := project.DefaultConfig()
		projCfg.Services = []project.Service{{Name: "web", URL: url}}
		cfg := config.Load()
		projCfg.Settings.AIBudget.Model = cfg.LLMModel

		// Create LLM client.
		client, clientErr := llm.NewClient(cfg.LLMModel, cfg.LLMAPIKey)
		if clientErr != nil {
			progress <- SessionProgress{SessionID: sess.ID, Status: "failed"}
			_ = srv.store.UpdateSessionStatus(runCtx, sess.ID, "failed")
			return
		}

		testSess, sessionErr := session.NewSession(runCtx, session.ModeRun, goal, &projCfg, srv.store, client, srv.logger, mcpGate)
		if sessionErr != nil {
			progress <- SessionProgress{SessionID: sess.ID, Status: "failed"}
			_ = srv.store.UpdateSessionStatus(runCtx, sess.ID, "failed")
			return
		}

		if runErr := testSess.Run(runCtx); runErr != nil {
			progress <- SessionProgress{SessionID: sess.ID, Status: "failed"}
			return
		}

		progress <- SessionProgress{SessionID: sess.ID, Status: "completed"}
	}()

	b, _ := json.Marshal(map[string]string{"session_id": sess.ID, "status": "running"})
	return textResult(string(b))
}

func (srv *Server) handleStatus(args map[string]any) callToolResult {
	sessionID, _ := args["session_id"].(string)
	if sessionID == "" {
		return errorResult("session_id is required")
	}
	srv.mu.Lock()
	rs, ok := srv.sessions[sessionID]
	srv.mu.Unlock()
	if !ok {
		// Check store for completed sessions.
		sess, err := srv.store.GetSession(context.Background(), sessionID)
		if err != nil {
			return errorResult("session not found")
		}
		b, _ := json.Marshal(SessionProgress{SessionID: sess.ID, Status: sess.Status})
		return textResult(string(b))
	}

	// Read latest progress (non-blocking).
	var latest SessionProgress
	for {
		select {
		case p := <-rs.progress:
			latest = p
		default:
			if latest.SessionID == "" {
				latest = SessionProgress{SessionID: sessionID, Status: "running"}
			}
			goto done
		}
	}
done:
	if rs.gate != nil {
		if evt := rs.gate.PendingEvent(); evt != nil {
			latest.Status = "pending_decision"
			latest.Event = &PendingEvent{Type: evt.Type, Message: evt.Message, Data: evt.Data}
		}
	}
	b, _ := json.Marshal(latest)
	return textResult(string(b))
}

func (srv *Server) handleReport(args map[string]any) callToolResult {
	sessionID, _ := args["session_id"].(string)
	if sessionID == "" {
		return errorResult("session_id is required")
	}
	sess, err := srv.store.GetSession(context.Background(), sessionID)
	if err != nil {
		return errorResult("session not found")
	}
	b, _ := json.MarshalIndent(sess, "", "  ")
	return textResult(string(b))
}

func (srv *Server) handleDecide(args map[string]any) callToolResult {
	sessionID, _ := args["session_id"].(string)
	action, _ := args["action"].(string)
	payload, _ := args["payload"].(string)
	if sessionID == "" || action == "" {
		return errorResult("session_id and action are required")
	}
	srv.mu.Lock()
	rs, ok := srv.sessions[sessionID]
	srv.mu.Unlock()
	if !ok {
		return errorResult("session not found or already completed")
	}
	rs.gate.SendDecision(escalation.Decision{Action: action, Payload: payload})
	return textResult(fmt.Sprintf(`{"session_id":"%s","decision_acknowledged":true}`, sessionID))
}

func (srv *Server) handleCancel(args map[string]any) callToolResult {
	sessionID, _ := args["session_id"].(string)
	if sessionID == "" {
		return errorResult("session_id is required")
	}
	srv.mu.Lock()
	rs, ok := srv.sessions[sessionID]
	if ok {
		rs.cancel()
		delete(srv.sessions, sessionID)
	}
	srv.mu.Unlock()
	if !ok {
		return errorResult("session not found or already completed")
	}
	return textResult(fmt.Sprintf(`{"session_id":"%s","status":"cancelled"}`, sessionID))
}
