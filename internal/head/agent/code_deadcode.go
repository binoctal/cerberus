package agent

import (
	"go/ast"
)

// collectDeclaredFunctions collects unexported function declarations.
func collectDeclaredFunctions(pkg *ast.Package) map[string]string { //nolint:staticcheck // SA1019 // ast.Package refactor to go/types pending
	declared := make(map[string]string)
	for path, f := range pkg.Files {
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			name := fn.Name.Name
			if shouldSkipFunction(name) {
				continue
			}
			declared[name] = path
		}
	}
	return declared
}

// shouldSkipFunction checks if a function should be excluded from dead code analysis.
func shouldSkipFunction(name string) bool {
	return ast.IsExported(name) ||
		name == "main" ||
		name == "init" ||
		startsWithTestPrefix(name)
}

// startsWithTestPrefix checks if a function has a test prefix.
func startsWithTestPrefix(name string) bool {
	return hasPrefix(name, "Test") ||
		hasPrefix(name, "Benchmark") ||
		hasPrefix(name, "Fuzz")
}

// hasPrefix checks if a string has a given prefix.
func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

// collectCalledFunctions collects all function call references.
func collectCalledFunctions(pkg *ast.Package) map[string]bool { //nolint:staticcheck // SA1019 // ast.Package refactor to go/types pending
	called := make(map[string]bool)
	for _, f := range pkg.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			switch expr := n.(type) {
			case *ast.CallExpr:
				switch fun := expr.Fun.(type) {
				case *ast.Ident:
					called[fun.Name] = true
				case *ast.SelectorExpr:
					called[fun.Sel.Name] = true
				}
			}
			return true
		})
	}
	return called
}
