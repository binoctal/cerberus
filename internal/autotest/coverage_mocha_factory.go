package autotest

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"

	"go.uber.org/zap"
)

// NewMochaCoverageProvider creates a new Mocha coverage provider. Pass nil run
// for the nil-runner guard (tests); callers wire the real default.
func NewMochaCoverageProvider(cfg *CoverageConfig, run coverageRunner, logger *zap.Logger) *MochaCoverageProvider {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &MochaCoverageProvider{config: cfg, run: run, logger: logger}
}

// DefaultMochaCoverageRunner runs nyc/mocha with coverage and returns the
// Istanbul JSON bytes. Mirrors DefaultMochaCoverageConfig: `npm test --
// --coverage --coverage-reporter=json`, reading the project-local
// coverage/coverage-final.json. Mirrors the fixed DefaultNodeCoverageRunner.
func DefaultMochaCoverageRunner(ctx context.Context, projectDir string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "npm", "test", "--", "--coverage", "--coverage-reporter=json")
	cmd.Dir = projectDir
	if runErr := cmd.Run(); runErr != nil {
		_ = runErr // coverage file may still exist
	}
	return os.ReadFile(filepath.Join(projectDir, "coverage", "coverage-final.json"))
}
