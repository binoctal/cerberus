package agent

import (
	"go.uber.org/zap"
)

// PluginRegistry manages ExecutorPlugin and RulePlugin registrations.
type PluginRegistry struct {
	executors []ExecutorPlugin
	rules     []RulePlugin
	logger    *zap.Logger
}

// NewPluginRegistry creates an empty plugin registry.
func NewPluginRegistry(logger *zap.Logger) *PluginRegistry {
	return &PluginRegistry{logger: logger}
}

// RegisterExecutor adds an ExecutorPlugin to the registry.
func (r *PluginRegistry) RegisterExecutor(p ExecutorPlugin) {
	r.executors = append(r.executors, p)
	r.logger.Info("registered executor plugin", zap.String("name", p.Name()))
}

// RegisterRule adds a RulePlugin to the registry.
func (r *PluginRegistry) RegisterRule(p RulePlugin) {
	r.rules = append(r.rules, p)
	r.logger.Info("registered rule plugin", zap.String("name", p.Name()))
}

// ApplyTo registers all executor plugins into the given MultiExecutor.
func (r *PluginRegistry) ApplyTo(multi *MultiExecutor) {
	for _, p := range r.executors {
		multi.Register(p.Executor(), p.ActionTypes()...)
	}
}

// RulePlugins returns all registered rule plugins.
func (r *PluginRegistry) RulePlugins() []RulePlugin {
	return r.rules
}

// ExecutorPlugins returns all registered executor plugins.
func (r *PluginRegistry) ExecutorPlugins() []ExecutorPlugin {
	return r.executors
}
