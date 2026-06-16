package architecture

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
)

// FunctionMetrics represents metrics for a single function
type FunctionMetrics struct {
	Name          string
	Parameters    int
	Cyclomatic    int
	NestingDepth  int
	LineNumber    int
}

// analyzeFunctionComplexity analyzes complexity of functions in a file
func (a *Analyzer) analyzeFunctionComplexity(filePath string, report *ArchitectureReport) ([]ArchitectureIssue, error) {
	issues := []ArchitectureIssue{}

	// Parse Go file
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		// If parsing fails, return nil (don't fail entire analysis)
		return nil, nil
	}

	// Analyze each function
	for _, decl := range node.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok {
			metrics := a.analyzeFunction(fn, fset)

			// Check: Too many parameters
			if metrics.Parameters > 5 {
				relPath, _ := filepath.Rel(a.projectPath, filePath)
				issues = append(issues, ArchitectureIssue{
					ID:       fmt.Sprintf("too-many-params-%s-%d", relPath, metrics.LineNumber),
					Type:     OverEngineering,
					Severity: SeverityWarning,
					File:     relPath,
					Line:     metrics.LineNumber,
					Description: fmt.Sprintf("函数 %s 有 %d 个参数，超过阈值 5", metrics.Name, metrics.Parameters),
					Rationale: "过多参数使函数难以理解和测试",
					Suggestion: "考虑使用参数对象或重构函数",
					Confidence: 1.0,
					Evidence:   []string{fmt.Sprintf("实际参数数: %d", metrics.Parameters)},
				})
			}

			// Check: High cyclomatic complexity
			if metrics.Cyclomatic > 10 {
				relPath, _ := filepath.Rel(a.projectPath, filePath)
				issues = append(issues, ArchitectureIssue{
					ID:       fmt.Sprintf("high-complexity-%s-%d", relPath, metrics.LineNumber),
					Type:     OverEngineering,
					Severity: SeverityWarning,
					File:     relPath,
					Line:     metrics.LineNumber,
					Description: fmt.Sprintf("函数 %s 的圈复杂度为 %d，超过阈值 10", metrics.Name, metrics.Cyclomatic),
					Rationale: "高圈复杂度表示函数逻辑复杂，难以测试和维护",
					Suggestion: "考虑拆分函数或简化逻辑",
					Confidence: 1.0,
					Evidence:   []string{fmt.Sprintf("实际复杂度: %d", metrics.Cyclomatic)},
				})
			}

			// Check: Deep nesting
			if metrics.NestingDepth > 4 {
				relPath, _ := filepath.Rel(a.projectPath, filePath)
				issues = append(issues, ArchitectureIssue{
					ID:       fmt.Sprintf("deep-nesting-%s-%d", relPath, metrics.LineNumber),
					Type:     OverEngineering,
					Severity: SeverityInfo,
					File:     relPath,
					Line:     metrics.LineNumber,
					Description: fmt.Sprintf("函数 %s 的嵌套深度为 %d，超过阈值 4", metrics.Name, metrics.NestingDepth),
					Rationale: "深层嵌套使代码难以理解和维护",
					Suggestion: "考虑使用早期返回或提取函数",
					Confidence: 1.0,
					Evidence:   []string{fmt.Sprintf("实际嵌套深度: %d", metrics.NestingDepth)},
				})
			}

			// Update max metrics
			if metrics.Parameters > report.Metrics.MaxParameters {
				report.Metrics.MaxParameters = metrics.Parameters
			}
			if metrics.NestingDepth > report.Metrics.MaxNestingDepth {
				report.Metrics.MaxNestingDepth = metrics.NestingDepth
			}
		}
	}

	return issues, nil
}

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

// complexityVisitor visits AST nodes to calculate complexity
type complexityVisitor struct {
	depth       int
	maxDepth    int
	complexity  int
	inControlFlow bool // Track if we're inside a control flow structure
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

// controlFlowVisitor handles visiting children of control flow structures
type controlFlowVisitor struct {
	parent *complexityVisitor
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

// CountFunctionsInFile counts functions in a file
func (a *Analyzer) CountFunctionsInFile(filePath string) (int, error) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		return 0, err
	}

	count := 0
	for _, decl := range node.Decls {
		if _, ok := decl.(*ast.FuncDecl); ok {
			count++
		}
	}

	return count, nil
}
