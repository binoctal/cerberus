package autotest

import (
	"context"
	"os"

	"go.uber.org/zap"
)

// processGap handles a single coverage gap with generation and verification
func (a *AutoTest) processGap(ctx context.Context, gap CoverageGap, projectDir string, before *CoverageReport, rep *AutoTestReport) *AutoTestItem {
	item := &AutoTestItem{
		TargetFile: gap.File,
		TargetFunc: gap.Func,
		Reason:     gap.Reason,
		Status:     "failed",
	}

	// Generate test (ignore file read errors, generator may not need source)
	src, _ := os.ReadFile(gap.File)
	tf, err := a.gen.Generate(ctx, gap, src)
	if err != nil {
		return item
	}

	item.TestPath = tf.Path
	item.Status = "generated"

	// Handle dry-run mode
	if a.mode == SafetyDryRun {
		return item
	}

	// Handle approval gate
	if a.mode == SafetyApprove {
		ok, _ := a.gate.Request(ctx, "destructive_risk", []string{tf.Path}, string(tf.Content))
		if !ok {
			item.Status = "skipped"
			return item
		}
	}

	// Write test file
	if err := a.writer.Write(tf); err != nil {
		return item
	}

	// Verify: re-run coverage; keep only if pass AND strictly more covered.
	// Note: In parallel mode, this verification happens per-test to avoid
	// interference between concurrent tests. A future optimization could
	// batch writes and do a single verification pass.
	after, verr := a.coverage.RunCoverage(ctx, projectDir)
	if verr != nil || !after.Pass || pct(after) <= pct(before) {
		_ = a.writer.Revert(tf.Path)
		item.Status = "reverted"
		a.logger.Warn("autotest reverted test", zap.String("path", tf.Path), zap.Error(verr))
	} else {
		item.Status = "written"
	}

	return item
}
