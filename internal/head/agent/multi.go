package agent

import (
	"context"
	"fmt"

	"github.com/binoctal/cerberus/internal/escalation"
	"github.com/binoctal/cerberus/internal/policy"
	"github.com/binoctal/cerberus/internal/sandbox"
	"github.com/binoctal/cerberus/internal/types"
	"go.uber.org/zap"
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

func (m *MultiExecutor) sandboxPolicyFor(action types.TypedAction) sandbox.Policy {
	switch action.GetActionType() {
	case types.ActionProcessExec, types.ActionProcessBuild:
		a := action.(types.ProcessExecAction)
		return sandbox.DefaultProcessPolicy(a.WorkDir)
	case types.ActionFileRead, types.ActionFileWrite, types.ActionFileExists, types.ActionFileGlob:
		return sandbox.DefaultFilePolicy(".")
	case types.ActionMCPCall:
		return sandbox.DefaultMCPPolicy()
	case types.ActionCodeAnalyze, types.ActionCodeLint, types.ActionCodeSymbols:
		return sandbox.DefaultCodePolicy(".")
	default:
		return sandbox.DefaultHTTPPolicy()
	}
}

// BuildMultiExecutor assembles the standard executor with all built-in executors.
// Attempts to use Linux sandbox isolation; falls back to NoOpSandbox if unavailable.
func BuildMultiExecutor(projectDir string, gate escalation.Gate, logger *zap.Logger) *MultiExecutor {
	p := policy.NewDefaultActionPolicy(projectDir)
	sb := sandbox.Sandbox(sandbox.NoOpSandbox{})
	if linuxSB := sandbox.TryNewLinuxSandbox(logger); linuxSB != nil {
		sb = linuxSB
	}
	if gate == nil {
		gate = escalation.NoOpGate{}
	}
	multi := NewMultiExecutor(p, sb, gate, logger)

	multi.Register(NewHTTPExecutor(logger), types.ActionAPIRequest, types.ActionNavigate)
	multi.Register(NewProcessExecutor(logger), types.ActionProcessExec, types.ActionProcessBuild)
	multi.Register(NewFileExecutor(projectDir, logger), types.ActionFileRead, types.ActionFileWrite, types.ActionFileExists, types.ActionFileGlob)
	multi.Register(NewMCPExecutor(nil, logger), types.ActionMCPCall)
	multi.Register(NewCodeExecutor(logger), types.ActionCodeAnalyze, types.ActionCodeLint, types.ActionCodeSymbols)
	multi.Register(NewWaitExecutor(), types.ActionWait)

	return multi
}
