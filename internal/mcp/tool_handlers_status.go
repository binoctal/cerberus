// internal/mcp/tool_handlers_status.go
package mcp

import (
	"context"
	"encoding/json"
)

// handleStatus returns the current status of a running or completed session.
// For running sessions, it checks the progress channel and any pending escalation events.
// For completed sessions, it reads from the database.
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
