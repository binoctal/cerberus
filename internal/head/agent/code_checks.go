package agent

import (
	"fmt"
	"go/ast"
	"go/token"

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

		assignments := collectErrorAssignments(fn.Body)
		for _, ea := range assignments {
			if !isErrorReferenced(fn.Body, ea) {
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
	declared := collectDeclaredFunctions(pkg)
	called := collectCalledFunctions(pkg)

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
