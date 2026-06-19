package autotest

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"

	"go.uber.org/zap"
)

// NewNodeCoverageProvider creates a new Node coverage provider
func NewNodeCoverageProvider(cfg *CoverageConfig) *NodeCoverageProvider {
	return &NodeCoverageProvider{
		config: cfg,
		logger: zap.NewNop(),
	}
}

// DefaultNodeCoverageRunner runs Jest with coverage and returns JSON report
func DefaultNodeCoverageRunner(ctx context.Context, projectDir string) ([]byte, error) {
	tmp, err := os.MkdirTemp("", "cerberus-node-cover-")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	out := filepath.Join(tmp, "coverage-final.json")
	cmd := exec.CommandContext(ctx, "npm", "test", "--", "--coverage", "--coverageReporters=json", "--outputCoverage="+out)
	cmd.Dir = projectDir

	if runErr := cmd.Run(); runErr != nil {
		// Coverage report might still exist
		_ = runErr
	}

	return os.ReadFile(out)
}
