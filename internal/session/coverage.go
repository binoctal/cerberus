package session

import (
	"context"
	"os"
	"path/filepath"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/autotest"
	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/head/contract"
	"github.com/binoctal/cerberus/internal/head/examiner"
)

// lineCoverage returns the Examiner-phase coverage measurement, reusing
// lineCoverageReport so the provider runs once and only the measurement is
// projected out (no regression to assessCoverageIfContract).
func (s *Session) lineCoverage(ctx context.Context) contract.CoverageMeasurement {
	_, m := s.lineCoverageReport(ctx)
	return m
}

// lineCoverageReport runs the Examiner-phase coverage provider ONCE and returns
// BOTH the raw CoverageReport (reused by the coverage repair loop for gap
// detection) and the derived CoverageMeasurement. It honors an injected
// override (tests): when coverageFn is set it returns (nil, measurement) — the
// stub supplies only a measurement, and callers tolerate a nil report.
func (s *Session) lineCoverageReport(ctx context.Context) (*autotest.CoverageReport, contract.CoverageMeasurement) {
	if s.coverageFn != nil {
		return nil, s.coverageFn(ctx, s)
	}
	return coverageReportForSession(ctx, s)
}

// assessCoverageIfContract runs the objective coverage assessment against the
// session's contract (if any). Shared by the run and resume Examiner paths.
// sess.lineCoverage honors an injected stub (tests) to avoid recursively
// running go test/jest/pytest when ProjectDir is a module under test.
func assessCoverageIfContract(ctx context.Context, sess *Session, examinerHead *examiner.Examiner, results []agent.StepResult) {
	if sess.Contract == nil {
		return
	}
	measurement := sess.lineCoverage(ctx)
	assessment, err := examinerHead.AssessCoverage(ctx, sess.Contract, results, measurement)
	if err == nil {
		sess.Assessment = assessment
		sess.Logger.Info("coverage assessment",
			zap.Bool("reached", assessment.Reached),
			zap.Int("gaps", len(assessment.Gaps)),
			zap.Float64("coverage_pct", assessment.CoveragePct))
	} else {
		sess.Logger.Warn("coverage assessment failed", zap.Error(err))
	}
}

// coverageForSession returns only the measurement, delegating to
// coverageReportForSession so the provider runs once. Kept as a thin wrapper
// for existing callers/tests.
func coverageForSession(ctx context.Context, sess *Session) contract.CoverageMeasurement {
	_, m := coverageReportForSession(ctx, sess)
	return m
}

// coverageReportForSession runs the language-specific coverage provider and
// returns BOTH the raw report (for gap reuse) and the measurement. Pct is
// normalized to a 0–1 fraction (matching Gate.LineThreshold). Known is true
// only when the provider succeeded and the coverage denominator is non-zero; a
// provider error yields Known=false so the objective gate is skipped instead of
// forcing a false not-reached on a fake 0.
func coverageReportForSession(ctx context.Context, sess *Session) (*autotest.CoverageReport, contract.CoverageMeasurement) {
	provider := coverageProviderForSession(sess)
	report, err := provider.RunCoverage(ctx, sess.ProjectDir)
	if err != nil || report == nil {
		return nil, contract.CoverageMeasurement{Known: false}
	}
	return report, measurementFromReport(report)
}

// detectLanguage identifies the project language from projectDir via package
// markers and a source-file extension. Shared by the coverage provider path
// and the coverage repair axis so language detection is not duplicated.
func detectLanguage(projectDir string) string {
	markers := make(map[string]bool)
	if _, err := os.Stat(filepath.Join(projectDir, "package.json")); err == nil {
		markers["package.json"] = true
	}
	if _, err := os.Stat(filepath.Join(projectDir, "requirements.txt")); err == nil {
		markers["requirements.txt"] = true
	}
	if _, err := os.Stat(filepath.Join(projectDir, "pyproject.toml")); err == nil {
		markers["pyproject.toml"] = true
	}

	var sourceFile string
	if matches, _ := filepath.Glob(filepath.Join(projectDir, "*.go")); len(matches) > 0 {
		sourceFile = matches[0]
	} else if matches, _ := filepath.Glob(filepath.Join(projectDir, "*.js")); len(matches) > 0 {
		sourceFile = matches[0]
	} else if matches, _ := filepath.Glob(filepath.Join(projectDir, "*.ts")); len(matches) > 0 {
		sourceFile = matches[0]
	} else if matches, _ := filepath.Glob(filepath.Join(projectDir, "*.py")); len(matches) > 0 {
		sourceFile = matches[0]
	}
	return autotest.DetectLanguage(sourceFile, markers)
}

// coverageProviderForSession builds the language-specific coverage provider
// for a session (nil runner; RunCoverage is only used on the measure paths).
func coverageProviderForSession(sess *Session) autotest.CoverageProvider {
	return autotest.NewCoverageProviderForLanguage(detectLanguage(sess.ProjectDir), nil, sess.Logger)
}

// measurementFromReport derives the normalized CoverageMeasurement from a raw
// provider report. Unit is "line" (Go) or "function" (Node/Python); Pct is a
// 0–1 fraction; Known is false when nothing measurable was collected.
func measurementFromReport(report *autotest.CoverageReport) contract.CoverageMeasurement {
	unit := report.CoverageUnit
	if unit == "" {
		unit = "function"
	}
	var pct100 float64
	known := false
	if unit == "line" {
		// Line coverage is measured when any profile block exists.
		if len(report.Profile) > 0 {
			pct100 = report.LineCoveragePct
			known = true
		}
	} else {
		if report.TotalFuncs > 0 {
			pct100 = float64(report.CoveredFuncs) / float64(report.TotalFuncs) * 100
			known = true
		}
	}
	if !known {
		return contract.CoverageMeasurement{Known: false}
	}
	return contract.CoverageMeasurement{Pct: pct100 / 100, Unit: unit, Known: true}
}
