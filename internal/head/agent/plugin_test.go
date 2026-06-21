package agent

import (
	"testing"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/escalation"
	"github.com/binoctal/cerberus/internal/policy"
	"github.com/binoctal/cerberus/internal/sandbox"
	"github.com/binoctal/cerberus/internal/types"
)

func TestPluginRegistryRegistersExecutors(t *testing.T) {
	logger := zap.NewNop()
	registry := NewPluginRegistry(logger)

	registry.RegisterExecutor(&httpPlugin{executor: NewHTTPExecutor(logger)})
	registry.RegisterExecutor(&waitPlugin{executor: NewWaitExecutor()})

	plugins := registry.ExecutorPlugins()
	if len(plugins) != 2 {
		t.Fatalf("expected 2 plugins, got %d", len(plugins))
	}
	if plugins[0].Name() != "http" {
		t.Errorf("expected first plugin 'http', got %s", plugins[0].Name())
	}
	if plugins[1].Name() != "wait" {
		t.Errorf("expected second plugin 'wait', got %s", plugins[1].Name())
	}
}

func TestPluginRegistryRegistersRules(t *testing.T) {
	registry := NewPluginRegistry(zap.NewNop())
	registry.RegisterRule(&testRulePlugin{name: "custom"})

	rules := registry.RulePlugins()
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule plugin, got %d", len(rules))
	}
	if rules[0].Name() != "custom" {
		t.Errorf("expected plugin 'custom', got %s", rules[0].Name())
	}
}

func TestApplyToRegistersAllPlugins(t *testing.T) {
	logger := zap.NewNop()
	registry := NewPluginRegistry(logger)
	registry.RegisterExecutor(&httpPlugin{executor: NewHTTPExecutor(logger)})
	registry.RegisterExecutor(&waitPlugin{executor: NewWaitExecutor()})

	multi := NewMultiExecutor(
		policy.NewDefaultActionPolicy("."),
		sandbox.NoOpSandbox{},
		escalation.NoOpGate{},
		logger,
	)
	registry.ApplyTo(multi)

	// Verify http actions are registered.
	httpActions := []types.ActionType{types.ActionAPIRequest, types.ActionNavigate}
	for _, at := range httpActions {
		if _, ok := multi.executors[at]; !ok {
			t.Errorf("expected executor for %s", at)
		}
	}
	// Verify wait action is registered.
	if _, ok := multi.executors[types.ActionWait]; !ok {
		t.Error("expected executor for wait")
	}
}

func TestExtendedRuleEnginePluginFirst(t *testing.T) {
	inner := NewRuleEngine("http://localhost:8080", nil, ".")
	registry := NewPluginRegistry(zap.NewNop())

	customPlugin := &testRulePlugin{
		name:    "custom_matcher",
		matches: map[string]bool{"special_target": true},
	}
	registry.RegisterRule(customPlugin)

	engine := NewExtendedRuleEngine(inner, registry.RulePlugins())

	tc := TestCase{ID: "t1", Target: "special_target", Method: "GET"}
	action, matched := engine.Match(tc)
	if !matched {
		t.Error("expected custom plugin to match")
	}
	if action == nil {
		t.Error("expected non-nil action from custom plugin")
	}
}

func TestExtendedRuleEngineFallback(t *testing.T) {
	inner := NewRuleEngine("http://localhost:8080", nil, ".")
	registry := NewPluginRegistry(zap.NewNop())
	registry.RegisterRule(&testRulePlugin{name: "no_match"})

	engine := NewExtendedRuleEngine(inner, registry.RulePlugins())

	// Should fall through to built-in rules (Rule 1: API test with method + path).
	tc := TestCase{ID: "t1", Target: "/api/users", Method: "GET"}
	action, matched := engine.Match(tc)
	if !matched {
		t.Error("expected built-in rule to match")
	}
	if http, ok := action.(types.HTTPAction); !ok {
		t.Error("expected HTTPAction from built-in rule")
	} else if http.Method != "GET" {
		t.Errorf("expected GET, got %s", http.Method)
	}
}

func TestExtendedRuleEngineStats(t *testing.T) {
	inner := NewRuleEngine("http://localhost:8080", nil, ".")
	engine := NewExtendedRuleEngine(inner, nil)

	hits, misses := engine.Stats()
	if hits != 0 || misses != 0 {
		t.Errorf("expected zero stats, got hits=%d misses=%d", hits, misses)
	}

	tc := TestCase{ID: "t1", Target: "/api", Method: "GET"}
	engine.Match(tc)
	hits, _ = engine.Stats()
	if hits != 1 {
		t.Errorf("expected 1 hit, got %d", hits)
	}
}

func TestBuiltinPluginsWithSandboxCount(t *testing.T) {
	logger := zap.NewNop()
	plugins := BuiltinPluginsWithSandbox(".", nil, nil, nil, logger)

	// At minimum: http, wait, process, file, mcp, code = 6 (browser optional).
	if len(plugins) < 6 {
		t.Errorf("expected at least 6 built-in plugins, got %d", len(plugins))
	}

	names := map[string]bool{}
	for _, p := range plugins {
		if p.Name() == "" {
			t.Error("plugin has empty name")
		}
		if len(p.ActionTypes()) == 0 {
			t.Errorf("plugin %s has no action types", p.Name())
		}
		names[p.Name()] = true
	}

	for _, name := range []string{"http", "process", "file", "mcp", "code", "wait"} {
		if !names[name] {
			t.Errorf("missing built-in plugin: %s", name)
		}
	}
}

// testRulePlugin is a simple RulePlugin for testing.
type testRulePlugin struct {
	name    string
	matches map[string]bool
}

func (p *testRulePlugin) Name() string { return p.name }
func (p *testRulePlugin) Match(tc TestCase) (types.TypedAction, bool) {
	if p.matches[tc.Target] {
		return types.HTTPAction{Method: "GET", URL: "http://custom/" + tc.Target}, true
	}
	return nil, false
}
