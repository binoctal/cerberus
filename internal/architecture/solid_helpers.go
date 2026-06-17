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

// findFunctionsInFile extracts all function declarations from an AST
func findFunctionsInFile(node *ast.File) []string {
	functions := []string{}
	for _, decl := range node.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok {
			functions = append(functions, fn.Name.Name)
		}
	}
	return functions
}

// findTypesInFile extracts all type declarations from an AST
func findTypesInFile(node *ast.File) []string {
	types := []string{}
	for _, decl := range node.Decls {
		if genDecl, ok := decl.(*ast.GenDecl); ok && genDecl.Tok == token.TYPE {
			for _, spec := range genDecl.Specs {
				if typeSpec, ok := spec.(*ast.TypeSpec); ok {
					types = append(types, typeSpec.Name.Name)
				}
			}
		}
	}
	return types
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
