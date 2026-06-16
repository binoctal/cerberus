package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
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

	// Load project config.
	projCfg := project.DefaultConfig()
	configPath := ".cerberus/project.yaml"
	if loaded, err := project.LoadFromFile(configPath); err == nil {
		projCfg = *loaded
	}
	projCfgPtr := project.ResolveCredentials(&projCfg)

	// Override URL if provided.
	if req.URL != "" && len(projCfgPtr.Services) > 0 {
		services := make([]project.Service, len(projCfgPtr.Services))
		copy(services, projCfgPtr.Services)
		services[0].URL = req.URL
		projCfgPtr.Services = services
	} else if req.URL != "" {
		projCfgPtr.Services = []project.Service{{Name: "target", URL: req.URL, Health: "/"}}
	}

	// Override model if provided.
	if req.Model != "" {
		projCfgPtr.Settings.AIBudget.Model = req.Model
	}

	// Create LLM client.
	model := projCfgPtr.Settings.AIBudget.Model
	if model == "" {
		model = srv.cfg.LLMModel
	}
	apiKey := srv.cfg.LLMAPIKey
	baseURL := projCfgPtr.Settings.AIBudget.BaseURL
	if baseURL == "" {
		baseURL = srv.cfg.LLMBaseURL
	}

	client, err := srv.clientFactory(llm.ClientConfig{
		Model:    model,
		APIKey:   apiKey,
		BaseURL:  baseURL,
		Provider: srv.cfg.LLMProvider,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "LLM client: %v", err)
		return
	}

	// Create session.
	mode := session.Mode(req.Mode)
	ctx, cancel := context.WithCancel(context.Background())

	sess, err := session.NewSession(ctx, session.SessionConfig{
		Mode:       mode,
		Goal:       req.Goal,
		Config:     projCfgPtr,
		Store:      srv.store,
		Client:     client,
		Logger:     srv.logger,
		Gate:       nil,
		ProjectDir: ".",
	})
	if err != nil {
		cancel()
		writeError(w, http.StatusInternalServerError, "create session: %v", err)
		return
	}

	// Inject clientFactory so SetupHeadDrivers uses mocked clients in tests
	sess.SetClientFactory(srv.clientFactory)
	sess.SetupHeadDrivers(srv.cfg.LLMAPIKey, baseURL, srv.cfg.TierModels)

	// Track for cancellation.
	srv.mu.Lock()
	srv.runs[sess.ID] = cancel
	srv.mu.Unlock()

	// Run async.
	go func() {
		defer func() {
			srv.mu.Lock()
			delete(srv.runs, sess.ID)
			srv.mu.Unlock()
			cancel()
		}()
		if runErr := sess.Run(ctx); runErr != nil {
			srv.logger.Error("session run failed",
				zap.String("session_id", sess.ID),
				zap.Error(runErr))
		}
		sess.Close()
	}()

	writeJSON(w, http.StatusCreated, map[string]string{
		"id":     sess.ID,
		"status": "running",
		"mode":   string(mode),
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
