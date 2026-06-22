package session

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/escalation"
)

// NewSession creates a new session with the given configuration.
func NewSession(ctx context.Context, cfg SessionConfig) (*Session, error) {
	sess := newSessionStruct(cfg)
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

// NewSessionForResume binds a Session to an existing session ID WITHOUT
// inserting a new sessions row. Used by --resume so the resumed run writes its
// verdicts/traces into the original session instead of leaving a freshly
// inserted "running" row orphaned.
func NewSessionForResume(ctx context.Context, cfg SessionConfig, resumeID string) (*Session, error) {
	sess := newSessionStruct(cfg)
	sess.ID = resumeID
	cfg.Logger.Info("session resume",
		zap.String("id", sess.ID),
		zap.String("goal", cfg.Goal))
	return sess, nil
}

// newSessionStruct builds the in-memory Session from config (driver, budget,
// timestamps) without touching the store. NewSession inserts a row afterwards;
// NewSessionForResume does not.
func newSessionStruct(cfg SessionConfig) *Session {
	if cfg.Gate == nil {
		cfg.Gate = escalation.NoOpGate{}
	}
	budget := ai.NewTokenBudget(
		cfg.Config.Settings.AIBudget.SessionTotalTokens,
		cfg.Config.Settings.AIBudget.PerCallLimit,
	)
	return &Session{
		Mode:       cfg.Mode,
		Goal:       cfg.Goal,
		Config:     cfg.Config,
		Store:      cfg.Store,
		Driver:     ai.NewDriver(cfg.Client, budget),
		Logger:     cfg.Logger,
		StartedAt:  time.Now(),
		ProjectDir: cfg.ProjectDir,
		Gate:       cfg.Gate,
		coverageFn: cfg.CoverageFn,
	}
}
