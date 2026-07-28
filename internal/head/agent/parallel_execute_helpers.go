package agent

import (
	"context"
	"fmt"
	"sync"

	"go.uber.org/zap"
)

// parallelExecState holds shared state for parallel execution
type parallelExecState struct {
	results   map[string]StepResult
	completed map[string]chan struct{}
	sem       chan struct{}
	mu        sync.Mutex
}

// initParallelExecState initializes the shared state for parallel execution
func initParallelExecState(cases []TestCase, maxWorkers int) *parallelExecState {
	results := make(map[string]StepResult, len(cases))
	completed := make(map[string]chan struct{}, len(cases))
	for _, tc := range cases {
		completed[tc.ID] = make(chan struct{})
	}

	return &parallelExecState{
		results:   results,
		completed: completed,
		sem:       make(chan struct{}, maxWorkers),
	}
}

// waitForDependencies waits for all dependencies to complete
func (p *ParallelExecutor) waitForDependencies(ctx context.Context, tc *TestCase, cleanGraph map[string][]string, state *parallelExecState) bool {
	deps := cleanGraph[tc.ID]
	for _, dep := range deps {
		depCh, exists := state.completed[dep]
		if !exists {
			continue
		}

		select {
		case <-depCh:
			// Dependency completed — check if it passed
			state.mu.Lock()
			depResult, depExists := state.results[dep]
			state.mu.Unlock()

			if depExists && depResult.Status != StepPassed {
				// Cascade skip: dependency failed/skipped/uncertain
				state.mu.Lock()
				state.results[tc.ID] = StepResult{
					TestCase: tc,
					Status:   StepSkipped,
					Error:    fmt.Errorf("dependency %s %s: cascade skip", dep, depResult.Status),
				}
				close(state.completed[tc.ID])
				state.mu.Unlock()

				p.logger.Info("cascade skip",
					zap.String("case_id", tc.ID),
					zap.String("dependency", dep),
					zap.String("dep_status", string(depResult.Status)),
				)
				return false // Skip execution
			}

		case <-ctx.Done():
			state.mu.Lock()
			state.results[tc.ID] = StepResult{
				TestCase: tc,
				Status:   StepSkipped,
				Error:    ctx.Err(),
			}
			close(state.completed[tc.ID])
			state.mu.Unlock()
			return false // Skip execution
		}
	}
	return true // Ready to execute
}

// acquireWorkerSlot acquires a worker slot from the semaphore
func acquireWorkerSlot(ctx context.Context, tc *TestCase, state *parallelExecState) bool {
	select {
	case state.sem <- struct{}{}:
		return true
	case <-ctx.Done():
		state.mu.Lock()
		state.results[tc.ID] = StepResult{
			TestCase: tc,
			Status:   StepSkipped,
			Error:    ctx.Err(),
		}
		close(state.completed[tc.ID])
		state.mu.Unlock()
		return false
	}
}

// executeAndStore executes a single test case and stores the result. On a
// non-environmental failure it also runs the case's lazy fallbacks (A1 Phase 2)
// inline in this same worker and stores each by its own ID. A fallback is bound
// to exactly one primary and runs only here, so results[fb.ID] is written once.
func (p *ParallelExecutor) executeAndStore(ctx context.Context, tc *TestCase, sessionID string, state *parallelExecState, fallbacksByPrimary map[string][]*TestCase) {
	defer func() { <-state.sem }()

	result := p.loop.executeStep(ctx, tc, sessionID)

	store := func(r StepResult) {
		state.mu.Lock()
		state.results[r.TestCase.ID] = r
		if ch, ok := state.completed[r.TestCase.ID]; ok {
			close(ch)
		}
		state.mu.Unlock()
	}
	store(result)

	// A1 Phase 2: activate lazy fallback on non-environmental failure.
	if result.Status == StepFailed && !isEnvironmental(result) {
		for _, fb := range fallbacksByPrimary[tc.ID] {
			fbResult := p.loop.executeStep(ctx, fb, sessionID)
			fbResult.Recovered = fbResult.Status == StepPassed
			store(fbResult)
		}
	}

	p.logger.Info("parallel case completed",
		zap.String("case_id", tc.ID),
		zap.String("status", string(result.Status)),
	)
}

// skipAndStore records a skipped result for a deprioritized case and marks it
// complete so dependent cases cascade-skip, without executing or consuming a
// worker slot.
func (p *ParallelExecutor) skipAndStore(tc *TestCase, state *parallelExecState) {
	state.mu.Lock()
	state.results[tc.ID] = StepResult{TestCase: tc, Status: StepSkipped}
	close(state.completed[tc.ID])
	state.mu.Unlock()

	p.logger.Info("parallel case skipped (deprioritized)",
		zap.String("case_id", tc.ID),
		zap.Float64("priority", tc.Priority),
	)
}

// collectResults collects results in the original order
func collectResults(cases []TestCase, results map[string]StepResult) []StepResult {
	ordered := make([]StepResult, 0, len(cases))
	for _, tc := range cases {
		if r, ok := results[tc.ID]; ok {
			ordered = append(ordered, r)
		}
	}
	return ordered
}
