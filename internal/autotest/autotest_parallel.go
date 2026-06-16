package autotest

import (
	"context"
	"sync"
)

// executeParallel processes gaps concurrently with worker pool and batch verification
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
