package agent

import (
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/escalation"
	"github.com/binoctal/cerberus/internal/sandbox"
)

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
