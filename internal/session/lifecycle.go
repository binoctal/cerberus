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
	ID         string
	Mode       Mode
	Goal       string
	Config     *project.Config
	Store      *store.Store
	Driver     *ai.Driver
	Logger     *zap.Logger
	StartedAt  time.Time
	DeepPlan   bool
	ProjectDir string
	Gate       escalation.Gate
	Parallel   bool
	MaxWorkers int

	// Per-head drivers. When nil, the shared Driver is used.
	scoutDriver    *ai.Driver
	agentDriver    *ai.Driver
	examinerDriver *ai.Driver
	criticDriver   *ai.Driver
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
		Mode:       mode,
		Goal:       goal,
		Config:     cfg,
		Store:      s,
		Driver:     ai.NewDriver(client, budget),
		Logger:     logger,
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

// SetupHeadDrivers creates per-head LLM drivers from model config.
// If models config is empty, all heads use the shared Driver.
func (s *Session) SetupHeadDrivers(apiKey, baseURL string) {
	models := s.Config.Settings.Models
	globalModel := s.Config.Settings.AIBudget.Model

	type head struct {
		model string
		field **ai.Driver
	}
	heads := []head{
		{models.Scout, &s.scoutDriver},
		{models.Agent, &s.agentDriver},
		{models.Examiner, &s.examinerDriver},
		{models.Critic, &s.criticDriver},
	}

	for _, h := range heads {
		m := h.model
		if m == "" {
			continue // will fall back to shared Driver
		}
		client, err := llm.NewClientWithConfig(llm.ClientConfig{
			Model:   m,
			APIKey:  apiKey,
			BaseURL: baseURL,
		})
		if err != nil {
			s.Logger.Warn("failed to create head driver, using shared",
				zap.String("model", m), zap.Error(err))
			continue
		}
		budget := ai.NewTokenBudget(
			s.Config.Settings.AIBudget.SessionTotalTokens,
			s.Config.Settings.AIBudget.PerCallLimit,
		)
		*h.field = ai.NewDriver(client, budget)
		s.Logger.Info("head driver configured", zap.String("model", m))
	}

	_ = globalModel // suppress unused warning
}

// driverFor returns the per-head driver if set, otherwise the shared Driver.
func (s *Session) driverFor(head **ai.Driver) *ai.Driver {
	if head != nil && *head != nil {
		return *head
	}
	return s.Driver
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
				Duration:   elapsed.Round(time.Millisecond).String(),
				DurationMs: elapsed.Milliseconds(),
			}
		} else {
			summary.TotalTokens = tokensUsed
			summary.Duration = elapsed.Round(time.Millisecond).String()
			summary.DurationMs = elapsed.Milliseconds()
		}

		// Write stats to store.
		if statsErr := s.Store.UpdateSessionStats(ctx, s.ID, summary.CoveragePct, summary); statsErr != nil {
			s.Logger.Error("update session stats", zap.Error(statsErr))
		}

		// Print human-readable summary.
		s.Logger.Info("session summary", zap.String("summary", summary.String()))

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
	scoutHead := scout.NewScout(s.driverFor(&s.scoutDriver), s.Store, s.Config, s.Logger)
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

	// Persist plan for potential resumption.
	if saveErr := s.Store.SavePlan(ctx, s.ID, plan); saveErr != nil {
		s.Logger.Warn("failed to save plan", zap.Error(saveErr))
	}

	// Phase 2: Agent — Execute.
	baseURL := s.resolveBaseURL()
	projectDir := s.ProjectDir
	if projectDir == "" {
		projectDir = "."
	}
	engine := agent.NewRuleEngine(baseURL, s.Config.Actors, projectDir)
	multiExec := agent.BuildMultiExecutor(projectDir, s.Gate, s.Logger)
	config := agent.DefaultReActConfig()
	loop := agent.NewReActLoopWithGate(s.driverFor(&s.agentDriver), s.Store, engine, multiExec, config, s.Gate, s.Logger)

	s.Logger.Info("executing test plan",
		zap.String("session_id", s.ID),
		zap.Int("cases", len(plan.Cases)),
		zap.Bool("parallel", s.Parallel),
	)

	var results []agent.StepResult
	if s.Parallel {
		workers := s.MaxWorkers
		if workers <= 0 {
			workers = 4
		}
		pExec := agent.NewParallelExecutor(loop, agent.ParallelConfig{MaxWorkers: workers}, s.Logger)
		results, err = pExec.ExecutePlan(ctx, plan, s.ID)
	} else {
		results, err = loop.ExecutePlan(ctx, plan, s.ID)
	}
	if err != nil {
		return fmt.Errorf("agent execute: %w", err)
	}

	// Phase 3: Examiner — Judge + Learn.
	examinerCfg := examiner.DefaultExaminerConfig()
	if s.Config.Settings.ConfidenceThreshold > 0 {
		examinerCfg.ConfThreshold = s.Config.Settings.ConfidenceThreshold
		examinerCfg.AutoFix = s.Config.Settings.AutoFix
	}
	examinerHead := examiner.NewExaminer(s.driverFor(&s.examinerDriver), s.criticDriver, s.Store, examinerCfg, s.Logger)
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

// Resume loads a saved plan and continues from the first uncompleted test case.
// It skips Scout entirely, reuses the stored plan, and only executes remaining cases.
func (s *Session) Resume(ctx context.Context) (err error) {
	s.Logger.Info("resuming session", zap.String("id", s.ID))
	runStart := time.Now()
	var summary *SessionSummary

	defer func() {
		elapsed := time.Since(runStart)
		tokensUsed := s.Driver.Budget().SessionTotal - s.Driver.Budget().Remaining()
		if summary == nil {
			summary = &SessionSummary{
				Goal: s.Goal, TotalTokens: tokensUsed,
				Duration:   elapsed.Round(time.Millisecond).String(),
				DurationMs: elapsed.Milliseconds(),
			}
		} else {
			summary.TotalTokens = tokensUsed
			summary.Duration = elapsed.Round(time.Millisecond).String()
			summary.DurationMs = elapsed.Milliseconds()
		}
		if statsErr := s.Store.UpdateSessionStats(ctx, s.ID, summary.CoveragePct, summary); statsErr != nil {
			s.Logger.Error("update session stats", zap.Error(statsErr))
		}
		s.Logger.Info("session summary", zap.String("summary", summary.String()))
		status := "completed"
		if err != nil {
			status = "failed"
		}
		if statsErr := s.Store.UpdateSessionStatus(ctx, s.ID, status); statsErr != nil {
			s.Logger.Error("update session status", zap.Error(statsErr))
		}
	}()

	// Load saved plan.
	var plan agent.TestPlan
	if err := s.Store.LoadPlan(ctx, s.ID, &plan); err != nil {
		return fmt.Errorf("load plan for session %s: %w", s.ID, err)
	}
	if len(plan.Cases) == 0 {
		return fmt.Errorf("saved plan has no test cases")
	}

	// Get completed targets.
	completed, err := s.Store.GetCompletedTargets(ctx, s.ID)
	if err != nil {
		return fmt.Errorf("get completed targets: %w", err)
	}

	// Filter out completed cases.
	var remaining []agent.TestCase
	for _, tc := range plan.Cases {
		if !completed[tc.Target] {
			remaining = append(remaining, tc)
		}
	}

	s.Logger.Info("resuming from saved plan",
		zap.Int("total_cases", len(plan.Cases)),
		zap.Int("completed", len(completed)),
		zap.Int("remaining", len(remaining)),
	)

	if len(remaining) == 0 {
		s.Logger.Info("all cases already completed")
		return nil
	}

	// Build a reduced plan with only remaining cases.
	resumePlan := &agent.TestPlan{
		Goal:       plan.Goal,
		Cases:      remaining,
		ProjectURL: plan.ProjectURL,
	}

	// Execute remaining cases.
	baseURL := s.resolveBaseURL()
	projectDir := s.ProjectDir
	if projectDir == "" {
		projectDir = "."
	}
	engine := agent.NewRuleEngine(baseURL, s.Config.Actors, projectDir)
	multiExec := agent.BuildMultiExecutor(projectDir, s.Gate, s.Logger)
	config := agent.DefaultReActConfig()
	loop := agent.NewReActLoopWithGate(s.driverFor(&s.agentDriver), s.Store, engine, multiExec, config, s.Gate, s.Logger)

	var results []agent.StepResult
	if s.Parallel {
		workers := s.MaxWorkers
		if workers <= 0 {
			workers = 4
		}
		pExec := agent.NewParallelExecutor(loop, agent.ParallelConfig{MaxWorkers: workers}, s.Logger)
		results, err = pExec.ExecutePlan(ctx, resumePlan, s.ID)
	} else {
		results, err = loop.ExecutePlan(ctx, resumePlan, s.ID)
	}
	if err != nil {
		return fmt.Errorf("agent execute (resume): %w", err)
	}

	// Examine results.
	examinerCfg := examiner.DefaultExaminerConfig()
	if s.Config.Settings.ConfidenceThreshold > 0 {
		examinerCfg.ConfThreshold = s.Config.Settings.ConfidenceThreshold
		examinerCfg.AutoFix = s.Config.Settings.AutoFix
	}
	examinerHead := examiner.NewExaminer(s.driverFor(&s.examinerDriver), s.criticDriver, s.Store, examinerCfg, s.Logger)
	verdicts, reflections, err := examinerHead.Examine(ctx, results, s.ID, s.Config.Project.Name)
	if err != nil {
		return fmt.Errorf("examiner (resume): %w", err)
	}

	// Build summary for resumed portion.
	summary = FromResults(
		s.Goal,
		s.resolveBaseURL(),
		len(remaining),
		results,
		verdicts,
		reflections,
		0,
		time.Since(runStart),
	)

	return nil
}

// resolveBaseURL returns the first service URL from project config, or empty string.
func (s *Session) resolveBaseURL() string {
	if len(s.Config.Services) > 0 {
		return s.Config.Services[0].URL
	}
	return ""
}
