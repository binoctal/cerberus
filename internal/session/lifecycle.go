package session

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/autotest"
	"github.com/binoctal/cerberus/internal/config"
	"github.com/binoctal/cerberus/internal/escalation"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/store"
)

type Mode string

const (
	ModeRun    Mode = "run"
	ModeVerify Mode = "verify"
	ModeServe  Mode = "serve"
)

// SessionConfig holds configuration for creating a new Session.
// This replaces the multiple parameters previously passed to NewSession.
type SessionConfig struct {
	Mode       Mode
	Goal       string
	Config     *project.Config
	Store      *store.Store
	Client     llm.Client
	Logger     *zap.Logger
	Gate       escalation.Gate
	ProjectDir string
}

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

	// AutoTest phase configuration
	AutoTestSafety     string // "" | "off" | "approve" | "auto" | "dry-run"
	LastAutoTestReport *autotest.AutoTestReport

	// Per-head drivers. When nil, the shared Driver is used.
	scoutDriver    *ai.Driver
	agentDriver    *ai.Driver
	examinerDriver *ai.Driver
	criticDriver   *ai.Driver
	tiers          config.TierModels // head → tier model, for context lookup

	// clientFactory creates LLM clients for per-head drivers. If nil, uses llm.NewClientWithConfig.
	// Injected by tests to provide mock clients.
	clientFactory func(llm.ClientConfig) (llm.Client, error)
}

func NewSession(ctx context.Context, cfg SessionConfig) (*Session, error) {
	if cfg.Gate == nil {
		cfg.Gate = escalation.NoOpGate{}
	}

	budget := ai.NewTokenBudget(
		cfg.Config.Settings.AIBudget.SessionTotalTokens,
		cfg.Config.Settings.AIBudget.PerCallLimit,
	)

	sess := &Session{
		Mode:       cfg.Mode,
		Goal:       cfg.Goal,
		Config:     cfg.Config,
		Store:      cfg.Store,
		Driver:     ai.NewDriver(cfg.Client, budget),
		Logger:     cfg.Logger,
		StartedAt:  time.Now(),
		ProjectDir: cfg.ProjectDir,
		Gate:       cfg.Gate,
	}

	dbSess, err := sess.Store.CreateSession(ctx, string(cfg.Mode), cfg.Goal, cfg.Config.Project.Name)
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	sess.ID = dbSess.ID

	cfg.Logger.Info("session created",
		zap.String("id", sess.ID),
		zap.String("mode", string(cfg.Mode)),
		zap.String("goal", cfg.Goal))

	return sess, nil
}

// SetupHeadDrivers creates per-head LLM drivers from model config.
// Model resolution uses the Phase 1 priority chain (see config.PickModel):
// explicit settings.models > tier from the host CLI > global ai_budget.model.
// Heads with no resolved model fall back to the shared Driver.
func (s *Session) SetupHeadDrivers(apiKey, baseURL string, tiers config.TierModels) {
	s.tiers = tiers
	globalModel := s.Config.Settings.AIBudget.Model
	models := s.Config.Settings.Models

	type headEntry struct {
		head     config.Head
		explicit string
		field    **ai.Driver
	}
	heads := []headEntry{
		{config.HeadScout, models.Scout, &s.scoutDriver},
		{config.HeadAgent, models.Agent, &s.agentDriver},
		{config.HeadExaminer, models.Examiner, &s.examinerDriver},
		{config.HeadCritic, models.Critic, &s.criticDriver},
	}

	for _, h := range heads {
		m := config.PickModel(h.head, h.explicit, tiers, globalModel)
		if m == "" {
			continue // will fall back to shared Driver
		}

		// Use injected clientFactory if available, otherwise default to llm.NewClientWithConfig
		var client llm.Client
		var err error
		if s.clientFactory != nil {
			client, err = s.clientFactory(llm.ClientConfig{
				Model:   m,
				APIKey:  apiKey,
				BaseURL: baseURL,
			})
		} else {
			client, err = llm.NewClientWithConfig(llm.ClientConfig{
				Model:   m,
				APIKey:  apiKey,
				BaseURL: baseURL,
			})
		}
		if err != nil {
			s.Logger.Warn("failed to create head driver, using shared",
				zap.String("head", string(h.head)),
				zap.String("model", m), zap.Error(err))
			continue
		}
		budget := ai.NewTokenBudget(
			s.Config.Settings.AIBudget.SessionTotalTokens,
			s.Config.Settings.AIBudget.PerCallLimit,
		)
		*h.field = ai.NewDriver(client, budget)
		s.Logger.Info("head driver configured",
			zap.String("head", string(h.head)),
			zap.String("model", m))
	}
}

// SetClientFactory injects a clientFactory for creating per-head LLM drivers.
// Used by tests to provide mock clients. If nil, SetupHeadDrivers uses llm.NewClientWithConfig.
func (s *Session) SetClientFactory(factory func(llm.ClientConfig) (llm.Client, error)) {
	s.clientFactory = factory
}

// driverFor returns the per-head driver if set, otherwise the shared Driver.
func (s *Session) driverFor(head **ai.Driver) *ai.Driver {
	if head != nil && *head != nil {
		return *head
	}
	return s.Driver
}

func (s *Session) Run(ctx context.Context) (err error) {
	runStart := time.Now()

	// Create run phase with state
	rp := &runPhase{
		session:   s,
		ctx:       ctx,
		startTime: runStart,
	}

	// Ensure finalization always runs
	defer rp.finalize()

	// Initialize
	if err := rp.initialize(); err != nil {
		rp.err = err
		return err
	}

	// Phase 1: Scout — Analyze + Plan
	model, err := rp.executeScoutPhase()
	if err != nil {
		rp.err = err
		return err
	}

	// Phase 2: Agent — Execute
	if err := rp.executeAgentPhase(); err != nil {
		rp.err = fmt.Errorf("agent execute: %w", err)
		return rp.err
	}

	// Phase 3: Examiner — Judge + Learn
	if err := rp.executeExaminerPhase(); err != nil {
		rp.err = fmt.Errorf("examiner: %w", err)
		return rp.err
	}

	// Phase 4: AutoTest — Coverage-driven test generation (optional)
	rp.executeAutoTestPhase()

	// Build summary
	rp.buildSummary(model)

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
	// Create resume phase with state
	rp := &resumePhase{
		session:   s,
		ctx:       ctx,
		startTime: time.Now(),
	}

	// Ensure finalization always runs
	defer rp.finalize()

	// Initialize
	if err := rp.initialize(); err != nil {
		rp.err = err
		return err
	}

	// Load saved plan
	if err := rp.loadSavedPlan(); err != nil {
		rp.err = err
		return err
	}

	// Filter out completed cases
	if err := rp.filterRemainingCases(); err != nil {
		// If all cases completed, this is not an error
		if err.Error() == "all cases already completed" {
			return nil
		}
		rp.err = err
		return err
	}

	// Execute remaining cases
	if err := rp.executeRemainingCases(); err != nil {
		rp.err = fmt.Errorf("agent execute (resume): %w", err)
		return rp.err
	}

	// Examine results
	if err := rp.examineResults(); err != nil {
		rp.err = fmt.Errorf("examiner (resume): %w", err)
		return rp.err
	}

	// Build summary
	rp.buildSummary()

	return nil
}

// resolveBaseURL returns the first service URL from project config, or empty string.
func (s *Session) resolveBaseURL() string {
	if len(s.Config.Services) > 0 {
		return s.Config.Services[0].URL
	}
	return ""
}
