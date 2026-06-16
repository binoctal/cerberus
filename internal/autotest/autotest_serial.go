package autotest

import (
	"context"
	"os"

	"go.uber.org/zap"
)

// executeSerial processes gaps one at a time with immediate verification
func (a *AutoTest) executeSerial(ctx context.Context, projectDir string, before *CoverageReport, rep *AutoTestReport) {
	for _, gap := range rep.Gaps {
		// Initialize item for this gap
		item := AutoTestItem{
			TargetFile: gap.File,
			TargetFunc: gap.Func,
			Reason:     gap.Reason,
			Status:     "failed", // default until proven otherwise
		}

		src, _ := os.ReadFile(gap.File)
		tf, err := a.gen.Generate(ctx, gap, src)
		if err != nil {
			rep.Failed = append(rep.Failed, gap.File)
			item.Status = "failed"
			rep.Items = append(rep.Items, item)
			continue
		}
		rep.Generated = append(rep.Generated, tf)
		item.TestPath = tf.Path
		item.Status = "generated" // default for dry-run

		switch a.mode {
		case SafetyDryRun:
			rep.Items = append(rep.Items, item)
			continue
		case SafetyApprove:
			ok, _ := a.gate.Request(ctx, "destructive_risk", []string{tf.Path}, string(tf.Content))
			if !ok {
				rep.Skipped = append(rep.Skipped, tf.Path)
				item.Status = "skipped"
				rep.Items = append(rep.Items, item)
				continue
			}
		case SafetyAuto:
			// write directly
		}
		if err := a.writer.Write(tf); err != nil {
			rep.Failed = append(rep.Failed, tf.Path)
			item.Status = "failed"
			rep.Items = append(rep.Items, item)
			continue
		}
		rep.Written = append(rep.Written, tf.Path)

		// Verify: re-run coverage; keep only if pass AND strictly more covered.
		after, verr := a.coverage.RunCoverage(ctx, projectDir)
		if verr != nil || !after.Pass || pct(after) <= pct(before) {
			_ = a.writer.Revert(tf.Path)
			rep.Reverted = append(rep.Reverted, tf.Path)
			item.Status = "reverted"
			a.logger.Warn("autotest reverted test", zap.String("path", tf.Path), zap.Error(verr))
		} else {
			item.Status = "written"
		}
		rep.Items = append(rep.Items, item)
	}
}
