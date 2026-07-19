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

// lineCoverage returns the Examiner-phase coverage measurement, using an injected
// override when present (tests); otherwise the default coverageForSession.
func (s *Session) lineCoverage(ctx context.Context) contract.CoverageMeasurement {
	if s.coverageFn != nil {
		return s.coverageFn(ctx, s)
	}
	return coverageForSession(ctx, s)
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

// coverageForSession runs the language-specific coverage provider and returns a
// CoverageMeasurement. Pct is normalized to a 0–1 fraction (matching
// Gate.LineThreshold). Known is true only when the provider succeeded and the
// coverage denominator is non-zero; a provider error yields Known=false so the
// objective gate is skipped instead of forcing a false not-reached on a fake 0.
func coverageForSession(ctx context.Context, sess *Session) contract.CoverageMeasurement {
	markers := make(map[string]bool)
	if _, err := os.Stat(filepath.Join(sess.ProjectDir, "package.json")); err == nil {
		markers["package.json"] = true
	}
	if _, err := os.Stat(filepath.Join(sess.ProjectDir, "requirements.txt")); err == nil {
		markers["requirements.txt"] = true
	}
	if _, err := os.Stat(filepath.Join(sess.ProjectDir, "pyproject.toml")); err == nil {
		markers["pyproject.toml"] = true
	}

	var sourceFile string
	if matches, _ := filepath.Glob(filepath.Join(sess.ProjectDir, "*.go")); len(matches) > 0 {
		sourceFile = matches[0]
	} else if matches, _ := filepath.Glob(filepath.Join(sess.ProjectDir, "*.js")); len(matches) > 0 {
		sourceFile = matches[0]
	} else if matches, _ := filepath.Glob(filepath.Join(sess.ProjectDir, "*.ts")); len(matches) > 0 {
		sourceFile = matches[0]
	} else if matches, _ := filepath.Glob(filepath.Join(sess.ProjectDir, "*.py")); len(matches) > 0 {
		sourceFile = matches[0]
	}

	lang := autotest.DetectLanguage(sourceFile, markers)
	provider := autotest.NewCoverageProviderForLanguage(lang, nil, sess.Logger)
	report, err := provider.RunCoverage(ctx, sess.ProjectDir)
	if err != nil || report == nil {
		return contract.CoverageMeasurement{Known: false}
	}

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
