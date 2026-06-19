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

	// Read the target source so the generator can ground the LLM in real code.
	// go cover emits module-qualified paths (github.com/x/y/...); sourcePath
	// resolves them to a filesystem path under projectDir.
	src, readErr := os.ReadFile(sourcePath(gap.File, projectDir))
	if readErr != nil {
		a.logger.Warn("autotest: could not read source for gap",
			zap.String("file", gap.File),
			zap.Error(readErr))
	}
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
