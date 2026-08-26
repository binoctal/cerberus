package agent

import (
	"context"
	"fmt"
	"path/filepath"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/escalation"
	"github.com/binoctal/cerberus/internal/policy"
	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/sandbox"
	"github.com/binoctal/cerberus/internal/types"
)

// TypedExecutor executes a TypedAction and returns an ExecutorResult.
type TypedExecutor interface {
	Execute(ctx context.Context, action types.TypedAction) types.ExecutorResult
}

// MultiExecutor dispatches TypedActions to specialized executors via plugin registry.
type MultiExecutor struct {
	executors map[types.ActionType]TypedExecutor
	policy    policy.ActionPolicy
	sandbox   sandbox.Sandbox
	gate      escalation.Gate
	anomaly   *policy.AnomalyDetector
	logger    *zap.Logger
}

// NewMultiExecutor creates a multi-executor with policy, sandbox, and escalation layers.
func NewMultiExecutor(
	p policy.ActionPolicy,
	sb sandbox.Sandbox,
	gate escalation.Gate,
	logger *zap.Logger,
) *MultiExecutor {
	return &MultiExecutor{
		executors: make(map[types.ActionType]TypedExecutor),
		policy:    p,
		sandbox:   sb,
		gate:      gate,
		anomaly:   policy.NewDefaultAnomalyDetector(),
		logger:    logger,
	}
}

// Register maps an executor to one or more action types.
func (m *MultiExecutor) Register(executor TypedExecutor, actionTypes ...types.ActionType) {
	for _, t := range actionTypes {
		m.executors[t] = executor
		m.logger.Info("registered executor", zap.String("action_type", string(t)))
	}
}

// Execute runs the full pipeline: policy → sandbox → route → anomaly detection.
func (m *MultiExecutor) Execute(ctx context.Context, action types.TypedAction) types.ExecutorResult {
	// Layer 1: Policy validation.
	if err := m.policy.Validate(action); err != nil {
		return types.ErrorResult{Err: fmt.Sprintf("policy denied: %v", err)}
	}

	// Layer 2: Sandbox isolation.
	sbPolicy := m.sandboxPolicyFor(action)
	ctx, cleanup, err := m.sandbox.Apply(ctx, sbPolicy)
	if err != nil {
		return types.ErrorResult{Err: fmt.Sprintf("sandbox apply: %v", err)}
	}
	defer cleanup()

	// Layer 3: Route to executor.
	executor, ok := m.executors[action.GetActionType()]
	if !ok {
		return types.ErrorResult{Err: fmt.Sprintf("no executor for action type: %s", action.GetActionType())}
	}
	result := executor.Execute(ctx, action)

	// Layer 4: Anomaly detection.
	if m.anomaly.Check(result) {
		m.gate.Check(ctx, escalation.Event{
			Type:    "anomalous_result",
			Message: result.Summary(),
			Data:    map[string]any{"action_type": string(action.GetActionType())},
		})
	}

	return result
}

// BuildMultiExecutor assembles the standard executor with all built-in executor plugins.
// Attempts to use Linux sandbox isolation; falls back to NoOpSandbox if unavailable.
// Loads optional policy overrides from .cerberus/policy.yaml.
func BuildMultiExecutor(projectDir string, serviceHeaders map[string]map[string]string, wsIdx *WSProtocolIndex, gate escalation.Gate, logger *zap.Logger) *MultiExecutor {
	p := policy.NewDefaultActionPolicy(projectDir)

	// Load optional policy overrides.
	if overrides, err := policy.LoadPolicyConfig(filepath.Join(projectDir, ".cerberus", "policy.yaml")); err != nil {
		logger.Warn("failed to load policy config", zap.Error(err))
	} else if overrides != nil {
		overrides.Apply(p)
		logger.Info("applied policy overrides from .cerberus/policy.yaml")
	}

	sb := sandbox.Sandbox(sandbox.NoOpSandbox{})
	if linuxSB := sandbox.TryNewLinuxSandbox(logger); linuxSB != nil {
		sb = linuxSB
	}
	if gate == nil {
		gate = escalation.NoOpGate{}
	}
	multi := NewMultiExecutor(p, sb, gate, logger)

	// Register built-in executor plugins.
	registry := NewPluginRegistry(logger)
	for _, plugin := range BuiltinPluginsWithSandbox(projectDir, serviceHeaders, wsIdx, sb, gate, logger) {
		registry.RegisterExecutor(plugin)
	}
	registry.ApplyTo(multi)

	// Browser leg session injection (spec §5): when a service declares a ui
	// vocabulary, log in as its auth actor and seed the browser's
	// localStorage once per run. Best-effort — on failure the UI cases run
	// unauthenticated and their assertions fail loudly (spec §8 keeps the
	// leg alive rather than aborting the run).
	if be := multi.BrowserExec(); be != nil {
		initBrowserSessionForProject(context.Background(), be, projectDir, logger)
	}

	return multi
}

// initBrowserSessionForProject loads the project config, finds the first
// service with a ui vocabulary, and runs the session injection against its
// API base URL (the service URL with ws:// normalized to http://).
func initBrowserSessionForProject(ctx context.Context, be *BrowserExecutor, projectDir string, logger *zap.Logger) {
	cfg, err := project.LoadFromFile(filepath.Join(projectDir, ".cerberus", "project.yaml"))
	if err != nil {
		logger.Warn("browser session: project config unavailable", zap.Error(err))
		return
	}
	for _, svc := range cfg.Services {
		if svc.Vocabulary == nil || svc.Vocabulary.UI == nil {
			continue
		}
		ui := svc.Vocabulary.UI
		actorName := ui.AuthActor
		if actorName == "" {
			actorName = "web-actor"
		}
		var actor project.Actor
		found := false
		for _, a := range cfg.Actors {
			if a.Name == actorName {
				actor, found = a, true
				break
			}
		}
		if !found {
			logger.Warn("browser session: auth actor not found", zap.String("actor", actorName))
			return
		}
		apiBase := httpBaseURL(svc.URL)
		if err := be.InitBrowserSession(ctx, ui, actor, apiBase); err != nil {
			logger.Warn("browser session injection failed — UI cases run unauthenticated", zap.Error(err))
		}
		return
	}
}

// BrowserExec returns the registered browser executor, or nil when the
// playwright plugin is unavailable (registration is single-threaded setup;
// reads happen after ApplyTo).
func (m *MultiExecutor) BrowserExec() *BrowserExecutor {
	if e, ok := m.executors[types.ActionBrowserGoto]; ok {
		if be, ok := e.(*BrowserExecutor); ok {
			return be
		}
	}
	return nil
}
