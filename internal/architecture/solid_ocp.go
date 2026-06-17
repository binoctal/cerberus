package architecture

import (
	"fmt"
	"go/ast"
	"go/token"
	"path/filepath"
	"strings"
)

// analyzeOCP checks Open/Closed Principle compliance
func (a *Analyzer) analyzeOCP(filePath string, report *ArchitectureReport) []ArchitectureIssue {
	issues := []ArchitectureIssue{}

	fset := token.NewFileSet()
	node, err := parseFileToAST(filePath, fset)
	if err != nil {
		return issues
	}

	// Analyze each function for OCP violations
	for _, decl := range node.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}

		// Count switch statements in this function
		regularSwitches, typeSwitches := countSwitchStatements(fn)
		_ = typeSwitches // Type switches are OK for OCP

		// If function has multiple switches, might violate OCP
		if regularSwitches > 2 {
			relPath, _ := filepath.Rel(a.projectPath, filePath)
			lineNo := fset.Position(fn.Pos()).Line

			issues = append(issues, ArchitectureIssue{
				ID:          fmt.Sprintf("ocp-%s-%d", strings.ReplaceAll(relPath, "/", "-"), lineNo),
				Type:        ViolatesOCP,
				Severity:    SeverityInfo,
				File:        relPath,
				Line:        lineNo,
				Description: fmt.Sprintf("函数 %s 包含 %d 个 switch 语句", fn.Name.Name, regularSwitches),
				Rationale:   "开闭原则（OCP）：软件实体应该对扩展开放，对修改关闭",
				Suggestion:  "考虑使用多态或策略模式替代复杂的 switch 逻辑",
				Confidence:  0.5,
				Evidence:    []string{fmt.Sprintf("switch 语句数: %d", regularSwitches)},
			})

			report.Metrics.OCPViolations++
		}
	}

	return issues
}
