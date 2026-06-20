package session

import (
	"context"
	"os"
	"path/filepath"

	"github.com/binoctal/cerberus/internal/autotest"
)

// lineCoverage returns the Examiner-phase line coverage percentage, using an
// injected override when present (tests); otherwise the default
// coverageForSession (reuses the AutoTest report, else runs a coverage provider).
func (s *Session) lineCoverage(ctx context.Context) float64 {
	if s.coverageFn != nil {
		return s.coverageFn(ctx, s)
	}
	return coverageForSession(ctx, s)
}

// coverageForSession returns the real line coverage percentage for the session's
// project. If AutoTest ran (has a report with coverage), reuse it; otherwise
// independently run the language-specific coverage provider.
func coverageForSession(ctx context.Context, sess *Session) float64 {
	// A: reuse AutoTest report if available.
	if sess.LastAutoTestReport != nil && sess.LastAutoTestReport.BeforeCoveragePct > 0 {
		return sess.LastAutoTestReport.BeforeCoveragePct
	}

	// B: independently run coverage provider.
	// Detect language from project directory
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

	// Find a source file to detect extension
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
	provider := autotest.NewCoverageProviderForLanguage(lang, autotest.DefaultGoCoverageRunner, sess.Logger)
	report, err := provider.RunCoverage(ctx, sess.ProjectDir)
	if err != nil || report == nil {
		return 0
	}

	// Calculate coverage percentage from report
	if report.TotalFuncs == 0 {
		return 0
	}
	return float64(report.CoveredFuncs) / float64(report.TotalFuncs) * 100
}
