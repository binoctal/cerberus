package architecture

import (
	"fmt"
)

// reportCircularDependency reports a circular dependency issue
func reportCircularDependency(cycle []string, issueCount int) ArchitectureIssue {
	return ArchitectureIssue{
		ID:          fmt.Sprintf("circular-dep-%d", issueCount),
		Type:        CircularDependency,
		Severity:    SeverityError,
		Description: "检测到循环依赖",
		Rationale:   "循环依赖导致代码难以理解和测试",
		Suggestion:  "引入依赖倒置或提取共同依赖到独立包",
		Confidence:  1.0,
		Evidence:    cycle,
	}
}

// countTotalDependencies counts total number of dependencies in graph
func countTotalDependencies(graph *DependencyGraph) int {
	totalDeps := 0
	for _, deps := range graph.Nodes {
		totalDeps += len(deps)
	}
	return totalDeps
}
