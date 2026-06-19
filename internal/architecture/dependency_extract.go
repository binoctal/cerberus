package architecture

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
)

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
	dir = strings.TrimPrefix(dir, "./")

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
