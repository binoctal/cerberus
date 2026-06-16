package agent

import (
	"fmt"
	"go/ast"
	"go/token"
	"strings"

	"github.com/binoctal/cerberus/internal/types"
)

// checkComplexity identifies functions with cyclomatic complexity > 15.
func checkComplexity(f *ast.File, fset *token.FileSet) []types.CodeFinding {
	var findings []types.CodeFinding
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		complexity := calcComplexity(fn.Body)
		if complexity > 15 {
			pos := fset.Position(fn.Pos())
			findings = append(findings, types.CodeFinding{
				Line:     pos.Line,
				Rule:     "high_complexity",
				Message:  fmt.Sprintf("function %s has complexity %d (threshold: 15)", fn.Name.Name, complexity),
				Severity: "warning",
			})
		}
	}
	return findings
}

// checkUnhandledErrors finds error variables assigned but never checked.
func checkUnhandledErrors(f *ast.File, fset *token.FileSet) []types.CodeFinding {
	var findings []types.CodeFinding
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}

		// Phase 1: Collect all err variables assigned from function calls.
		type errAssign struct {
			name string
			pos  token.Pos
		}
		var assignments []errAssign
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			stmt, ok := n.(*ast.AssignStmt)
			if !ok {
				return true
			}
			hasCall := false
			for _, rhs := range stmt.Rhs {
				if _, isCall := rhs.(*ast.CallExpr); isCall {
					hasCall = true
					break
				}
			}
			if !hasCall {
				return true
			}
			for _, lhs := range stmt.Lhs {
				if ident, ok := lhs.(*ast.Ident); ok {
					name := ident.Name
					if name == "err" || (strings.HasPrefix(strings.ToLower(name), "err") && len(name) > 3) {
						assignments = append(assignments, errAssign{name: name, pos: stmt.Pos()})
					}
				}
			}
			return true
		})

		if len(assignments) == 0 {
			continue
		}

		// Phase 2: Walk the function body and check if each err name appears
		// in any context other than its own assignment LHS.
		for _, ea := range assignments {
			referenced := false
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				ident, ok := n.(*ast.Ident)
				if !ok {
					return true
				}
				if ident.Name != ea.name {
					return true
				}
				// Check if this ident is the LHS of its own assignment (skip it).
				if ident.Pos() == ea.pos || (ident.Pos() >= ea.pos && ident.Pos() <= ea.pos+token.Pos(len(ea.name))) {
					return true
				}
				referenced = true
				return false
			})
			if !referenced {
				p := fset.Position(ea.pos)
				findings = append(findings, types.CodeFinding{
					Line:     p.Line,
					Rule:     "unhandled_error",
					Message:  fmt.Sprintf("error variable %s returned from call may not be handled", ea.name),
					Severity: "warning",
				})
			}
		}
	}
	return findings
}

// checkDeadCode finds unexported functions that are declared but never called
// within the same package. Exported functions, main, init, and test functions
// are excluded since they may be called externally.
func checkDeadCode(pkg *ast.Package, fset *token.FileSet) []types.CodeFinding { //nolint:staticcheck // SA1019
	declared := make(map[string]string) // name -> file path
	called := make(map[string]bool)

	for path, f := range pkg.Files {
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			name := fn.Name.Name
			// Skip exported, main, init, and test functions.
			if ast.IsExported(name) || name == "main" || name == "init" ||
				strings.HasPrefix(name, "Test") ||
				strings.HasPrefix(name, "Benchmark") ||
				strings.HasPrefix(name, "Fuzz") {
				continue
			}
			declared[name] = path
		}

		// Collect all call references.
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

	var findings []types.CodeFinding
	for name, path := range declared {
		if !called[name] {
			findings = append(findings, types.CodeFinding{
				File:     path,
				Rule:     "dead_code",
				Message:  fmt.Sprintf("function %s is declared but never called within this package", name),
				Severity: "info",
			})
		}
	}
	return findings
}
