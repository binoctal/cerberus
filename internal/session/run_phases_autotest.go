package session

import (
	"fmt"
	"os"
	"path/filepath"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/autotest"
)

// executeAutoTestPhase runs the optional AutoTest coverage-driven test generation
func (rp *runPhase) executeAutoTestPhase() {
	if rp.session.AutoTestSafety == "" || rp.session.AutoTestSafety == "off" {
		return
	}

	mode := autotest.SafetyMode(rp.session.AutoTestSafety)

	// Detect project language and route to appropriate provider directly
	driver := rp.session.driverFor(&rp.session.scoutDriver)

	// Detect language from project directory
	markers := make(map[string]bool)
	if _, err := os.Stat(filepath.Join(rp.session.ProjectDir, "package.json")); err == nil {
		markers["package.json"] = true
	}
	if _, err := os.Stat(filepath.Join(rp.session.ProjectDir, "requirements.txt")); err == nil {
		markers["requirements.txt"] = true
	}
	if _, err := os.Stat(filepath.Join(rp.session.ProjectDir, "pyproject.toml")); err == nil {
		markers["pyproject.toml"] = true
	}

	// Find a source file to detect extension
	var sourceFile string
	if matches, _ := filepath.Glob(filepath.Join(rp.session.ProjectDir, "*.go")); len(matches) > 0 {
		sourceFile = matches[0]
	} else if matches, _ := filepath.Glob(filepath.Join(rp.session.ProjectDir, "*.js")); len(matches) > 0 {
		sourceFile = matches[0]
	} else if matches, _ := filepath.Glob(filepath.Join(rp.session.ProjectDir, "*.ts")); len(matches) > 0 {
		sourceFile = matches[0]
	} else if matches, _ := filepath.Glob(filepath.Join(rp.session.ProjectDir, "*.py")); len(matches) > 0 {
		sourceFile = matches[0]
	}

	// Detect language and create appropriate provider
	var cov autotest.CoverageProvider
	var gen autotest.TestGenerator

	lang := autotest.DetectLanguage(sourceFile, markers)
	switch lang {
	case "node":
		cov = autotest.NewNodeCoverageProvider(autotest.DefaultNodeCoverageConfig())
		gen = autotest.NewNodeTestGenerator(driver)
	case "python":
		cov = autotest.NewPythonCoverageProvider(autotest.DefaultPythonCoverageConfig())
		gen = autotest.NewPythonTestGenerator(driver)
	default: // "go" or fallback
		cov = autotest.NewGoCoverageProvider(autotest.DefaultGoCoverageRunner, rp.session.Logger)
		gen = autotest.NewGoTestGenerator(driver, rp.session.Logger)
	}

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
