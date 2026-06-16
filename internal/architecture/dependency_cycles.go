package architecture

import (
	"strings"
)

// detectCircularDependencies detects cycles in dependency graph using DFS
func (a *Analyzer) detectCircularDependencies(graph *DependencyGraph) [][]string {
	cycles := [][]string{}

	visited := make(map[string]bool)
	recStack := make(map[string]bool)

	// Track cycles we've found to avoid duplicates
	foundCycles := make(map[string]bool)

	for node := range graph.Nodes {
		if !visited[node] {
			cycle := a.findCycle(node, visited, recStack, graph, []string{})
			if len(cycle) > 0 {
				cycleKey := strings.Join(cycle, "→")
				if !foundCycles[cycleKey] {
					cycles = append(cycles, cycle)
					foundCycles[cycleKey] = true
				}
			}
		}
	}

	return cycles
}

// findCycle performs DFS to detect cycles
func (a *Analyzer) findCycle(node string, visited, recStack map[string]bool, graph *DependencyGraph, path []string) []string {
	visited[node] = true
	recStack[node] = true
	path = append(path, node)

	for _, neighbor := range graph.Nodes[node] {
		// Skip standard library
		if a.isStandardLib(neighbor) {
			continue
		}

		if !visited[neighbor] {
			if cycle := a.findCycle(neighbor, visited, recStack, graph, path); len(cycle) > 0 {
				return cycle
			}
		} else if recStack[neighbor] {
			// Found a cycle - extract it from path
			cycleStart := -1
			for i, n := range path {
				if n == neighbor {
					cycleStart = i
					break
				}
			}

			if cycleStart >= 0 {
				cycle := append(path[cycleStart:], neighbor)
				return cycle
			}
		}
	}

	recStack[node] = false
	return nil
}
