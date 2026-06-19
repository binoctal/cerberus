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

	// Phase 3: Execute test cases in parallel
	var wg sync.WaitGroup
	for i := range plan.Cases {
		tc := &plan.Cases[i]
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
			p.executeAndStore(ctx, tc, sessionID, state)
		}(tc)
	}

	wg.Wait()

	// Phase 4: Collect results in original order
	return collectResults(plan.Cases, state.results), nil
}
