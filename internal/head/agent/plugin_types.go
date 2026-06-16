package agent

import (
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/types"
)

// ExecutorPlugin bundles an executor with its action types for registration.
type ExecutorPlugin interface {
	// Name returns a unique identifier for this plugin.
	Name() string
	// Executor returns the TypedExecutor that handles actions for this plugin.
	Executor() TypedExecutor
	// ActionTypes returns the action types this plugin handles.
	ActionTypes() []types.ActionType
}

// RulePlugin adds custom rule matching logic beyond the built-in rules.
type RulePlugin interface {
	// Name returns a unique identifier for this plugin.
	Name() string
	// Match attempts to produce a deterministic TypedAction for the given TestCase.
	// Returns the action and true if matched, nil and false otherwise.
	Match(tc TestCase) (types.TypedAction, bool)
}

// PluginRegistry manages ExecutorPlugin and RulePlugin registrations.
type PluginRegistry struct {
	executors []ExecutorPlugin
	rules     []RulePlugin
	logger    *zap.Logger
}
