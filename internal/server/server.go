package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/binoctal/cerberus/internal/config"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/session"
	"github.com/binoctal/cerberus/internal/store"
	"go.uber.org/zap"
)

// Server is the HTTP API server for CI/CD integration.
type Server struct {
	store  *store.Store
	cfg    *config.Config
	logger *zap.Logger
	mu     sync.Mutex
	runs   map[string]context.CancelFunc // session ID → cancel
}

// New creates a new API server.
func New(s *store.Store, cfg *config.Config, logger *zap.Logger) *Server {
	return &Server{
		store:  s,
		cfg:    cfg,
		logger: logger,
		runs:   make(map[string]context.CancelFunc),
	}
}

// Handler returns the HTTP handler with all routes registered.
func (srv *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", srv.handleHealth)
	mux.HandleFunc("POST /api/v1/sessions", srv.handleCreateSession)
	mux.HandleFunc("GET /api/v1/sessions", srv.handleListSessions)
	mux.HandleFunc("GET /api/v1/sessions/{id}", srv.handleGetSession)
	mux.HandleFunc("GET /api/v1/sessions/{id}/report", srv.handleGetReport)
	mux.HandleFunc("POST /api/v1/sessions/{id}/cancel", srv.handleCancelSession)
	return mux
}

// ListenAndServe starts the HTTP server on the given address.
func (srv *Server) ListenAndServe(addr string) error {
	srv.logger.Info("cerberus API server starting", zap.String("addr", addr))
	return http.ListenAndServe(addr, srv.Handler())
}

// --- Handlers ---

func (srv *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "ok",
		"time":   time.Now().UTC().Format(time.RFC3339),
	})
}

type createSessionRequest struct {
	Mode  string `json:"mode"`  // "run" or "verify", default "run"
	Goal  string `json:"goal"`  // required
	URL   string `json:"url"`   // override project URL
	Model string `json:"model"` // override LLM model
}

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
	client, err := llm.NewClient(model, apiKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "LLM client: %v", err)
		return
	}

	// Create session.
	mode := session.Mode(req.Mode)
	ctx, cancel := context.WithCancel(context.Background())

	sess, err := session.NewSession(ctx, mode, req.Goal, projCfgPtr, srv.store, client, srv.logger)
	if err != nil {
		cancel()
		writeError(w, http.StatusInternalServerError, "create session: %v", err)
		return
	}

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

func (srv *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sess, err := srv.store.GetSession(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	writeJSON(w, http.StatusOK, sess)
}

func (srv *Server) handleGetReport(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sess, err := srv.store.GetSession(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	// Get traces for evidence.
	traces, _ := srv.store.GetTraces(r.Context(), id)

	// Get verdicts.
	verdicts, _ := srv.store.GetVerdicts(r.Context(), id)

	// Content negotiation: text/plain for CLI, JSON default.
	accept := r.Header.Get("Accept")
	if accept == "text/plain" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintf(w, "Session: %s\n", sess.ID)
		fmt.Fprintf(w, "Goal: %s\n", sess.Goal)
		fmt.Fprintf(w, "Status: %s\n", sess.Status)
		fmt.Fprintf(w, "Started: %s\n", sess.StartedAt)
		if sess.FinishedAt != "" {
			fmt.Fprintf(w, "Finished: %s\n", sess.FinishedAt)
		}
		fmt.Fprintf(w, "Traces: %d\n", len(traces))
		fmt.Fprintf(w, "Verdicts: %d\n", len(verdicts))
		if sess.Stats != "" && sess.Stats != "{}" {
			fmt.Fprintf(w, "Stats: %s\n", sess.Stats)
		}
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"session":  sess,
		"traces":   traces,
		"verdicts": verdicts,
	})
}

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

// --- Helpers ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, format string, args ...any) {
	writeJSON(w, status, map[string]string{
		"error": fmt.Sprintf(format, args...),
	})
}
