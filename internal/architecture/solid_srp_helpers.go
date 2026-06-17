package architecture

import (
	"fmt"
	"go/ast"
	"go/token"
	"path/filepath"
	"strings"
)

// fileDeclarations holds functions and types from a file
type fileDeclarations struct {
	functions []string
	types     []string
}

// collectDeclarations extracts function and type declarations from AST
func collectDeclarations(node *ast.File) fileDeclarations {
	decls := fileDeclarations{
		functions: []string{},
		types:     []string{},
	}

	for _, decl := range node.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			decls.functions = append(decls.functions, d.Name.Name)
		case *ast.GenDecl:
			if d.Tok == token.TYPE {
				for _, spec := range d.Specs {
					if typeSpec, ok := spec.(*ast.TypeSpec); ok {
						decls.types = append(decls.types, typeSpec.Name.Name)
					}
				}
			}
		}
	}

	return decls
}

// reportSRPViolation reports when a file violates SRP
func reportSRPViolation(filePath string, projectPath string, responsibilities map[string]bool, report *ArchitectureReport) ArchitectureIssue {
	relPath, _ := filepath.Rel(projectPath, filePath)

	respNames := []string{}
	for resp := range responsibilities {
		respNames = append(respNames, resp)
	}

	issue := ArchitectureIssue{
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
	}

	report.Metrics.SRPViolations++
	return issue
}
