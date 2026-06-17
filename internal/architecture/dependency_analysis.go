package architecture

import (
	"os"
	"path/filepath"
	"strings"
)

// analyzeDependencies analyzes package dependencies
func (a *Analyzer) analyzeDependencies(report *ArchitectureReport) error {
	graph := &DependencyGraph{
		Nodes: make(map[string][]string),
	}

	// Phase 1: Build dependency graph
	err := a.buildDependencyGraph(graph)
	if err != nil {
		return err
	}

	// Phase 2: Detect and report circular dependencies
	a.detectAndReportCycles(graph, report)

	// Phase 3: Count total dependencies
	report.Metrics.TotalDependencies = countTotalDependencies(graph)

	return nil
}

// buildDependencyGraph walks through Go files to build dependency graph
func (a *Analyzer) buildDependencyGraph(graph *DependencyGraph) error {
	return filepath.Walk(a.projectPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !a.shouldAnalyzeFileForDeps(path, info) {
			return nil
		}

		// Parse file to extract imports
		if err := a.extractImports(path, graph); err != nil {
			// Continue with other files if parsing fails
			return nil
		}

		return nil
	})
}

// shouldAnalyzeFileForDeps checks if file should be analyzed for dependencies
func (a *Analyzer) shouldAnalyzeFileForDeps(path string, info os.FileInfo) bool {
	// Skip directories and non-Go files
	if info.IsDir() || !strings.HasSuffix(path, ".go") {
		return false
	}

	// Skip test files and vendor
	if strings.Contains(path, "_test.go") || strings.Contains(path, "vendor/") {
		return false
	}

	return true
}

// detectAndReportCycles detects circular dependencies and reports them
func (a *Analyzer) detectAndReportCycles(graph *DependencyGraph, report *ArchitectureReport) {
	cycles := a.detectCircularDependencies(graph)
	report.Metrics.CircularDependencies = len(cycles)

	for i, cycle := range cycles {
		issue := reportCircularDependency(cycle, i)
		report.Issues = append(report.Issues, issue)
	}
}
