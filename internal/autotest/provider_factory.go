package autotest

import (
	"context"

	"go.uber.org/zap"
)

// NewCoverageProviderForLanguage creates the correct coverage provider for a
// detected language. Reused by AutoTest (run_phases_autotest) and the Examiner
// coverage step (coverage.go). Pass nil runner to use each language's Default.
func NewCoverageProviderForLanguage(lang string, runner func(context.Context, string) ([]byte, error), logger *zap.Logger) CoverageProvider {
	switch lang {
	case "node":
		if runner == nil {
			runner = DefaultNodeCoverageRunner
		}
		return NewNodeCoverageProvider(DefaultNodeCoverageConfig(), runner, logger)
	case "python":
		if runner == nil {
			runner = DefaultPythonCoverageRunner
		}
		return NewPythonCoverageProvider(DefaultPythonCoverageConfig(), runner, logger)
	default: // "go" or fallback
		if runner == nil {
			runner = DefaultGoCoverageRunner
		}
		return NewGoCoverageProvider(runner, logger)
	}
}
