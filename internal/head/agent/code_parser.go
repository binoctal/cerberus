package agent

import (
	"go/ast"
	"go/parser"
	"go/token"

	"github.com/binoctal/cerberus/internal/types"
)

// parseAndAnalyze performs Go AST analysis with specified checks.
// Returns findings, statistics, and any parsing errors.
func (e *CodeExecutor) parseAndAnalyze(root string, checks []string) ([]types.CodeFinding, types.CodeStats, error) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, root, nil, parser.AllErrors|parser.ParseComments) //nolint:staticcheck // SA1019
	if err != nil {
		return nil, types.CodeStats{}, err
	}

	if len(checks) == 0 {
		checks = []string{"complexity", "unhandled_error", "dead_code"}
	}
	checkFns := map[string]func(*ast.File, *token.FileSet) []types.CodeFinding{
		"complexity":      checkComplexity,
		"unhandled_error": checkUnhandledErrors,
	}

	var allFindings []types.CodeFinding
	fileCount := 0
	for _, pkg := range pkgs {
		for path, f := range pkg.Files {
			fileCount++
			for _, check := range checks {
				if fn, ok := checkFns[check]; ok {
					findings := fn(f, fset)
					for i := range findings {
						findings[i].File = path
					}
					allFindings = append(allFindings, findings...)
				}
			}
		}

		// Package-level checks (need all files).
		if containsCheck(checks, "dead_code") {
			findings := checkDeadCode(pkg, fset)
			allFindings = append(allFindings, findings...)
		}
	}
	return allFindings, types.CodeStats{FilesAnalyzed: fileCount, SymbolCount: len(allFindings)}, nil
}
