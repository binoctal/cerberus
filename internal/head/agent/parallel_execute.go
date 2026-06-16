package agent

import (
	"context"
	"fmt"
	"sync"

	"go.uber.org/zap"
)

// ExecutePlan runs test cases in parallel respecting DependsOn ordering.
// Cases with no dependencies run concurrently (bounded by MaxWorkers).
// A case waits for ALL its dependencies to complete before starting.
// If any dependency failed or was skipped, this case is automatically skipped (cascade).
func (p *ParallelExecutor) ExecutePlan(ctx context.Context, plan *TestPlan, sessionID string) ([]StepResult, error) {
	if len(plan.Cases) == 0 {
		return nil, nil
	}

	// Build dependency graph.
	depGraph := p.buildDependencyGraph(plan.Cases)

	// Detect and break cycles.
	cleanGraph := detectAndBreakCycles(depGraph, p.logger)

	// Result map: case ID → StepResult.
	results := make(map[string]StepResult, len(plan.Cases))
	var mu sync.Mutex

	// Completion signal per case ID.
	completed := make(map[string]chan struct{}, len(plan.Cases))
	for _, tc := range plan.Cases {
		completed[tc.ID] = make(chan struct{})
	}

	// Worker pool semaphore.
	sem := make(chan struct{}, p.config.MaxWorkers)

	var wg sync.WaitGroup

	for i := range plan.Cases {
		tc := &plan.Cases[i]
		wg.Add(1)

		go func(tc *TestCase) {
			defer wg.Done()

			// Wait for ALL dependencies to complete (fan-in).
			deps := cleanGraph[tc.ID]
			for _, dep := range deps {
				if depCh, exists := completed[dep]; exists {
					select {
					case <-depCh:
						// Dependency completed — check if it passed.
						mu.Lock()
						depResult, depExists := results[dep]
						mu.Unlock()
						if depExists && depResult.Status != StepPassed {
							// Cascade skip: dependency failed/skipped/uncertain.
							mu.Lock()
							results[tc.ID] = StepResult{
								TestCase: tc,
								Status:   StepSkipped,
								Error:    fmt.Errorf("dependency %s %s: cascade skip", dep, depResult.Status),
							}
							close(completed[tc.ID])
							mu.Unlock()
							p.logger.Info("cascade skip",
								zap.String("case_id", tc.ID),
								zap.String("dependency", dep),
								zap.String("dep_status", string(depResult.Status)),
							)
							return
						}
					case <-ctx.Done():
						mu.Lock()
						results[tc.ID] = StepResult{
							TestCase: tc, Status: StepSkipped,
							Error: ctx.Err(),
						}
						close(completed[tc.ID])
						mu.Unlock()
						return
					}
				}
			}

			// Acquire worker slot.
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				mu.Lock()
				results[tc.ID] = StepResult{
					TestCase: tc, Status: StepSkipped,
					Error: ctx.Err(),
				}
				close(completed[tc.ID])
				mu.Unlock()
				return
			}
			defer func() { <-sem }()

			// Execute the step.
			result := p.loop.executeStep(ctx, tc, sessionID)

			mu.Lock()
			results[tc.ID] = result
			close(completed[tc.ID])
			mu.Unlock()

			p.logger.Info("parallel case completed",
				zap.String("case_id", tc.ID),
				zap.String("status", string(result.Status)),
			)
		}(tc)
	}

	wg.Wait()

	// Collect results in original order.
	ordered := make([]StepResult, 0, len(plan.Cases))
	for _, tc := range plan.Cases {
		if r, ok := results[tc.ID]; ok {
			ordered = append(ordered, r)
		}
	}

	return ordered, nil
}
