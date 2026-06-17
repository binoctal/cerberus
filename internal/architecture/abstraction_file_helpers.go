package architecture

import (
	"go/ast"
	"go/token"
	"path/filepath"
)

// interfaceSpec holds interface type specification info
type interfaceSpec struct {
	name       string
	methods    int
	lineNumber int
}

// extractInterfaceSpec extracts interface specification from type spec
func extractInterfaceSpec(typeSpec *ast.TypeSpec, fset *token.FileSet) *interfaceSpec {
	if ifaceType, ok := typeSpec.Type.(*ast.InterfaceType); ok {
		methodCount := 0
		if ifaceType.Methods != nil {
			methodCount = len(ifaceType.Methods.List)
		}

		return &interfaceSpec{
			name:       typeSpec.Name.Name,
			methods:    methodCount,
			lineNumber: fset.Position(typeSpec.Pos()).Line,
		}
	}
	return nil
}

// registerInterface registers or updates an interface in the collection
func (a *Analyzer) registerInterface(spec *interfaceSpec, filePath string, interfaces map[string]*InterfaceInfo, implCount int) {
	relPath, _ := filepath.Rel(a.projectPath, filePath)

	if _, exists := interfaces[spec.name]; !exists {
		interfaces[spec.name] = &InterfaceInfo{
			Name:       spec.name,
			FilePath:   relPath,
			LineNumber: spec.lineNumber,
			Methods:    spec.methods,
		}
	}

	interfaces[spec.name].Implementations += implCount
}

// processTypeSpec processes a single type specification for interfaces
func (a *Analyzer) processTypeSpec(filePath string, spec *ast.TypeSpec, fset *token.FileSet, interfaces map[string]*InterfaceInfo) {
	ifaceSpec := extractInterfaceSpec(spec, fset)
	if ifaceSpec == nil {
		return
	}

	implCount := a.countImplementations(filePath, ifaceSpec.name, nil)
	a.registerInterface(ifaceSpec, filePath, interfaces, implCount)
}
