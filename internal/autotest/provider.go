package autotest

import "context"

// CoverageProvider runs tests, parses coverage, and finds uncovered code.
type CoverageProvider interface {
	RunCoverage(ctx context.Context, projectDir string) (*CoverageReport, error)
	Gaps(report *CoverageReport) []CoverageGap
}

// TestGenerator produces a test file for one gap.
type TestGenerator interface {
	Generate(ctx context.Context, gap CoverageGap, source []byte) (TestFile, error)
}
