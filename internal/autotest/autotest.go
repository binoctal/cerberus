package autotest

import (
	"context"
	"fmt"
	"os"
	"time"

	"go.uber.org/zap"
)

// RequestGate is the gate surface autotest needs: ask (or auto-approve) before a
// destructive write. escalation.Gate is adapted to this in Task 6.
type RequestGate interface {
	Request(ctx context.Context, checkpoint string, files []string, preview string) (bool, error)
}

// Writer writes a generated test and can revert it.
type Writer interface {
	Write(tf TestFile) error
	Revert(path string) error
}

// FSWriter is the default Writer: writes to disk, reverts via os.Remove.
type FSWriter struct{}

func (FSWriter) Write(tf TestFile) error  { return os.WriteFile(tf.Path, tf.Content, 0o644) }
func (FSWriter) Revert(path string) error { return os.Remove(path) }

type AutoTest struct {
	coverage CoverageProvider
	gen      TestGenerator
	gate     RequestGate
	writer   Writer
	mode     SafetyMode
	MaxGaps  int // cap on gaps generated per run (0 = unlimited); defaults to 5
	logger   *zap.Logger
}

func NewAutoTest(cov CoverageProvider, gen TestGenerator, gate RequestGate, w Writer, mode SafetyMode, logger *zap.Logger) *AutoTest {
	if logger == nil {
		logger = zap.NewNop()
	}
	if w == nil {
		w = FSWriter{}
	}
	return &AutoTest{coverage: cov, gen: gen, gate: gate, writer: w, mode: mode, MaxGaps: 5, logger: logger}
}

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
	// Cap gaps generated per run: a large codebase can have hundreds of gaps;
	// generating tests for all of them would be slow and expensive. Take the
	// first MaxGaps (>0). A future revision can rank by estimated coverage gain.
	if a.MaxGaps > 0 && len(rep.Gaps) > a.MaxGaps {
		rep.Gaps = rep.Gaps[:a.MaxGaps]
	}

	for _, gap := range rep.Gaps {
		src, _ := os.ReadFile(gap.File)
		tf, err := a.gen.Generate(ctx, gap, src)
		if err != nil {
			rep.Failed = append(rep.Failed, gap.File)
			continue
		}
		rep.Generated = append(rep.Generated, tf)

		switch a.mode {
		case SafetyDryRun:
			continue
		case SafetyApprove:
			ok, _ := a.gate.Request(ctx, "destructive_risk", []string{tf.Path}, string(tf.Content))
			if !ok {
				rep.Skipped = append(rep.Skipped, tf.Path)
				continue
			}
		case SafetyAuto:
			// write directly
		}
		if err := a.writer.Write(tf); err != nil {
			rep.Failed = append(rep.Failed, tf.Path)
			continue
		}
		rep.Written = append(rep.Written, tf.Path)

		// Verify: re-run coverage; keep only if pass AND strictly more covered.
		after, verr := a.coverage.RunCoverage(ctx, projectDir)
		if verr != nil || !after.Pass || pct(after) <= pct(before) {
			_ = a.writer.Revert(tf.Path)
			rep.Reverted = append(rep.Reverted, tf.Path)
			a.logger.Warn("autotest reverted test", zap.String("path", tf.Path), zap.Error(verr))
			continue
		}
	}
	rep.AfterCoveragePct = a.afterCoverageOr(ctx, projectDir, rep.BeforeCoveragePct)
	rep.Duration = time.Since(start)
	return rep, nil
}

func (a *AutoTest) afterCoverageOr(ctx context.Context, dir string, fallback float64) float64 {
	r, err := a.coverage.RunCoverage(ctx, dir)
	if err != nil {
		return fallback
	}
	return pct(r)
}

func pct(r *CoverageReport) float64 {
	if r == nil || r.TotalFuncs == 0 {
		return 0
	}
	return float64(r.CoveredFuncs) / float64(r.TotalFuncs) * 100
}
