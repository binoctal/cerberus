package agent

import (
	"github.com/binoctal/cerberus/internal/types"
)

// ExtendedRuleEngine delegates to RulePlugins first, then falls back to built-in rules.
type ExtendedRuleEngine struct {
	plugins []RulePlugin
	inner   *RuleEngine
}

// NewExtendedRuleEngine wraps a RuleEngine with plugin support.
func NewExtendedRuleEngine(inner *RuleEngine, plugins []RulePlugin) *ExtendedRuleEngine {
	return &ExtendedRuleEngine{inner: inner, plugins: plugins}
}

// Match tries each RulePlugin first, then falls back to built-in rules.
func (e *ExtendedRuleEngine) Match(tc TestCase) (types.TypedAction, bool) {
	for _, p := range e.plugins {
		if action, ok := p.Match(tc); ok {
			return action, true
		}
	}
	return e.inner.Match(tc)
}

// Stats delegates to the inner RuleEngine stats.
func (e *ExtendedRuleEngine) Stats() (hits, misses int64) {
	return e.inner.Stats()
}
