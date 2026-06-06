package session

import (
	"context"
	"fmt"
	"time"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/head/agent"
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

	// Build Agent head components.
	baseURL := s.resolveBaseURL()
	engine := agent.NewRuleEngine(baseURL, s.Config.Actors)
	httpExec := agent.NewHTTPActionExecutor(baseURL, s.Logger)
	config := agent.DefaultReActConfig()
	loop := agent.NewReActLoop(s.Driver, s.Store, engine, httpExec, config, s.Logger)

	// Build a plan from project config (temporary bridge until Scout head is implemented).
	plan := s.buildPlanFromConfig()

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

// buildPlanFromConfig derives a TestPlan from project config services and invariants.
// This is a temporary bridge until the Scout head produces real TestPlans (C2a).
func (s *Session) buildPlanFromConfig() *agent.TestPlan {
	plan := &agent.TestPlan{
		Goal:       s.Goal,
		ProjectURL: s.resolveBaseURL(),
	}

	caseID := 0

	// Generate cases from services endpoints.
	for _, svc := range s.Config.Services {
		if svc.Health != "" {
			caseID++
			plan.Cases = append(plan.Cases, agent.TestCase{
				ID:          fmt.Sprintf("health-%d", caseID),
				Name:        fmt.Sprintf("Health check: %s", svc.Name),
				Target:      svc.Health,
				Method:      "GET",
				Expectation: "returns 200 OK",
			})
		}
	}

	// Generate cases from invariants.
	for _, inv := range s.Config.Invariants {
		caseID++
		plan.Cases = append(plan.Cases, agent.TestCase{
			ID:          fmt.Sprintf("inv-%d", caseID),
			Name:        fmt.Sprintf("Invariant: %s", inv.ID),
			Target:      inv.Check,
			Expectation: inv.Assertion,
		})
	}

	// If no cases were generated, create a default health check.
	if len(plan.Cases) == 0 && plan.ProjectURL != "" {
		plan.Cases = append(plan.Cases, agent.TestCase{
			ID:          "default-health",
			Name:        "Default health check",
			Target:      "/",
			Method:      "GET",
			Expectation: "returns 200 OK",
		})
	}

	return plan
}
