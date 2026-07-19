package autotest

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"

	"go.uber.org/zap"
)

// NewNodeCoverageProvider creates a new Node coverage provider. Pass nil run
// to use the RunCoverage nil-runner guard (tests); the factory wires the real
// default. Matches GoCoverageProvider's shape.
func NewNodeCoverageProvider(cfg *CoverageConfig, run coverageRunner, logger *zap.Logger) *NodeCoverageProvider {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &NodeCoverageProvider{config: cfg, run: run, logger: logger}
}

// DefaultNodeCoverageRunner runs Jest with coverage and returns the JSON report
// bytes. It mirrors what the old inline RunCoverage did: `npm test -- --coverage
// --coverageReporters=json`, reading Jest's default output at
// <projectDir>/coverage/coverage-final.json. Jest returns non-zero on test
// failure but still writes the coverage file.
func DefaultNodeCoverageRunner(ctx context.Context, projectDir string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "npm", "test", "--", "--coverage", "--coverageReporters=json")
	cmd.Dir = projectDir
	if runErr := cmd.Run(); runErr != nil {
		_ = runErr // coverage file may still exist
	}
	return os.ReadFile(filepath.Join(projectDir, "coverage", "coverage-final.json"))
}
