package architecture

import (
	"go/ast"
	"go/token"
)

// analyzeFunction analyzes a single function
func (a *Analyzer) analyzeFunction(fn *ast.FuncDecl, fset *token.FileSet) *FunctionMetrics {
	metrics := &FunctionMetrics{
		Name:       fn.Name.Name,
		LineNumber: fset.Position(fn.Pos()).Line,
		Cyclomatic: 1, // Base complexity
	}

	// Count parameters
	if fn.Type.Params != nil {
		metrics.Parameters = len(fn.Type.Params.List)
	}

	// Calculate cyclomatic complexity and nesting depth
	if fn.Body != nil {
		v := &complexityVisitor{
			depth:      0,
			maxDepth:   0,
			complexity: 1,
		}
		ast.Walk(v, fn.Body)
		metrics.Cyclomatic = v.complexity
		metrics.NestingDepth = v.maxDepth
	}

	return metrics
}

func (v *complexityVisitor) Visit(node ast.Node) ast.Visitor {
	if node == nil {
		// Exiting a node, decrement depth if we were in control flow
		if v.inControlFlow {
			v.depth--
			v.inControlFlow = false
		}
		return nil
	}

	switch node.(type) {
	case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt:
		v.complexity++
		v.depth++
		if v.depth > v.maxDepth {
			v.maxDepth = v.depth
		}
		// Mark that we're entering control flow
		// Set inControlFlow after processing this node
		return &controlFlowVisitor{parent: v}

	case *ast.CaseClause:
		v.complexity++
		// No depth tracking for case clauses (they're part of switch)
		return v

	default:
		return v
	}
}

func (c *controlFlowVisitor) Visit(node ast.Node) ast.Visitor {
	if node == nil {
		// Exiting the control flow structure
		c.parent.depth--
		return nil
	}

	// For nested control flow, return another controlFlowVisitor
	switch node.(type) {
	case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt:
		c.parent.complexity++
		c.parent.depth++
		if c.parent.depth > c.parent.maxDepth {
			c.parent.maxDepth = c.parent.depth
		}
		return c
	default:
		return c.parent
	}
}
