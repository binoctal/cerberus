package autotest

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"

	"go.uber.org/zap"
)

// NewPythonCoverageProvider creates a new Python coverage provider
func NewPythonCoverageProvider(cfg *CoverageConfig) *PythonCoverageProvider {
	return &PythonCoverageProvider{
		config: cfg,
		logger: zap.NewNop(),
	}
}

// DefaultPythonCoverageRunner runs pytest with coverage and returns JSON report
func DefaultPythonCoverageRunner(ctx context.Context, projectDir string) ([]byte, error) {
	tmp, err := os.MkdirTemp("", "cerberus-python-cover-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)

	out := filepath.Join(tmp, "coverage.json")
	cmd := exec.CommandContext(ctx, "coverage", "run", "-m", "pytest", "--cov-report=json:"+out)
	cmd.Dir = projectDir

	if runErr := cmd.Run(); runErr != nil {
		_ = runErr // Coverage report might still exist
	}

	return os.ReadFile(out)
}
