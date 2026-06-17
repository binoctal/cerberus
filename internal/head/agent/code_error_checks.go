package agent

import (
	"go/ast"
	"go/token"
	"strings"
)

// errAssign represents an error variable assignment.
type errAssign struct {
	name string
	pos  token.Pos
}

// collectErrorAssignments finds all error variable assignments from function calls.
func collectErrorAssignments(body *ast.BlockStmt) []errAssign {
	var assignments []errAssign
	ast.Inspect(body, func(n ast.Node) bool {
		stmt, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}

		if !hasFunctionCall(stmt.Rhs) {
			return true
		}

		for _, lhs := range stmt.Lhs {
			if ident, ok := lhs.(*ast.Ident); ok {
				name := ident.Name
				if isErrorVariableName(name) {
					assignments = append(assignments, errAssign{name: name, pos: stmt.Pos()})
				}
			}
		}
		return true
	})
	return assignments
}

// hasFunctionCall checks if any expression is a function call.
func hasFunctionCall(exprs []ast.Expr) bool {
	for _, expr := range exprs {
		if _, isCall := expr.(*ast.CallExpr); isCall {
			return true
		}
	}
	return false
}

// isErrorVariableName checks if a name is an error variable.
func isErrorVariableName(name string) bool {
	return name == "err" || (strings.HasPrefix(strings.ToLower(name), "err") && len(name) > 3)
}

// isErrorReferenced checks if an error variable is referenced after its assignment.
func isErrorReferenced(body *ast.BlockStmt, ea errAssign) bool {
	referenced := false
	ast.Inspect(body, func(n ast.Node) bool {
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
	return referenced
}
