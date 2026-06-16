package architecture

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// Responsibility represents a code responsibility
type Responsibility struct {
	Name        string
	Keywords    []string
	Examples    []string
}

// Common responsibility patterns
var responsibilityPatterns = []Responsibility{
	{"parsing", []string{"parse", "read", "decode", "unmarshal", "extract"}, []string{}},
	{"validation", []string{"validate", "check", "verify", "ensure", "confirm"}, []string{}},
	{"persistence", []string{"save", "persist", "store", "write", "insert", "update", "delete"}, []string{}},
	{"calculation", []string{"calculate", "compute", "evaluate", "process"}, []string{}},
	{"rendering", []string{"render", "display", "show", "format", "print"}, []string{}},
	{"network", []string{"fetch", "request", "send", "receive", "connect"}, []string{}},
	{"configuration", []string{"config", "setting", "option", "parameter"}, []string{}},
	{"logging", []string{"log", "debug", "trace", "info", "warn", "error"}, []string{}},
	{"testing", []string{"test", "mock", "stub", "fixture"}, []string{}},
	{"caching", []string{"cache", "buffer", "store"}, []string{}},
}

// analyzeSOLID analyzes SOLID principle compliance
func (a *Analyzer) analyzeSOLID(report *ArchitectureReport) error {
	// Analyze each Go file
	err := filepath.Walk(a.projectPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories and non-Go files
		if info.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}

		// Skip test files and vendor
		if strings.Contains(path, "_test.go") || strings.Contains(path, "vendor/") {
			return nil
		}

		// Analyze SRP compliance
		srpIssues := a.analyzeSRP(path, report)
		report.Issues = append(report.Issues, srpIssues...)

		// Analyze OCP compliance
		ocpIssues := a.analyzeOCP(path, report)
		report.Issues = append(report.Issues, ocpIssues...)

		return nil
	})

	if err != nil {
		return err
	}

	return nil
}

// analyzeSRP checks Single Responsibility Principle compliance
func (a *Analyzer) analyzeSRP(filePath string, report *ArchitectureReport) []ArchitectureIssue {
	issues := []ArchitectureIssue{}

	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		return issues
	}

	// Identify responsibilities in this file
	responsibilities := make(map[string]bool)
	functions := []string{}
	types := []string{}

	// Analyze functions
	for _, decl := range node.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			fnName := d.Name.Name
			functions = append(functions, fnName)
			
			// Check function name against responsibility patterns
			for _, pattern := range responsibilityPatterns {
				for _, keyword := range pattern.Keywords {
					if strings.Contains(strings.ToLower(fnName), keyword) {
						responsibilities[pattern.Name] = true
						// Store example
						pattern.Examples = append(pattern.Examples, fnName)
					}
				}
			}

		case *ast.GenDecl:
			if d.Tok == token.TYPE {
				for _, spec := range d.Specs {
					if typeSpec, ok := spec.(*ast.TypeSpec); ok {
						typeName := typeSpec.Name.Name
						types = append(types, typeName)
						
						// Check type name against responsibility patterns
						for _, pattern := range responsibilityPatterns {
							for _, keyword := range pattern.Keywords {
								if strings.Contains(strings.ToLower(typeName), keyword) {
									responsibilities[pattern.Name] = true
									pattern.Examples = append(pattern.Examples, typeName)
								}
							}
						}
					}
				}
			}
		}
	}

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

// analyzeOCP checks Open/Closed Principle compliance
func (a *Analyzer) analyzeOCP(filePath string, report *ArchitectureReport) []ArchitectureIssue {
	issues := []ArchitectureIssue{}

	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		return issues
	}

	// Count switch/if-else chains that could be refactored to polymorphism
	switchCount := 0
	typeSwitchCount := 0
	
	for _, decl := range node.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok {
			if fn.Body != nil {
				switchInFunc := 0
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					switch n.(type) {
					case *ast.SwitchStmt:
						switchInFunc++
						switchCount++
					case *ast.TypeSwitchStmt:
						typeSwitchCount++
					}
					return true
				})

				// If function has multiple switches, might violate OCP
				if switchInFunc > 2 {
					relPath, _ := filepath.Rel(a.projectPath, filePath)
					lineNo := fset.Position(fn.Pos()).Line
					
					issues = append(issues, ArchitectureIssue{
						ID:          fmt.Sprintf("ocp-%s-%d", strings.ReplaceAll(relPath, "/", "-"), lineNo),
						Type:        ViolatesOCP,
						Severity:    SeverityInfo,
						File:        relPath,
						Line:        lineNo,
						Description: fmt.Sprintf("函数 %s 包含 %d 个 switch 语句", fn.Name.Name, switchInFunc),
						Rationale:   "开闭原则（OCP）：软件实体应该对扩展开放，对修改关闭",
						Suggestion:  "考虑使用多态或策略模式替代复杂的 switch 逻辑",
						Confidence:  0.5,
						Evidence:    []string{fmt.Sprintf("switch 语句数: %d", switchInFunc)},
					})

					report.Metrics.OCPViolations++
				}
			}
		}
	}

	return issues
}
