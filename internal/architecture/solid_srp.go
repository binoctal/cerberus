package architecture

import (
	"fmt"
	"go/ast"
	"go/token"
	"path/filepath"
	"strings"
)

// analyzeSRP checks Single Responsibility Principle compliance
func (a *Analyzer) analyzeSRP(filePath string, report *ArchitectureReport) []ArchitectureIssue {
	issues := []ArchitectureIssue{}

	fset := token.NewFileSet()
	node, err := parseFileToAST(filePath, fset)
	if err != nil {
		return issues
	}

	// Collect functions and types
	functions := []string{}
	types := []string{}

	for _, decl := range node.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			functions = append(functions, d.Name.Name)
		case *ast.GenDecl:
			if d.Tok == token.TYPE {
				for _, spec := range d.Specs {
					if typeSpec, ok := spec.(*ast.TypeSpec); ok {
						types = append(types, typeSpec.Name.Name)
					}
				}
			}
		}
	}

	// Match responsibilities using pattern matcher
	matcher := NewPatternMatcher(responsibilityPatterns)
	allIdentifiers := append(functions, types...)
	responsibilities := matcher.collectMatches(allIdentifiers)

	// If multiple responsibilities found, report issue
	if len(responsibilities) > 1 {
		relPath, _ := filepath.Rel(a.projectPath, filePath)

		respNames := []string{}
		for resp := range responsibilities {
			respNames = append(respNames, resp)
		}

		issues = append(issues, ArchitectureIssue{
			ID:          fmt.Sprintf("srp-%s", strings.ReplaceAll(relPath, "/", "-")),
			Type:        ViolatesSRP,
			Severity:    SeverityWarning,
			File:        relPath,
			Line:        0,
			Description: fmt.Sprintf("文件有 %d 个职责: %s", len(responsibilities), strings.Join(respNames, ", ")),
			Rationale:   "单一职责原则（SRP）：一个文件应该只有一个改变的理由",
			Suggestion:  "考虑拆分为多个文件，每个文件负责一个职责",
			Confidence:  0.6,
			Evidence:    respNames,
		})

		report.Metrics.SRPViolations++
	}

	return issues
}
