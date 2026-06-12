package agent

import (
	"github.com/binoctal/cerberus/internal/escalation"
	"github.com/binoctal/cerberus/internal/sandbox"
	"github.com/binoctal/cerberus/internal/types"
	"go.uber.org/zap"
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

// --- Built-in executor plugin wrappers ---

type httpPlugin struct{ executor *HTTPExecutor }

func (p *httpPlugin) Name() string           { return "http" }
func (p *httpPlugin) Executor() TypedExecutor { return p.executor }
func (p *httpPlugin) ActionTypes() []types.ActionType {
	return []types.ActionType{types.ActionAPIRequest, types.ActionNavigate}
}

type processPlugin struct{ executor *ProcessExecutor }

func (p *processPlugin) Name() string           { return "process" }
func (p *processPlugin) Executor() TypedExecutor { return p.executor }
func (p *processPlugin) ActionTypes() []types.ActionType {
	return []types.ActionType{types.ActionProcessExec, types.ActionProcessBuild}
}

type filePlugin struct{ executor *FileExecutor }

func (p *filePlugin) Name() string           { return "file" }
func (p *filePlugin) Executor() TypedExecutor { return p.executor }
func (p *filePlugin) ActionTypes() []types.ActionType {
	return []types.ActionType{types.ActionFileRead, types.ActionFileWrite, types.ActionFileExists, types.ActionFileGlob}
}

type mcpPlugin struct{ executor *MCPExecutor }

func (p *mcpPlugin) Name() string           { return "mcp" }
func (p *mcpPlugin) Executor() TypedExecutor { return p.executor }
func (p *mcpPlugin) ActionTypes() []types.ActionType {
	return []types.ActionType{types.ActionMCPCall}
}

type codePlugin struct{ executor *CodeExecutor }

func (p *codePlugin) Name() string           { return "code" }
func (p *codePlugin) Executor() TypedExecutor { return p.executor }
func (p *codePlugin) ActionTypes() []types.ActionType {
	return []types.ActionType{types.ActionCodeAnalyze, types.ActionCodeLint, types.ActionCodeSymbols}
}

type waitPlugin struct{ executor *WaitExecutor }

func (p *waitPlugin) Name() string           { return "wait" }
func (p *waitPlugin) Executor() TypedExecutor { return p.executor }
func (p *waitPlugin) ActionTypes() []types.ActionType {
	return []types.ActionType{types.ActionWait}
}

type browserPlugin struct{ executor *BrowserExecutor }

func (p *browserPlugin) Name() string           { return "browser" }
func (p *browserPlugin) Executor() TypedExecutor { return p.executor }
func (p *browserPlugin) ActionTypes() []types.ActionType {
	return []types.ActionType{types.ActionBrowserGoto, types.ActionBrowserClick,
		types.ActionBrowserFill, types.ActionBrowserEval}
}


type dbPlugin struct{ executor *DatabaseExecutor }

func (p *dbPlugin) Name() string            { return "database" }
func (p *dbPlugin) Executor() TypedExecutor { return p.executor }
func (p *dbPlugin) ActionTypes() []types.ActionType {
	return []types.ActionType{types.ActionDBQuery, types.ActionDBAssert}
}

type graphqlPlugin struct{ executor *GraphQLExecutor }

func (p *graphqlPlugin) Name() string            { return "graphql" }
func (p *graphqlPlugin) Executor() TypedExecutor { return p.executor }
func (p *graphqlPlugin) ActionTypes() []types.ActionType {
	return []types.ActionType{types.ActionGraphQLQuery}
}

type wsPlugin struct{ executor *WebSocketExecutor }

func (p *wsPlugin) Name() string            { return "websocket" }
func (p *wsPlugin) Executor() TypedExecutor { return p.executor }
func (p *wsPlugin) ActionTypes() []types.ActionType {
	return []types.ActionType{types.ActionWSConnect, types.ActionWSSend}
}

// --- ExtendedRuleEngine ---

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

// --- Registry helpers ---

// BuiltinExecutorPlugins returns the default set of executor plugins.
// These are the same plugins that BuildMultiExecutor registers internally.
func BuiltinExecutorPlugins(projectDir string, logger *zap.Logger) []ExecutorPlugin {
	return []ExecutorPlugin{
		&httpPlugin{executor: NewHTTPExecutor(logger)},
		&waitPlugin{executor: NewWaitExecutor()},
	}
}

// BuiltinPluginsWithSandbox returns all built-in plugins including those
// that require sandbox and optional dependencies.
func BuiltinPluginsWithSandbox(projectDir string, sb sandbox.Sandbox, gate escalation.Gate, logger *zap.Logger) []ExecutorPlugin {
	plugins := BuiltinExecutorPlugins(projectDir, logger)
	plugins = append(plugins,
		&processPlugin{executor: NewProcessExecutor(sb, logger)},
		&filePlugin{executor: NewFileExecutor(projectDir, logger)},
		&mcpPlugin{executor: NewMCPExecutor(nil, logger)},
		&codePlugin{executor: NewCodeExecutor(sb, logger)},
		&dbPlugin{executor: NewDatabaseExecutor(logger)},
		&graphqlPlugin{executor: NewGraphQLExecutor(logger)},
		&wsPlugin{executor: NewWebSocketExecutor(logger)},
	)

	// Browser executor (optional — requires playwright binary).
	if browserExec, err := NewBrowserExecutor(logger); err == nil {
		plugins = append(plugins, &browserPlugin{executor: browserExec})
		logger.Info("browser plugin registered (playwright)")
	} else {
		logger.Warn("browser plugin unavailable (install playwright)", zap.Error(err))
	}

	return plugins
}
