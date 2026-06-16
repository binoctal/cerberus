package agent

import (
	"go/ast"
)

// containsCheck returns true if the check name is in the list.
func containsCheck(checks []string, name string) bool {
	for _, c := range checks {
		if c == name {
			return true
		}
	}
	return false
}

// calcComplexity computes cyclomatic complexity for a block of code.
func calcComplexity(block *ast.BlockStmt) int {
	if block == nil {
		return 1
	}
	complexity := 1
	for _, stmt := range block.List {
		ast.Inspect(stmt, func(n ast.Node) bool {
			switch n.(type) {
			case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt:
				complexity++
			case *ast.CaseClause:
				complexity++
			}
			return true
		})
	}
	return complexity
}
