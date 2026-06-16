package server

import (
	"context"
	"io/fs"
	"net/http"
	"sync"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/config"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/store"
)

// Server is the HTTP API server for CI/CD integration.
type Server struct {
	store         *store.Store
	cfg           *config.Config
	logger        *zap.Logger
	mu            sync.Mutex
	runs          map[string]context.CancelFunc                  // session ID → cancel
	clientFactory func(cfg llm.ClientConfig) (llm.Client, error) // optional override
}

// New creates a new API server.
func New(s *store.Store, cfg *config.Config, logger *zap.Logger) *Server {
	srv := &Server{
		store:  s,
		cfg:    cfg,
		logger: logger,
		runs:   make(map[string]context.CancelFunc),
	}
	srv.clientFactory = func(cfg llm.ClientConfig) (llm.Client, error) {
		return llm.NewClientWithConfig(cfg)
	}
	return srv
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

	// Web dashboard (embedded static files).
	sub, _ := fs.Sub(dashboardFS, "dashboard")
	mux.Handle("GET /dashboard/", http.StripPrefix("/dashboard/", http.FileServer(http.FS(sub))))
	mux.HandleFunc("GET /dashboard", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/dashboard/", http.StatusMovedPermanently)
	})

	return mux
}

// ListenAndServe starts the HTTP server on the given address.
func (srv *Server) ListenAndServe(addr string) error {
	srv.logger.Info("cerberus API server starting", zap.String("addr", addr))
	return http.ListenAndServe(addr, srv.Handler())
}
