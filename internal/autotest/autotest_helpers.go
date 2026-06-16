package autotest

import "context"

// afterCoverageOr runs coverage after test generation, returns fallback on error
func (a *AutoTest) afterCoverageOr(ctx context.Context, dir string, fallback float64) float64 {
	r, err := a.coverage.RunCoverage(ctx, dir)
	if err != nil {
		return fallback
	}
	return pct(r)
}

// pct calculates coverage percentage from a report
func pct(r *CoverageReport) float64 {
	if r == nil || r.TotalFuncs == 0 {
		return 0
	}
	return float64(r.CoveredFuncs) / float64(r.TotalFuncs) * 100
}
