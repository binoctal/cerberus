package session

import (
	"context"
	"fmt"
	"time"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/store"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

type Mode string

const (
	ModeRun    Mode = "run"
	ModeVerify Mode = "verify"
	ModeServe  Mode = "serve"
)

type Session struct {
	ID        uuid.UUID
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
		zap.String("id", sess.ID.String()),
		zap.String("mode", string(mode)),
		zap.String("goal", goal))

	return sess, nil
}

func (s *Session) Run(ctx context.Context) (err error) {
	s.Logger.Info("session starting", zap.String("id", s.ID.String()))

	defer func() {
		status := "completed"
		if err != nil {
			status = "failed"
		}
		if updateErr := s.Store.UpdateSessionStatus(ctx, s.ID, status); updateErr != nil {
			s.Logger.Error("update session status", zap.Error(updateErr))
		}
	}()

	s.Logger.Info("session completed (skeleton — no heads wired yet)",
		zap.String("id", s.ID.String()))

	return nil
}

func (s *Session) Close() {
	s.Logger.Info("session closed",
		zap.String("id", s.ID.String()),
		zap.Int("tokens_spent", s.Driver.Budget().SessionTotal-s.Driver.Budget().Remaining()))
}
