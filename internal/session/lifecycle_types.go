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
	// coverage. If nil, the default behavior independently runs the language-
	// specific coverage provider (go test/jest/pytest) for the measurement; it
	// does NOT reuse the AutoTest report. Tests inject a stub to avoid
	// recursively running those subprocesses when ProjectDir is itself a module
	// under test.
	CoverageFn func(ctx context.Context, sess *Session) contract.CoverageMeasurement
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

	// RepairedCoverage is the post-AutoTest-dispatch coverage (Agent + AutoTest
	// tests), measured inside the coverage repair loop. It is a SEPARATE track
	// from Assessment (the Agent-only gate) so the Agent verdict is never
	// overwritten by AutoTest files. Observability-only (D1 invariant, spec §5.3).
	RepairedCoverage *contract.CoverageMeasurement
	// CoverageRecovered is observability-only: RepairedCoverage met the contract
	// threshold. It does NOT flip the Agent Assessment or any case verdict
	// (spec §5.3, §6.6 [R10]).
	CoverageRecovered bool

	// repairTargeted is the in-memory set of gaps the coverage repair loop has
	// already dispatched AutoTest for this run (set by the loop, read by the
	// Phase-4 gap exclusion). NOT persisted — resume does not re-run the loop
	// (spec §8).
	repairTargeted map[coverKey]bool

	// harness manages real-process actors (fidelity: real-process) for this
	// session; nil when no actor is real-process or before setup.
	harness *harness

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
	coverageFn func(ctx context.Context, sess *Session) contract.CoverageMeasurement
}
