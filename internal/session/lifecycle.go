package session

import (
	"context"
	"fmt"
	"time"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/head/scout"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/store"
	"go.uber.org/zap"
)

type Mode string

const (
	ModeRun    Mode = "run"
	ModeVerify Mode = "verify"
	ModeServe  Mode = "serve"
)

type Session struct {
	ID        string
	Mode      Mode
	Goal      string
	Config    *project.Config
	Store     *store.Store
	Driver    *ai.Driver
	Logger    *zap.Logger
	StartedAt time.Time
}

func NewSession(ctx context.Context, mode Mode, goal string, cfg *project.Config,
	s *store.Store, client llm.Client, logger *zap.Logger) (*Session, error) {

	budget := ai.NewTokenBudget(
		cfg.Settings.AIBudget.SessionTotalTokens,
		cfg.Settings.AIBudget.PerCallLimit,
	)

	sess := &Session{
		Mode:      mode,
		Goal:      goal,
		Config:    cfg,
		Store:     s,
		Driver:    ai.NewDriver(client, budget),
		Logger:    logger,
		StartedAt: time.Now(),
	}

	dbSess, err := s.CreateSession(ctx, string(mode), goal, cfg.Project.Name)
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	sess.ID = dbSess.ID

	logger.Info("session created",
		zap.String("id", sess.ID),
		zap.String("mode", string(mode)),
		zap.String("goal", goal))

	return sess, nil
}

func (s *Session) Run(ctx context.Context) (err error) {
	s.Logger.Info("session starting", zap.String("id", s.ID))

	defer func() {
		// Write final stats
		stats := map[string]any{
			"total_tokens": s.Driver.Budget().SessionTotal - s.Driver.Budget().Remaining(),
		}
		if statsErr := s.Store.UpdateSessionStats(ctx, s.ID, 0, stats); statsErr != nil {
			s.Logger.Error("update session stats", zap.Error(statsErr))
		}

		// Update status (terminal)
		status := "completed"
		if err != nil {
			status = "failed"
		}
		if updateErr := s.Store.UpdateSessionStatus(ctx, s.ID, status); updateErr != nil {
			s.Logger.Error("update session status", zap.Error(updateErr))
		}
	}()

	// Build Scout head — Analyze + Plan.
	scoutHead := scout.NewScout(s.Driver, s.Store, s.Config, s.Logger)
	model, err := scoutHead.Analyze(ctx, scout.TargetInfo{
		URL:  s.resolveBaseURL(),
		Goal: s.Goal,
	})
	if err != nil {
		return fmt.Errorf("scout analyze: %w", err)
	}

	plan, err := scoutHead.Plan(ctx, s.Goal, model)
	if err != nil {
		return fmt.Errorf("scout plan: %w", err)
	}

	// Build Agent head components.
	baseURL := s.resolveBaseURL()
	engine := agent.NewRuleEngine(baseURL, s.Config.Actors)
	httpExec := agent.NewHTTPActionExecutor(baseURL, s.Logger)
	config := agent.DefaultReActConfig()
	loop := agent.NewReActLoop(s.Driver, s.Store, engine, httpExec, config, s.Logger)

	s.Logger.Info("executing test plan",
		zap.String("session_id", s.ID),
		zap.Int("cases", len(plan.Cases)),
	)

	results, err := loop.ExecutePlan(ctx, plan, s.ID)
	if err != nil {
		return fmt.Errorf("agent execute: %w", err)
	}

	// Log summary.
	passed, failed, skipped := 0, 0, 0
	for _, r := range results {
		switch r.Status {
		case agent.StepPassed:
			passed++
		case agent.StepFailed:
			failed++
		case agent.StepSkipped:
			skipped++
		}
	}
	s.Logger.Info("session completed",
		zap.String("id", s.ID),
		zap.Int("passed", passed),
		zap.Int("failed", failed),
		zap.Int("skipped", skipped),
	)

	return nil
}

func (s *Session) Close() {
	s.Logger.Info("session closed",
		zap.String("id", s.ID),
		zap.Int("tokens_spent", s.Driver.Budget().SessionTotal-s.Driver.Budget().Remaining()))
}

// resolveBaseURL returns the first service URL from project config, or empty string.
func (s *Session) resolveBaseURL() string {
	if len(s.Config.Services) > 0 {
		return s.Config.Services[0].URL
	}
	return ""
}
