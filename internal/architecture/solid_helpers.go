package architecture

import (
	"go/ast"
	"go/parser"
	"go/token"
)

// parseFileToAST parses a Go file into an AST
// This helper function encapsulates the common AST parsing logic
func parseFileToAST(filePath string, fset *token.FileSet) (*ast.File, error) {
	return parser.ParseFile(fset, filePath, nil, parser.ParseComments)
}

// countSwitchStatements counts switch and type switch statements in a function body
func countSwitchStatements(fn *ast.FuncDecl) (regularSwitches, typeSwitches int) {
	if fn.Body == nil {
		return 0, 0
	}

	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch n.(type) {
		case *ast.SwitchStmt:
			regularSwitches++
		case *ast.TypeSwitchStmt:
			typeSwitches++
		}
		return true
	})

	return regularSwitches, typeSwitches
}
