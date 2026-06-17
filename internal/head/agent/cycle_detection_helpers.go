package agent

import (
	"go.uber.org/zap"
)

// inDegreeResult stores the in-degree map and adjacency list
type inDegreeResult struct {
	inDegree map[string]int
	adj      map[string][]string
}

// buildInDegreeAndAdjacency builds in-degree map and adjacency list from dependency graph.
func buildInDegreeAndAdjacency(graph map[string][]string) inDegreeResult {
	inDegree := make(map[string]int)
	adj := make(map[string][]string)

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

	return inDegreeResult{inDegree: inDegree, adj: adj}
}

// runKahnsAlgorithm executes Kahn's algorithm to detect cycles.
// Returns the number of nodes visited (if equals total nodes, no cycle).
func runKahnsAlgorithm(result inDegreeResult, totalNodes int) int {
	queue := make([]string, 0)
	for node, deg := range result.inDegree {
		if deg == 0 {
			queue = append(queue, node)
		}
	}

	visited := 0
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		visited++
		for _, dependent := range result.adj[node] {
			result.inDegree[dependent]--
			if result.inDegree[dependent] == 0 {
				queue = append(queue, dependent)
			}
		}
	}

	return visited
}

// findCycleNodes identifies nodes still in the cycle (non-zero in-degree).
func findCycleNodes(inDegree map[string]int) []string {
	cycleNodes := make([]string, 0)
	for node, deg := range inDegree {
		if deg > 0 {
			cycleNodes = append(cycleNodes, node)
		}
	}
	return cycleNodes
}

// breakCycle removes edges between nodes in the cycle to break it.
func breakCycle(graph map[string][]string, cycleNodes []string, logger *zap.Logger) map[string][]string {
	logger.Warn("dependency cycle detected, breaking edges",
		zap.Strings("cycle_nodes", cycleNodes),
	)

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
