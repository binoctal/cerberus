package agent

import (
	"context"
	"fmt"
	"sync"

	"go.uber.org/zap"
)

// ParallelConfig controls parallel execution behavior.
type ParallelConfig struct {
	MaxWorkers int // Maximum concurrent workers (default: 4)
}

// DefaultParallelConfig returns sensible defaults.
func DefaultParallelConfig() ParallelConfig {
	return ParallelConfig{MaxWorkers: 4}
}

// ParallelExecutor runs independent test cases concurrently with dependency ordering.
type ParallelExecutor struct {
	loop   *ReActLoop
	config ParallelConfig
	logger *zap.Logger
}

// NewParallelExecutor creates a parallel test executor.
func NewParallelExecutor(loop *ReActLoop, config ParallelConfig, logger *zap.Logger) *ParallelExecutor {
	return &ParallelExecutor{loop: loop, config: config, logger: logger}
}

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

// buildDependencyGraph maps case ID → list of dependency IDs.
func (p *ParallelExecutor) buildDependencyGraph(cases []TestCase) map[string][]string {
	graph := make(map[string][]string, len(cases))
	for _, tc := range cases {
		graph[tc.ID] = tc.DependsOn
	}
	return graph
}

// detectAndBreakCycles uses Kahn's algorithm to detect cycles.
// If a cycle is found, edges from the lowest-priority nodes are removed
// and a warning is logged.
func detectAndBreakCycles(graph map[string][]string, logger *zap.Logger) map[string][]string {
	// Build in-degree map and adjacency list.
	inDegree := make(map[string]int)
	adj := make(map[string][]string) // forward edges: node → dependents

	for node := range graph {
		if _, ok := inDegree[node]; !ok {
			inDegree[node] = 0
		}
		for _, dep := range graph[node] {
			// dep must complete before node (edge: dep → node).
			adj[dep] = append(adj[dep], node)
			inDegree[node]++
		}
	}

	// Kahn's algorithm: collect nodes with zero in-degree.
	queue := make([]string, 0)
	for node, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, node)
		}
	}

	visited := 0
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		visited++
		for _, dependent := range adj[node] {
			inDegree[dependent]--
			if inDegree[dependent] == 0 {
				queue = append(queue, dependent)
			}
		}
	}

	// If all nodes visited, no cycle.
	if visited == len(graph) {
		return graph
	}

	// Cycle detected: find remaining nodes (non-zero in-degree).
	cycleNodes := make([]string, 0)
	for node, deg := range inDegree {
		if deg > 0 {
			cycleNodes = append(cycleNodes, node)
		}
	}

	logger.Warn("dependency cycle detected, breaking edges",
		zap.Strings("cycle_nodes", cycleNodes),
	)

	// Break cycle: for each node in the cycle, remove all deps that are also in the cycle.
	cycleSet := make(map[string]bool, len(cycleNodes))
	for _, n := range cycleNodes {
		cycleSet[n] = true
	}

	cleanGraph := make(map[string][]string, len(graph))
	for node, deps := range graph {
		var kept []string
		for _, dep := range deps {
			if cycleSet[dep] && cycleSet[node] {
				// Remove intra-cycle edge to break the cycle.
				continue
			}
			kept = append(kept, dep)
		}
		cleanGraph[node] = kept
	}

	return cleanGraph
}
