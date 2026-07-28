package agent

import (
	"context"
	"sync"
)

// ExecutePlan runs test cases in parallel respecting DependsOn ordering.
// Cases with no dependencies run concurrently (bounded by MaxWorkers).
// A case waits for ALL its dependencies to complete before starting.
// If any dependency failed or was skipped, this case is automatically skipped (cascade).
func (p *ParallelExecutor) ExecutePlan(ctx context.Context, plan *TestPlan, sessionID string) ([]StepResult, error) {
	if len(plan.Cases) == 0 {
		return nil, nil
	}

	// Phase 1: Build dependency graph and detect cycles
	depGraph := p.buildDependencyGraph(plan.Cases)
	cleanGraph := detectAndBreakCycles(depGraph, p.logger)

	// Phase 2: Initialize parallel execution state
	state := initParallelExecState(plan.Cases, p.config.MaxWorkers)

	// Phase 2.5: A1 Phase 2 — index lazy fallback cases by primary ID. They are
	// skipped in the dispatch loop and activated inline by their primary's worker.
	fallbacksByPrimary := map[string][]*TestCase{}
	for i := range plan.Cases {
		if tc := &plan.Cases[i]; tc.FallbackFor != "" {
			fb := &plan.Cases[i]
			fallbacksByPrimary[fb.FallbackFor] = append(fallbacksByPrimary[fb.FallbackFor], fb)
		}
	}

	// Phase 3: Execute test cases in parallel
	var wg sync.WaitGroup
	for i := range plan.Cases {
		tc := &plan.Cases[i]
		if tc.FallbackFor != "" {
			// Lazy fallback: activated only by its primary's worker below.
			continue
		}
		if isDeprioritized(tc) {
			p.skipAndStore(tc, state)
			continue
		}
		wg.Add(1)

		go func(tc *TestCase) {
			defer wg.Done()

			// Wait for dependencies
			if !p.waitForDependencies(ctx, tc, cleanGraph, state) {
				return
			}

			// Acquire worker slot
			if !acquireWorkerSlot(ctx, tc, state) {
				return
			}

			// Execute and store result
			p.executeAndStore(ctx, tc, sessionID, state, fallbacksByPrimary)
		}(tc)
	}

	wg.Wait()

	// Phase 4: Collect results in original order
	return collectResults(plan.Cases, state.results), nil
}
