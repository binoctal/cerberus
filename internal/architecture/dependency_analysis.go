package architecture

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// analyzeDependencies analyzes package dependencies
func (a *Analyzer) analyzeDependencies(report *ArchitectureReport) error {
	graph := &DependencyGraph{
		Nodes: make(map[string][]string),
	}

	// Walk through Go files to build dependency graph
	err := filepath.Walk(a.projectPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories and non-Go files
		if info.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}

		// Skip test files and vendor
		if strings.Contains(path, "_test.go") || strings.Contains(path, "vendor/") {
			return nil
		}

		// Parse file to extract imports
		if err := a.extractImports(path, graph); err != nil {
			// Continue with other files if parsing fails
			return nil
		}

		return nil
	})

	if err != nil {
		return err
	}

	// Detect circular dependencies
	cycles := a.detectCircularDependencies(graph)
	report.Metrics.CircularDependencies = len(cycles)

	for _, cycle := range cycles {
		report.Issues = append(report.Issues, ArchitectureIssue{
			ID:          fmt.Sprintf("circular-dep-%d", len(report.Issues)),
			Type:        CircularDependency,
			Severity:    SeverityError,
			Description: "检测到循环依赖",
			Rationale:   "循环依赖导致代码难以理解和测试",
			Suggestion:  "引入依赖倒置或提取共同依赖到独立包",
			Confidence:  1.0,
			Evidence:    cycle,
		})
	}

	// Count total dependencies
	totalDeps := 0
	for _, deps := range graph.Nodes {
		totalDeps += len(deps)
	}
	report.Metrics.TotalDependencies = totalDeps

	return nil
}
