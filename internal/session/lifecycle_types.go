package session

import (
	"context"
	"time"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/autotest"
	"github.com/binoctal/cerberus/internal/config"
	"github.com/binoctal/cerberus/internal/escalation"
	"github.com/binoctal/cerberus/internal/head/contract"
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

	// CoverageFn optionally overrides how the Examiner phase obtains real line
	// coverage. If nil, the default behavior reuses the AutoTest report when
	// available, else independently runs the language-specific coverage provider
	// (go test/jest/pytest). Tests inject a stub to avoid recursively running
	// those subprocesses when ProjectDir is itself a module under test.
	CoverageFn func(ctx context.Context, sess *Session) float64
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

	// Coverage contract (built by Scout, assessed by Examiner)
	Contract   *contract.Contract
	Assessment *contract.Assessment

	// Per-head drivers. When nil, the shared Driver is used.
	scoutDriver    *ai.Driver
	agentDriver    *ai.Driver
	examinerDriver *ai.Driver
	criticDriver   *ai.Driver
	tiers          config.TierModels // head → tier model, for context lookup

	// clientFactory creates LLM clients for per-head drivers. If nil, uses llm.NewClientWithConfig.
	// Injected by tests to provide mock clients.
	clientFactory func(llm.ClientConfig) (llm.Client, error)

	// coverageFn overrides Examiner-phase coverage retrieval. If nil, the
	// package-level coverageForSession is used. Mirrors clientFactory: injected
	// by tests via SessionConfig.CoverageFn to avoid real coverage subprocesses.
	coverageFn func(ctx context.Context, sess *Session) float64
}
