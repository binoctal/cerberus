package architecture

import (
	"go/ast"
	"go/parser"
	"go/token"
)

// analyzeFileInterfaces analyzes interfaces in a single file
func (a *Analyzer) analyzeFileInterfaces(filePath string, interfaces map[string]*InterfaceInfo) error {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		return err
	}

	// Find all interface declarations
	for _, decl := range node.Decls {
		ifaceDecl, ok := decl.(*ast.GenDecl)
		if !ok || ifaceDecl.Tok != token.TYPE {
			continue
		}

		for _, spec := range ifaceDecl.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}

			a.processTypeSpec(filePath, typeSpec, fset, interfaces)
		}
	}

	return nil
}
