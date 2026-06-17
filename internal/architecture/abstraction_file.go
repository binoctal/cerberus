package architecture

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
)

// analyzeFileInterfaces analyzes interfaces in a single file
func (a *Analyzer) analyzeFileInterfaces(filePath string, interfaces map[string]*InterfaceInfo) error {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		return err
	}

	relPath, _ := filepath.Rel(a.projectPath, filePath)

	// Find all interface declarations
	for _, decl := range node.Decls {
		if ifaceDecl, ok := decl.(*ast.GenDecl); ok {
			if ifaceDecl.Tok == token.TYPE {
				for _, spec := range ifaceDecl.Specs {
					if typeSpec, ok := spec.(*ast.TypeSpec); ok {
						if ifaceType, ok := typeSpec.Type.(*ast.InterfaceType); ok {
							ifaceName := typeSpec.Name.Name

							// Count methods
							methodCount := 0
							if ifaceType.Methods != nil {
								methodCount = len(ifaceType.Methods.List)
							}

							// Check if interface already exists
							if _, exists := interfaces[ifaceName]; !exists {
								interfaces[ifaceName] = &InterfaceInfo{
									Name:       ifaceName,
									FilePath:   relPath,
									LineNumber: fset.Position(typeSpec.Pos()).Line,
									Methods:    methodCount,
								}
							}

							// Count implementations (simple heuristic: check for struct embedding)
							// This is a basic implementation - full analysis would require more sophisticated analysis
							implementations := a.countImplementations(filePath, ifaceName, node)
							interfaces[ifaceName].Implementations += implementations
						}
					}
				}
			}
		}
	}

	return nil
}
