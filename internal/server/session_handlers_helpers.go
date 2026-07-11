package server

import (
	"context"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/session"
)

// loadAndResolveConfig loads project config and applies request overrides
func loadAndResolveConfig(req *createSessionRequest) (*project.Config, error) {
	// Load project config
	projCfg := project.DefaultConfig()
	configPath := ".cerberus/project.yaml"
	if loaded, err := project.LoadFromFile(configPath); err == nil {
		projCfg = *loaded
	}
	projCfgPtr := project.ResolveCredentials(&projCfg)

	// Override URL if provided
	if req.URL != "" && len(projCfgPtr.Services) > 0 {
		services := make([]project.Service, len(projCfgPtr.Services))
		copy(services, projCfgPtr.Services)
		services[0].URL = req.URL
		projCfgPtr.Services = services
	} else if req.URL != "" {
		projCfgPtr.Services = []project.Service{{Name: "target", URL: req.URL, Health: "/"}}
	}

	// Override model if provided
	if req.Model != "" {
		projCfgPtr.Settings.AIBudget.Model = req.Model
	}

	return projCfgPtr, nil
}

// createLLMClient creates an LLM client with the given config
func (srv *Server) createLLMClient(projCfg *project.Config, reqModel string) (llm.Client, error) {
	model := projCfg.Settings.AIBudget.Model
	if model == "" {
		model = srv.cfg.LLMModel
	}
	apiKey := srv.cfg.LLMAPIKey
	baseURL := projCfg.Settings.AIBudget.BaseURL
	if baseURL == "" {
		baseURL = srv.cfg.LLMBaseURL
	}

	return srv.clientFactory(llm.ClientConfig{
		Model:      model,
		APIKey:     apiKey,
		BaseURL:    baseURL,
		Provider:   srv.cfg.LLMProvider,
		AuthScheme: srv.cfg.LLMAuthScheme,
	})
}

// createSessionWithConfig creates a new session with the given configuration
func (srv *Server) createSessionWithConfig(ctx context.Context, cancel context.CancelFunc, req *createSessionRequest, projCfg *project.Config, client llm.Client) (*session.Session, error) {
	mode := session.Mode(req.Mode)

	sess, err := session.NewSession(ctx, session.SessionConfig{
		Mode:       mode,
		Goal:       req.Goal,
		Config:     projCfg,
		Store:      srv.store,
		Client:     client,
		Logger:     srv.logger,
		Gate:       nil,
		ProjectDir: ".",
	})
	if err != nil {
		cancel()
		return nil, err
	}

	// Inject clientFactory so SetupHeadDrivers uses mocked clients in tests
	sess.SetClientFactory(srv.clientFactory)
	baseURL := projCfg.Settings.AIBudget.BaseURL
	if baseURL == "" {
		baseURL = srv.cfg.LLMBaseURL
	}
	sess.SetupHeadDrivers(srv.cfg.LLMAPIKey, baseURL, srv.cfg.LLMAuthScheme, srv.cfg.TierModels)

	return sess, nil
}

// trackAndRunSession tracks the session for cancellation and runs it asynchronously
func (srv *Server) trackAndRunSession(sess *session.Session, cancel context.CancelFunc, ctx context.Context) {
	// Track for cancellation
	srv.mu.Lock()
	srv.runs[sess.ID] = cancel
	srv.mu.Unlock()

	// Run async
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
}
