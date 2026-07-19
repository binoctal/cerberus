package autotest

import (
	"context"
	"fmt"

	"go.uber.org/zap"
)

// RunCoverage invokes the injected runner and parses the returned Istanbul JSON.
// The runner owns exec and reading the coverage file; timeout is the caller's responsibility via ctx.
func (p *MochaCoverageProvider) RunCoverage(ctx context.Context, projectDir string) (*CoverageReport, error) {
	if p.config == nil {
		return nil, fmt.Errorf("mocha coverage: config not set")
	}
	if p.run == nil {
		return nil, fmt.Errorf("mocha coverage: runner not configured")
	}
	data, err := p.run(ctx, projectDir)
	if err != nil {
		return nil, fmt.Errorf("mocha coverage: run failed: %w", err)
	}
	report, err := p.parseIstanbulCoverage(data)
	if err != nil {
		return nil, fmt.Errorf("mocha coverage: parse: %w", err)
	}
	report.Pass = true
	report.CoverageUnit = "function"
	p.logger.Info("mocha coverage complete",
		zap.Int("total_funcs", report.TotalFuncs),
		zap.Int("covered_funcs", report.CoveredFuncs))
	return report, nil
}
