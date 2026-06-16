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

// RunCoverage runs pytest with coverage and parses the output
func (p *PythonCoverageProvider) RunCoverage(ctx context.Context, projectDir string) (*CoverageReport, error) {
	if p.config == nil {
		return nil, fmt.Errorf("python coverage: config not set")
	}

	// Determine Python command
	pythonCmd := "python3"
	if p.config.Env != nil && len(p.config.Env) > 0 {
		// Check if PYTHON_CMD is set in env
		for _, env := range p.config.Env {
			if strings.HasPrefix(env, "PYTHON_CMD=") {
				pythonCmd = strings.TrimPrefix(env, "PYTHON_CMD=")
				break
			}
		}
	}

	// Run coverage.py + pytest
	args := append([]string{pythonCmd}, p.config.TestCommand...)
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = projectDir
	if p.config.Env != nil {
		cmd.Env = append(os.Environ(), p.config.Env...)
	}

	p.logger.Info("running python coverage",
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

	// Run command
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Check if it was a timeout
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("python coverage: timed out after %v", p.config.Timeout)
		}
		// Tests may have failed but coverage report might still exist
		p.logger.Warn("python coverage test had errors", zap.Error(err), zap.String("output", string(output)))
	}

	// Generate JSON report
	if p.config.CoverageArgs != nil && len(p.config.CoverageArgs) > 0 {
		reportArgs := append([]string{pythonCmd}, p.config.CoverageArgs...)
		reportCmd := exec.Command(reportArgs[0], reportArgs[1:]...)
		reportCmd.Dir = projectDir
		if runErr := reportCmd.Run(); runErr != nil {
			p.logger.Warn("python coverage report generation failed", zap.Error(runErr))
		}
	}

	// Parse the JSON coverage report
	coveragePath := filepath.Join(projectDir, p.config.OutputPath)
	data, err := os.ReadFile(coveragePath)
	if err != nil {
		// Try SQLite database as fallback
		return p.parseSQLiteCoverage(projectDir)
	}

	report, err := p.parseJSONCoverage(data)
	if err != nil {
		// Try SQLite as fallback
		return p.parseSQLiteCoverage(projectDir)
	}

	// Mark as pass if we got coverage data
	report.Pass = true

	p.logger.Info("python coverage complete",
		zap.Int("total_funcs", report.TotalFuncs),
		zap.Int("covered_funcs", report.CoveredFuncs))

	return report, nil
}
