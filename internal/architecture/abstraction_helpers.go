package architecture

import (
	"go/ast"
	"go/token"
)

// countImplementations counts concrete implementations of an interface
func (a *Analyzer) countImplementations(filePath string, ifaceName string, fileNode *ast.File) int {
	count := 0

	// Look for struct types that embed the interface
	for _, decl := range fileNode.Decls {
		if genDecl, ok := decl.(*ast.GenDecl); ok {
			if genDecl.Tok == token.TYPE {
				for _, spec := range genDecl.Specs {
					if typeSpec, ok := spec.(*ast.TypeSpec); ok {
						if structType, ok := typeSpec.Type.(*ast.StructType); ok {
							// Check if struct embeds the interface
							for _, field := range structType.Fields.List {
								if field.Names == nil {
									// Embedded field
									if selector, ok := field.Type.(*ast.Ident); ok {
										if selector.Name == ifaceName {
											count++
										}
									} else if selector, ok := field.Type.(*ast.SelectorExpr); ok {
										if ident, ok := selector.X.(*ast.Ident); ok {
											if ident.Name == ifaceName {
												count++
											}
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}

	return count
}
