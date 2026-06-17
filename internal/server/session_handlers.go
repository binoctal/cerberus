package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"github.com/binoctal/cerberus/internal/session"
	"github.com/binoctal/cerberus/internal/store"
)

// handleCreateSession creates and starts a new test session.
func (srv *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	var req createSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid JSON: %v", err)
		return
	}

	if req.Goal == "" {
		writeError(w, http.StatusBadRequest, "goal is required")
		return
	}
	if req.Mode == "" {
		req.Mode = "run"
	}

	// Phase 1: Load and resolve project config
	projCfg, err := loadAndResolveConfig(&req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "config: %v", err)
		return
	}

	// Phase 2: Create LLM client
	client, err := srv.createLLMClient(projCfg, req.Model)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "LLM client: %v", err)
		return
	}

	// Phase 3: Create session
	ctx, cancel := context.WithCancel(context.Background())
	sess, err := srv.createSessionWithConfig(ctx, cancel, &req, projCfg, client)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create session: %v", err)
		return
	}

	// Phase 4: Track and run session
	srv.trackAndRunSession(sess, cancel, ctx)

	writeJSON(w, http.StatusCreated, map[string]string{
		"id":     sess.ID,
		"status": "running",
		"mode":   string(session.Mode(req.Mode)),
	})
}

// handleListSessions returns a list of recent sessions.
func (srv *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	limit := 20
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}
	sessions, err := srv.store.ListSessions(r.Context(), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list sessions: %v", err)
		return
	}
	if sessions == nil {
		sessions = []store.Session{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"sessions": sessions,
		"count":    len(sessions),
	})
}

// handleGetSession returns a single session by ID.
func (srv *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sess, err := srv.store.GetSession(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	writeJSON(w, http.StatusOK, sess)
}

// handleCancelSession cancels a running session.
func (srv *Server) handleCancelSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	srv.mu.Lock()
	cancel, ok := srv.runs[id]
	srv.mu.Unlock()

	if !ok {
		writeError(w, http.StatusNotFound, "session not running or not found")
		return
	}

	cancel()
	_ = srv.store.UpdateSessionStatus(r.Context(), id, "aborted")

	writeJSON(w, http.StatusOK, map[string]string{
		"id":     id,
		"status": "aborted",
	})
}
