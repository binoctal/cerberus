// internal/mcp/tool_handlers_run.go
package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/binoctal/cerberus/internal/config"
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
		Model:      cfg.LLMModel,
		APIKey:     cfg.LLMAPIKey,
		BaseURL:    cfg.LLMBaseURL,
		Provider:   cfg.LLMProvider,
		AuthScheme: cfg.LLMAuthScheme,
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
	testSess.SetupHeadDrivers(cfg.LLMAPIKey, cfg.LLMBaseURL, cfg.LLMAuthScheme, cfg.TierModels)
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
