package agent

import (
	"time"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/embed"
	"github.com/binoctal/cerberus/internal/escalation"
	"github.com/binoctal/cerberus/internal/store"
)

// ReActLoopConfig holds configuration for creating a ReAct execution loop.
// This replaces the multiple parameters previously passed to NewReActLoopWithGate.
type ReActLoopConfig struct {
	Driver   *ai.Driver
	Store    *store.Store
	Engine   *RuleEngine
	Executor TypedExecutor
	WSIdx    *WSProtocolIndex
	Config   ReActConfig
	Gate     escalation.Gate
	Logger   *zap.Logger
	Embedder embed.Provider
	Project  string
}

// NewReActLoopWithGateWithConfig creates a ReAct execution loop with an explicit escalation gate using config.
func NewReActLoopWithGateWithConfig(cfg ReActLoopConfig) *ReActLoop {
	if cfg.Gate == nil {
		cfg.Gate = escalation.NoOpGate{}
	}

	loop := &ReActLoop{
		driver:      cfg.Driver,
		store:       cfg.Store,
		engine:      cfg.Engine,
		executor:    cfg.Executor,
		wsIdx:       cfg.WSIdx,
		recovery:    NewRecovery(cfg.Driver, cfg.Store, cfg.Config, cfg.Logger, cfg.Embedder),
		config:      cfg.Config,
		gate:        cfg.Gate,
		logger:      cfg.Logger,
		processMgr:  NewProcessManager(cfg.Logger),
		projectName: cfg.Project,
	}

	// Set project name on recovery if it implements the interface
	if cfg.Project != "" {
		loop.recovery.SetProject(cfg.Project)
	}

	return loop
}

// NewReActLoopWithConfig creates a ReAct execution loop with a no-op escalation gate using config.
func NewReActLoopWithConfig(cfg ReActLoopConfig) *ReActLoop {
	cfg.Gate = escalation.NoOpGate{}
	return NewReActLoopWithGateWithConfig(cfg)
}

// NewReActLoopWithGate creates a ReAct execution loop with an explicit escalation gate.
// Legacy function - maintained for backward compatibility. Use NewReActLoopWithGateWithConfig instead.
func NewReActLoopWithGate(
	driver *ai.Driver,
	store *store.Store,
	engine *RuleEngine,
	executor TypedExecutor,
	config ReActConfig,
	gate escalation.Gate,
	logger *zap.Logger,
) *ReActLoop {
	return NewReActLoopWithGateWithConfig(ReActLoopConfig{
		Driver:   driver,
		Store:    store,
		Engine:   engine,
		Executor: executor,
		Config:   config,
		Gate:     gate,
		Logger:   logger,
	})
}

// NewReActLoop creates a ReAct execution loop with a no-op escalation gate.
// Legacy function - maintained for backward compatibility. Use NewReActLoopWithConfig instead.
func NewReActLoop(
	driver *ai.Driver,
	store *store.Store,
	engine *RuleEngine,
	executor TypedExecutor,
	config ReActConfig,
	logger *zap.Logger,
) *ReActLoop {
	return NewReActLoopWithGateWithConfig(ReActLoopConfig{
		Driver:   driver,
		Store:    store,
		Engine:   engine,
		Executor: executor,
		Config:   config,
		Gate:     escalation.NoOpGate{},
		Logger:   logger,
	})
}

// SetProgressChannel sets an optional channel for real-time progress events.
// Sends are non-blocking — events are dropped if the channel is full.
func (r *ReActLoop) SetProgressChannel(ch chan<- ProgressEvent) {
	r.progressCh = ch
}

// emitProgress sends a progress event non-blocking.
func (r *ReActLoop) emitProgress(event ProgressEvent) {
	if r.progressCh == nil {
		return
	}
	event.Timestamp = time.Now()
	select {
	case r.progressCh <- event:
	default:
	}
}
