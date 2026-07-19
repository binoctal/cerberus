package autotest

import (
	"context"

	"go.uber.org/zap"
)

// NewCoverageProviderForLanguage creates the correct coverage provider for a
// detected language. Reused by AutoTest (run_phases_autotest) and the
// Examiner coverage step (run_phases_examiner).
//
// For Go: pass a coverageRunner func(ctx, projectDir) ([]byte, error).
// For Node/Python: pass nil runner (providers use CoverageConfig with embedded logic).
func NewCoverageProviderForLanguage(lang string, runner interface{}, logger *zap.Logger) CoverageProvider {
	switch lang {
	case "node":
		return NewNodeCoverageProvider(DefaultNodeCoverageConfig(), DefaultNodeCoverageRunner, logger)
	case "python":
		return NewPythonCoverageProvider(DefaultPythonCoverageConfig(), DefaultPythonCoverageRunner, logger)
	default: // "go" or fallback
		if runner == nil {
			runner = DefaultGoCoverageRunner
		}
		if cr, ok := runner.(func(context.Context, string) ([]byte, error)); ok {
			return NewGoCoverageProvider(cr, logger)
		}
		// Fallback to default runner
		return NewGoCoverageProvider(DefaultGoCoverageRunner, logger)
	}
}
