// internal/mcp/tool_handlers.go
package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/binoctal/cerberus/internal/config"
	"github.com/binoctal/cerberus/internal/escalation"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/session"
)

// handleRun creates a session, wires the MCP escalation gate, and starts
// real session execution in a background goroutine. Progress events are
// streamed to the host via notifications/progress JSON-RPC notifications.
func (srv *Server) handleRun(ctx context.Context, c *conn, args map[string]any) callToolResult {
	goal, _ := args["goal"].(string)
	url, _ := args["url"].(string)
	if goal == "" || url == "" {
		return errorResult("goal and url are required")
	}

	progress := make(chan SessionProgress, 16)
	runCtx, cancel := context.WithCancel(ctx)
	mcpGate := NewMCPGate()

	// Build project config for this run.
	projCfg := project.DefaultConfig()
	projCfg.Services = []project.Service{{Name: "web", URL: url}}
	cfg := config.Load()
	projCfg.Settings.AIBudget.Model = cfg.LLMModel

	// Create LLM client.
	client, clientErr := llm.NewClientWithConfig(llm.ClientConfig{
		Model:    cfg.LLMModel,
		APIKey:   cfg.LLMAPIKey,
		BaseURL:  cfg.LLMBaseURL,
		Provider: cfg.LLMProvider,
	})
	if clientErr != nil {
		cancel()
		return errorResult(fmt.Sprintf("create LLM client: %v", clientErr))
	}

	// NewSession creates the session row in the database (single source of truth).
	testSess, sessionErr := session.NewSession(runCtx, session.SessionConfig{
		Mode:       session.ModeRun,
		Goal:       goal,
		Config:     &projCfg,
		Store:      srv.store,
		Client:     client,
		Logger:     srv.logger,
		Gate:       mcpGate,
		ProjectDir: ".",
	})
	if sessionErr != nil {
		cancel()
		return errorResult(fmt.Sprintf("create session: %v", sessionErr))
	}
	testSess.SetupHeadDrivers(cfg.LLMAPIKey, cfg.LLMBaseURL, cfg.TierModels)
	sessionID := testSess.ID

	srv.mu.Lock()
	srv.sessions[sessionID] = &runningSession{progress: progress, cancel: cancel, gate: mcpGate}
	srv.mu.Unlock()

	// Stream progress notifications to the host.
	go func() {
		for p := range progress {
			c.writeNotification("notifications/progress", p)
		}
	}()

	go func() {
		defer cancel()
		defer func() {
			srv.mu.Lock()
			delete(srv.sessions, sessionID)
			srv.mu.Unlock()
		}()
		defer close(progress)

		progress <- SessionProgress{SessionID: sessionID, Phase: "scout", Status: "running"}

		if runErr := testSess.Run(runCtx); runErr != nil {
			progress <- SessionProgress{SessionID: sessionID, Status: "failed"}
			return
		}

		progress <- SessionProgress{SessionID: sessionID, Status: "completed"}
	}()

	b, _ := json.Marshal(map[string]string{"session_id": sessionID, "status": "running"})
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

	// Validate action against known escalation decisions.
	validActions := map[string]bool{escalation.DecisionContinue: true, escalation.DecisionAbort: true, escalation.DecisionSkipCase: true}
	if !validActions[action] {
		return errorResult(fmt.Sprintf("invalid action %q: must be one of continue, abort, skip_case", action))
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
