package session

import (
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
}
