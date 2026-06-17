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
		if genDecl, ok := decl.(*ast.GenDecl); ok && genDecl.Tok == token.TYPE {
			count += a.countTypeSpecImplementations(genDecl, ifaceName)
		}
	}

	return count
}

// countTypeSpecImplementations counts implementations in type declarations
func (a *Analyzer) countTypeSpecImplementations(genDecl *ast.GenDecl, ifaceName string) int {
	count := 0
	for _, spec := range genDecl.Specs {
		if typeSpec, ok := spec.(*ast.TypeSpec); ok {
			if a.isStructImplementingInterface(typeSpec, ifaceName) {
				count++
			}
		}
	}
	return count
}

// isStructImplementingInterface checks if a type spec is a struct implementing the interface
func (a *Analyzer) isStructImplementingInterface(typeSpec *ast.TypeSpec, ifaceName string) bool {
	structType, ok := typeSpec.Type.(*ast.StructType)
	if !ok {
		return false
	}

	// Check if struct embeds the interface
	for _, field := range structType.Fields.List {
		if field.Names == nil && a.isEmbeddedInterface(field.Type, ifaceName) {
			return true
		}
	}
	return false
}

// isEmbeddedInterface checks if a field type is an embedded interface
func (a *Analyzer) isEmbeddedInterface(fieldType ast.Expr, ifaceName string) bool {
	switch expr := fieldType.(type) {
	case *ast.Ident:
		// Simple identifier: `type MyStruct ifaceName`
		return expr.Name == ifaceName
	case *ast.SelectorExpr:
		// Selector expression: `type MyStruct pkg.ifaceName`
		if ident, ok := expr.X.(*ast.Ident); ok {
			return ident.Name == ifaceName
		}
	}
	return false
}
