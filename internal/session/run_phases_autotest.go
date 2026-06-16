package session

import (
	"fmt"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/autotest"
)

// executeAutoTestPhase runs the optional AutoTest coverage-driven test generation
func (rp *runPhase) executeAutoTestPhase() {
	if rp.session.AutoTestSafety == "" || rp.session.AutoTestSafety == "off" {
		return
	}

	mode := autotest.SafetyMode(rp.session.AutoTestSafety)
	cov := autotest.NewGoCoverageProvider(autotest.DefaultGoCoverageRunner, rp.session.Logger)
	gen := autotest.NewGoTestGenerator(rp.session.driverFor(&rp.session.scoutDriver), rp.session.Logger)
	at := autotest.NewAutoTest(cov, gen, autotest.NewEscalationGateAdapter(rp.session.Gate), nil, mode, rp.session.Logger)

	report, atErr := at.Run(rp.ctx, rp.session.ProjectDir)
	if atErr != nil {
		rp.session.Logger.Warn("autotest phase failed", zap.Error(atErr))
		return
	}

	if report != nil {
		rp.session.Logger.Info("autotest phase complete",
			zap.String("mode", string(mode)),
			zap.Int("gaps", len(report.Gaps)),
			zap.Int("generated", len(report.Generated)),
			zap.Int("written", len(report.Written)),
			zap.Int("reverted", len(report.Reverted)),
			zap.Float64("before_pct", report.BeforeCoveragePct),
			zap.Float64("after_pct", report.AfterCoveragePct))

		// dry-run: print each generated test for review
		if mode == autotest.SafetyDryRun {
			fmt.Println("\nAutoTest dry-run — generated test previews:")
			for _, tf := range report.Generated {
				fmt.Printf("\n--- %s ---\n%s\n", tf.Path, tf.Content)
			}
		}

		rp.session.LastAutoTestReport = report

		// Persist AutoTest report to DB (best-effort, non-blocking).
		if perr := rp.session.Store.UpdateSessionAutoTest(rp.ctx, rp.session.ID, report); perr != nil {
			rp.session.Logger.Warn("persist autotest report", zap.Error(perr))
		}
	}
}
