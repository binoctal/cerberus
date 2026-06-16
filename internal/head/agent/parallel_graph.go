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
