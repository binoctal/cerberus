// internal/mcp/tool_handlers_simple.go
package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/binoctal/cerberus/internal/escalation"
)

// handleReport returns the full session details from the database.
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

// handleDecide sends a user decision to unblock a pending escalation checkpoint.
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

// handleCancel cancels a running session.
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
