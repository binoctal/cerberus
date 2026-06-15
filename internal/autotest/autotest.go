package autotest

import (
	"context"
	"fmt"
	"os"
	"sync"
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
	coverage      CoverageProvider
	gen           TestGenerator
	gate          RequestGate
	writer        Writer
	mode          SafetyMode
	MaxGaps       int // cap on gaps generated per run (0 = unlimited); defaults to 5
	MaxConcurrency int // max parallel workers (0 = serial); defaults to 3
	logger        *zap.Logger
}

func NewAutoTest(cov CoverageProvider, gen TestGenerator, gate RequestGate, w Writer, mode SafetyMode, logger *zap.Logger) *AutoTest {
	if logger == nil {
		logger = zap.NewNop()
	}
	if w == nil {
		w = FSWriter{}
	}
	return &AutoTest{
		coverage:       cov,
		gen:            gen,
		gate:           gate,
		writer:         w,
		mode:           mode,
		MaxGaps:        5,
		MaxConcurrency: 1, // default to serial for backward compatibility
		logger:         logger,
	}
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

// executeSerial processes gaps one at a time with immediate verification.
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

// executeParallel processes gaps concurrently with worker pool and batch verification.
func (a *AutoTest) executeParallel(ctx context.Context, projectDir string, before *CoverageReport, rep *AutoTestReport) {
	// Create channels for work distribution and result collection
	gapChan := make(chan CoverageGap, len(rep.Gaps))
	resultChan := make(chan *AutoTestItem, len(rep.Gaps))

	// Worker pool
	var wg sync.WaitGroup
	for i := 0; i < a.MaxConcurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for gap := range gapChan {
				item := a.processGap(ctx, gap, projectDir, before, rep)
				resultChan <- item
			}
		}()
	}

	// Distribute work
	for _, gap := range rep.Gaps {
		gapChan <- gap
	}
	close(gapChan)

	// Wait for all workers to complete
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// Collect results
	for item := range resultChan {
		rep.Items = append(rep.Items, *item)
		switch item.Status {
		case "failed":
			rep.Failed = append(rep.Failed, item.TargetFile)
		case "generated":
			rep.Generated = append(rep.Generated, TestFile{Path: item.TestPath})
		case "skipped":
			rep.Skipped = append(rep.Skipped, item.TestPath)
		case "written":
			rep.Written = append(rep.Written, item.TestPath)
		case "reverted":
			rep.Reverted = append(rep.Reverted, item.TestPath)
		}
	}
}

// processGap handles a single coverage gap with generation and verification.
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
