package agent

import (
	"go.uber.org/zap"
)

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
	// Phase 1: Build in-degree map and adjacency list
	result := buildInDegreeAndAdjacency(graph)

	// Phase 2: Run Kahn's algorithm to detect cycles
	visited := runKahnsAlgorithm(result, len(graph))

	// Phase 3: If all nodes visited, no cycle
	if visited == len(graph) {
		return graph
	}

	// Phase 4: Find nodes in the cycle
	cycleNodes := findCycleNodes(result.inDegree)

	// Phase 5: Break the cycle
	return breakCycle(graph, cycleNodes, logger)
}
