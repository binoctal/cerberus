package architecture

import (
	"go/ast"
	"go/parser"
	"go/token"
)

// CountFunctionsInFile counts functions in a file
func (a *Analyzer) CountFunctionsInFile(filePath string) (int, error) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		return 0, err
	}

	count := 0
	for _, decl := range node.Decls {
		if _, ok := decl.(*ast.FuncDecl); ok {
			count++
		}
	}

	return count, nil
}
