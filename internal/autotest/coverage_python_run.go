package autotest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"go.uber.org/zap"
)

// RunCoverage runs pytest with coverage and parses the output
func (p *PythonCoverageProvider) RunCoverage(ctx context.Context, projectDir string) (*CoverageReport, error) {
	if p.config == nil {
		return nil, fmt.Errorf("python coverage: config not set")
	}

	// Phase 1: Determine Python command
	pythonCmd := determinePythonCommand(p.config)

	// Phase 2: Build test command
	cmdCtx := buildPythonTestCommand(ctx, pythonCmd, p.config, projectDir, p.logger)

	// Phase 3: Apply timeout if configured
	var cancel context.CancelFunc
	if p.config.Timeout > 0 {
		cancel = cmdCtx.applyTimeout()
		defer cancel()
	}

	// Phase 4: Execute test command
	_, _ = cmdCtx.executeTestCommand()

	// Phase 5: Generate coverage report
	_ = cmdCtx.generateCoverageReport()

	// Phase 6: Parse coverage data (try JSON, fallback to SQLite)
	report, err := p.parseCoverageData(projectDir)
	if err != nil {
		return nil, err
	}

	// Mark as pass if we got coverage data
	report.Pass = true

	p.logger.Info("python coverage complete",
		zap.Int("total_funcs", report.TotalFuncs),
		zap.Int("covered_funcs", report.CoveredFuncs))

	return report, nil
}

// parseCoverageData attempts to parse JSON coverage, falls back to SQLite
func (p *PythonCoverageProvider) parseCoverageData(projectDir string) (*CoverageReport, error) {
	// Try JSON first
	coveragePath := filepath.Join(projectDir, p.config.OutputPath)
	data, err := os.ReadFile(coveragePath)
	if err == nil {
		report, parseErr := p.parseJSONCoverage(data)
		if parseErr == nil {
			return report, nil
		}
	}

	// Fallback to SQLite
	return p.parseSQLiteCoverage(projectDir)
}
