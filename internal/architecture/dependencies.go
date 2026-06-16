package architecture

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// DependencyGraph represents import dependencies between packages
type DependencyGraph struct {
	Nodes map[string][]string // package -> list of imported packages
}

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

// extractImports extracts import statements from a Go file
func (a *Analyzer) extractImports(filePath string, graph *DependencyGraph) error {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filePath, nil, parser.ImportsOnly)
	if err != nil {
		return err
	}

	// Get the package of this file
	filePkg := a.getPackageFromPath(filePath)

	// Extract imports
	for _, imp := range node.Imports {
		importPath := strings.Trim(imp.Path.Value, "\"")
		
		// Skip standard library and relative imports
		if a.isStandardLib(importPath) || strings.HasPrefix(importPath, ".") {
			continue
		}

		// Add to graph
		if _, exists := graph.Nodes[filePkg]; !exists {
			graph.Nodes[filePkg] = []string{}
		}
		graph.Nodes[filePkg] = append(graph.Nodes[filePkg], importPath)
	}

	return nil
}

// getPackageFromPath extracts package name from file path
func (a *Analyzer) getPackageFromPath(filePath string) string {
	// Convert file path to package path
	relPath, err := filepath.Rel(a.projectPath, filePath)
	if err != nil {
		return "unknown"
	}

	// Remove file name
	dir := filepath.Dir(relPath)
	
	// Skip leading "./"
	if strings.HasPrefix(dir, "./") {
		dir = dir[2:]
	}
	
	// Convert to package name
	return strings.ReplaceAll(dir, "/", "/")
}

// isStandardLib checks if import is from Go standard library
func (a *Analyzer) isStandardLib(importPath string) bool {
	// List of common standard library prefixes
	standardLibs := []string{
		"fmt", "os", "io", "strings", "time",
		"net/http", "encoding/json", "database/sql",
		"context", "sync", "log", "bufio",
		"path/filepath", "math", "sort",
	}

	for _, std := range standardLibs {
		if importPath == std || strings.HasPrefix(importPath, std+"/") {
			return true
		}
	}

	// Check for single-word imports (usually stdlib)
	if !strings.Contains(importPath, "/") && !strings.Contains(importPath, ".") {
		return true
	}

	return false
}

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
