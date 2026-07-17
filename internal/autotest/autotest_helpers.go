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

// pct calculates coverage percentage from a report (0–100). It prefers
// statement-level (line) coverage when the report carries profile data —
// including a measured 0% — and falls back to function/block-level otherwise.
func pct(r *CoverageReport) float64 {
	if r == nil {
		return 0
	}
	if len(r.Profile) > 0 {
		return r.LineCoveragePct
	}
	if r.TotalFuncs == 0 {
		return 0
	}
	return float64(r.CoveredFuncs) / float64(r.TotalFuncs) * 100
}
