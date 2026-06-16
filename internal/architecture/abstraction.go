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

// InterfaceInfo represents information about a Go interface
type InterfaceInfo struct {
	Name         string
	FilePath     string
	LineNumber   int
	Implementations int // Number of concrete implementations
	Methods       int
}

// analyzeAbstractions analyzes abstraction usage
func (a *Analyzer) analyzeAbstractions(report *ArchitectureReport) error {
	// Collect all interfaces and their implementations
	interfaces := make(map[string]*InterfaceInfo)
	
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

		// Parse file to find interfaces
		if err := a.analyzeFileInterfaces(path, interfaces); err != nil {
			// Continue with other files
			return nil
		}

		return nil
	})

	if err != nil {
		return err
	}

	// Check for premature abstractions
	for _, iface := range interfaces {
		report.Metrics.TotalInterfaces++
		
		// Check: Single implementation (possible premature abstraction)
		if iface.Implementations == 1 {
			report.Issues = append(report.Issues, ArchitectureIssue{
				ID:          fmt.Sprintf("single-impl-%s", iface.Name),
				Type:        PrematureAbstraction,
				Severity:    SeverityInfo,
				File:        iface.FilePath,
				Line:        iface.LineNumber,
				Description: fmt.Sprintf("接口 %s 只有 1 个实现", iface.Name),
				Rationale:   "单一实现的接口可能是过早抽象，YAGNI原则",
				Suggestion:  "考虑在真正需要多实现时再抽象，或使用具体类型",
				Confidence:  0.7,
				Evidence:    []string{fmt.Sprintf("实现数: %d", iface.Implementations)},
			})
			report.Metrics.SingleImplInterfaces++
		}

		// Check: Unused abstraction (no implementations found in analysis)
		if iface.Implementations == 0 {
			report.Issues = append(report.Issues, ArchitectureIssue{
				ID:          fmt.Sprintf("unused-interface-%s", iface.Name),
				Type:        PrematureAbstraction,
				Severity:    SeverityWarning,
				File:        iface.FilePath,
				Line:        iface.LineNumber,
				Description: fmt.Sprintf("接口 %s 未找到实现", iface.Name),
				Rationale:   "未使用的抽象增加复杂性",
				Suggestion:  "删除接口或实现它，如果是为了未来预留请记录理由",
				Confidence:  0.5,
				Evidence:    []string{"可能实现在分析范围外"},
			})
			report.Metrics.UnusedAbstractions++
		}
	}

	return nil
}

// analyzeFileInterfaces analyzes interfaces in a single file
func (a *Analyzer) analyzeFileInterfaces(filePath string, interfaces map[string]*InterfaceInfo) error {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		return err
	}

	relPath, _ := filepath.Rel(a.projectPath, filePath)

	// Find all interface declarations
	for _, decl := range node.Decls {
		if ifaceDecl, ok := decl.(*ast.GenDecl); ok {
			if ifaceDecl.Tok == token.TYPE {
				for _, spec := range ifaceDecl.Specs {
					if typeSpec, ok := spec.(*ast.TypeSpec); ok {
						if ifaceType, ok := typeSpec.Type.(*ast.InterfaceType); ok {
							ifaceName := typeSpec.Name.Name
							
							// Count methods
							methodCount := 0
							if ifaceType.Methods != nil {
								methodCount = len(ifaceType.Methods.List)
							}

							// Check if interface already exists
							if _, exists := interfaces[ifaceName]; !exists {
								interfaces[ifaceName] = &InterfaceInfo{
									Name:       ifaceName,
									FilePath:   relPath,
									LineNumber: fset.Position(typeSpec.Pos()).Line,
									Methods:    methodCount,
								}
							}

							// Count implementations (simple heuristic: check for struct embedding)
							// This is a basic implementation - full analysis would require more sophisticated analysis
							implementations := a.countImplementations(filePath, ifaceName, node)
							interfaces[ifaceName].Implementations += implementations
						}
					}
				}
			}
		}
	}

	return nil
}

// countImplementations counts concrete implementations of an interface
func (a *Analyzer) countImplementations(filePath string, ifaceName string, fileNode *ast.File) int {
	count := 0

	// Look for struct types that embed the interface
	for _, decl := range fileNode.Decls {
		if genDecl, ok := decl.(*ast.GenDecl); ok {
			if genDecl.Tok == token.TYPE {
				for _, spec := range genDecl.Specs {
					if typeSpec, ok := spec.(*ast.TypeSpec); ok {
						if structType, ok := typeSpec.Type.(*ast.StructType); ok {
							// Check if struct embeds the interface
							for _, field := range structType.Fields.List {
								if field.Names == nil {
									// Embedded field
									if selector, ok := field.Type.(*ast.Ident); ok {
										if selector.Name == ifaceName {
											count++
										}
									} else if selector, ok := field.Type.(*ast.SelectorExpr); ok {
										if ident, ok := selector.X.(*ast.Ident); ok {
											if ident.Name == ifaceName {
												count++
											}
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}

	return count
}
