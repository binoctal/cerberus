package session

import (
	"context"
	"fmt"
	"time"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/escalation"
	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/head/examiner"
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
	DeepPlan  bool
	ProjectDir string
	Gate      escalation.Gate
}

func NewSession(ctx context.Context, mode Mode, goal string, cfg *project.Config,
	s *store.Store, client llm.Client, logger *zap.Logger, gate escalation.Gate,
	projectDir string) (*Session, error) {

	if gate == nil {
		gate = escalation.NoOpGate{}
	}

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
		StartedAt:  time.Now(),
		ProjectDir: projectDir,
		Gate:       gate,
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
	runStart := time.Now()
	var summary *SessionSummary

	defer func() {
		elapsed := time.Since(runStart)
		tokensUsed := s.Driver.Budget().SessionTotal - s.Driver.Budget().Remaining()

		// Build summary if not yet built (e.g. on early error).
		if summary == nil {
			summary = &SessionSummary{
				Goal: s.Goal, TotalTokens: tokensUsed,
				Duration: elapsed.Round(time.Millisecond).String(),
				DurationMs: elapsed.Milliseconds(),
			}
		} else {
			summary.TotalTokens = tokensUsed
			summary.Duration = elapsed.Round(time.Millisecond).String()
			summary.DurationMs = elapsed.Milliseconds()
		}

		// Write stats to store.
		if statsErr := s.Store.UpdateSessionStats(ctx, s.ID, 0, summary); statsErr != nil {
			s.Logger.Error("update session stats", zap.Error(statsErr))
		}

		// Print human-readable summary.
		fmt.Println()
		fmt.Println(summary.String())

		// Update status (terminal).
		status := "completed"
		if err != nil {
			status = "failed"
		}
		if updateErr := s.Store.UpdateSessionStatus(ctx, s.ID, status); updateErr != nil {
			s.Logger.Error("update session status", zap.Error(updateErr))
		}
	}()

	// Phase 1: Scout — Analyze + Plan.
	scoutHead := scout.NewScout(s.Driver, s.Store, s.Config, s.Logger)
	if s.DeepPlan {
		scoutHead.SetDeepPlan(scout.DefaultToTConfig())
	}
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

	// Phase 2: Agent — Execute.
	baseURL := s.resolveBaseURL()
	engine := agent.NewRuleEngine(baseURL, s.Config.Actors)
	projectDir := s.ProjectDir
	if projectDir == "" {
		projectDir = "."
	}
	multiExec := agent.BuildMultiExecutor(projectDir, s.Gate, s.Logger)
	config := agent.DefaultReActConfig()
	loop := agent.NewReActLoopWithGate(s.Driver, s.Store, engine, multiExec, config, s.Gate, s.Logger)

	s.Logger.Info("executing test plan",
		zap.String("session_id", s.ID),
		zap.Int("cases", len(plan.Cases)),
	)

	results, err := loop.ExecutePlan(ctx, plan, s.ID)
	if err != nil {
		return fmt.Errorf("agent execute: %w", err)
	}

	// Phase 3: Examiner — Judge + Learn.
	examinerCfg := examiner.DefaultExaminerConfig()
	examinerHead := examiner.NewExaminer(s.Driver, nil, s.Store, examinerCfg, s.Logger)
	verdicts, reflections, err := examinerHead.Examine(ctx, results, s.ID, s.Config.Project.Name)
	if err != nil {
		return fmt.Errorf("examiner: %w", err)
	}

	s.Logger.Info("examination complete",
		zap.Int("verdicts", len(verdicts)),
		zap.Int("reflections_stored", reflections),
	)

	// Build summary.
	summary = FromResults(
		s.Goal,
		s.resolveBaseURL(),
		len(plan.Cases),
		results,
		verdicts,
		reflections,
		0, // tokens filled in defer
		time.Since(runStart),
	)
	summary.EndpointsFound = len(model.API.Endpoints)

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
