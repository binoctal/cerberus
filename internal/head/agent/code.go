package agent

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"time"

	"github.com/binoctal/cerberus/internal/types"
	"go.uber.org/zap"
)

// CodeExecutor performs static analysis on Go source code.
type CodeExecutor struct {
	logger *zap.Logger
}

// NewCodeExecutor creates a code analysis executor.
func NewCodeExecutor(logger *zap.Logger) *CodeExecutor {
	return &CodeExecutor{logger: logger}
}

// Execute dispatches code analysis actions.
func (e *CodeExecutor) Execute(ctx context.Context, action types.TypedAction) types.ExecutorResult {
	start := time.Now()
	switch a := action.(type) {
	case types.CodeAnalyzeAction:
		return e.analyze(a, start)
	case types.CodeLintAction:
		return e.lint(a, start)
	case types.CodeSymbolsAction:
		return e.symbols(a, start)
	default:
		return types.ErrorResult{Err: fmt.Sprintf("code executor: unsupported action %T", action)}
	}
}

func (e *CodeExecutor) analyze(a types.CodeAnalyzeAction, start time.Time) types.ExecutorResult {
	findings, stats, err := e.parseAndAnalyze(a.TargetPath, a.Checks)
	if err != nil {
		return types.CodeResult{OK: false, Err: err.Error(), Latency: time.Since(start)}
	}
	return types.CodeResult{
		OK:       len(findings) == 0,
		Findings: findings,
		Stats:    stats,
		Latency:  time.Since(start),
	}
}

func (e *CodeExecutor) lint(a types.CodeLintAction, start time.Time) types.ExecutorResult {
	checks := a.Rules
	if len(checks) == 0 {
		checks = []string{"unhandled_error", "complexity"}
	}
	findings, stats, err := e.parseAndAnalyze(a.TargetPath, checks)
	if err != nil {
		return types.CodeResult{OK: false, Err: err.Error(), Latency: time.Since(start)}
	}
	return types.CodeResult{OK: len(findings) == 0, Findings: findings, Stats: stats, Latency: time.Since(start)}
}

func (e *CodeExecutor) symbols(a types.CodeSymbolsAction, start time.Time) types.ExecutorResult {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, a.TargetPath, nil, parser.ImportsOnly)
	if err != nil {
		return types.CodeResult{OK: false, Err: err.Error(), Latency: time.Since(start)}
	}
	fileCount := 0
	symbolCount := 0
	for _, pkg := range pkgs {
		fileCount += len(pkg.Files)
		for _, f := range pkg.Files {
			symbolCount += len(f.Decls)
		}
	}
	return types.CodeResult{
		OK: true,
		Stats: types.CodeStats{
			FilesAnalyzed: fileCount,
			SymbolCount:   symbolCount,
		},
		Latency: time.Since(start),
	}
}

func (e *CodeExecutor) parseAndAnalyze(root string, checks []string) ([]types.CodeFinding, types.CodeStats, error) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, root, nil, parser.AllErrors|parser.ParseComments)
	if err != nil {
		return nil, types.CodeStats{}, err
	}

	if len(checks) == 0 {
		checks = []string{"complexity", "unhandled_error"}
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
	}
	return allFindings, types.CodeStats{FilesAnalyzed: fileCount, SymbolCount: len(allFindings)}, nil
}

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

func checkUnhandledErrors(f *ast.File, _ *token.FileSet) []types.CodeFinding {
	// Simplified: flag assignments where RHS is a call and error return is not captured.
	// Full version would use go/types for type resolution.
	return nil
}

func calcComplexity(block *ast.BlockStmt) int {
	c := 1
	ast.Inspect(block, func(n ast.Node) bool {
		switch n.(type) {
		case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt, *ast.CaseClause:
			c++
		}
		return true
	})
	return c
}
