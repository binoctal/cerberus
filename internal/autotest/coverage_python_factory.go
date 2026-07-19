package autotest

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"

	"go.uber.org/zap"
)

// NewPythonCoverageProvider creates a new Python coverage provider. Pass nil run
// for the nil-runner guard (tests); the factory wires the real default.
func NewPythonCoverageProvider(cfg *CoverageConfig, run coverageRunner, logger *zap.Logger) *PythonCoverageProvider {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &PythonCoverageProvider{config: cfg, run: run, logger: logger}
}

// DefaultPythonCoverageRunner runs pytest with coverage and returns the JSON
// report bytes. Mirrors DefaultPythonCoverageConfig: `pytest --cov
// --cov-report=term --cov-report=json:coverage.json`, reading the project-local
// coverage.json. pytest returns non-zero on test failure but still writes the
// report.
func DefaultPythonCoverageRunner(ctx context.Context, projectDir string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "pytest", "--cov", "--cov-report=term", "--cov-report=json:coverage.json")
	cmd.Dir = projectDir
	if runErr := cmd.Run(); runErr != nil {
		_ = runErr // report may still exist
	}
	return os.ReadFile(filepath.Join(projectDir, "coverage.json"))
}
