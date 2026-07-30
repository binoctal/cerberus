package autotest

import (
	"context"
	"fmt"
	"time"
)

// Run executes the autotest cycle: baseline coverage → gap detection → test generation → verification
func (a *AutoTest) Run(ctx context.Context, projectDir string) (*AutoTestReport, error) {
	start := time.Now()
	rep := &AutoTestReport{}
	before, err := a.coverage.RunCoverage(ctx, projectDir)
	if err != nil {
		return rep, err
	}
	rep.BeforeCoveragePct = pct(before)
	if !before.Pass {
		return rep, fmt.Errorf("autotest: existing tests failing; fix before generating")
	}
	rep.Gaps = a.coverage.Gaps(before)
	// Addendum: a concrete GoCoverageProvider also knows no-test-file gaps.
	if gcp, ok := a.coverage.(*GoCoverageProvider); ok {
		rep.Gaps = append(rep.Gaps, gcp.NoTestFileGaps(projectDir)...)
	}
	// D1 §6.7: drop gaps the coverage repair loop already targeted, so Phase 4
	// does not regenerate tests for them. Applied before the MaxGaps cap so
	// excluded gaps do not consume cap slots.
	rep.Gaps = a.withoutExcluded(rep.Gaps)
	// Cap gaps generated per run: a large codebase can have hundreds of gaps;
	// generating tests for all of them would be slow and expensive. Rank by
	// estimated coverage gain (Go: zero-cover block count per file) so the
	// highest-gain gaps survive the cap, then take the first MaxGaps (>0).
	rep.Gaps = RankByGain(rep.Gaps, before)
	if a.MaxGaps > 0 && len(rep.Gaps) > a.MaxGaps {
		rep.Gaps = rep.Gaps[:a.MaxGaps]
	}

	// Execute: serial or parallel based on MaxConcurrency setting
	if a.MaxConcurrency <= 1 {
		a.executeSerial(ctx, projectDir, before, rep)
	} else {
		a.executeParallel(ctx, projectDir, before, rep)
	}

	rep.AfterCoveragePct = a.afterCoverageOr(ctx, projectDir, rep.BeforeCoveragePct)
	rep.Duration = time.Since(start)
	return rep, nil
}
