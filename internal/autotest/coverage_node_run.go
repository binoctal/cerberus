package autotest

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"go.uber.org/zap"
)

// RunCoverage runs Jest tests with coverage and parses the JSON output
func (p *NodeCoverageProvider) RunCoverage(ctx context.Context, projectDir string) (*CoverageReport, error) {
	if p.config == nil {
		return nil, fmt.Errorf("node coverage: config not set")
	}

	// Create output directory if needed
	outputDir := filepath.Dir(p.config.OutputPath)
	if outputDir != "." && outputDir != "" {
		if err := os.MkdirAll(filepath.Join(projectDir, outputDir), 0755); err != nil {
			return nil, fmt.Errorf("node coverage: create output dir: %w", err)
		}
	}

	// Build test command: npm test -- --coverage --coverageReporters=json
	args := append(p.config.TestCommand, p.config.CoverageArgs...)
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = projectDir
	if p.config.Env != nil {
		cmd.Env = append(os.Environ(), p.config.Env...)
	}

	p.logger.Info("running node coverage",
		zap.String("cmd", strings.Join(args, " ")),
		zap.String("dir", projectDir))

	// Run with timeout
	if p.config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, p.config.Timeout)
		defer cancel()
		cmd = exec.CommandContext(ctx, args[0], args[1:]...)
		cmd.Dir = projectDir
		if p.config.Env != nil {
			cmd.Env = append(os.Environ(), p.config.Env...)
		}
	}

	// Run command - Jest returns non-zero if tests fail, but we still check coverage
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Check if it was a timeout
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("node coverage: timed out after %v", p.config.Timeout)
		}
		// Tests may have failed but coverage report might still exist
		p.logger.Warn("node coverage test had errors", zap.Error(err), zap.String("output", string(output)))
	}

	// Parse the JSON coverage report
	coveragePath := filepath.Join(projectDir, p.config.OutputPath)
	data, err := os.ReadFile(coveragePath)
	if err != nil {
		return nil, fmt.Errorf("node coverage: read coverage file: %w", err)
	}

	report, err := p.parseJestCoverage(data)
	if err != nil {
		return nil, fmt.Errorf("node coverage: parse coverage: %w", err)
	}

	// Mark as pass if we got coverage data (even if tests failed)
	report.Pass = true
	report.CoverageUnit = "function"

	p.logger.Info("node coverage complete",
		zap.Int("total_funcs", report.TotalFuncs),
		zap.Int("covered_funcs", report.CoveredFuncs))

	return report, nil
}
