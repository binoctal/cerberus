package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go/parser"
	"go/token"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/sandbox"
	"github.com/binoctal/cerberus/internal/types"
)

// CodeExecutor performs static analysis on source code.
// Go uses built-in AST analysis; other languages delegate to CLI tools via sandbox.
type CodeExecutor struct {
	logger  *zap.Logger
	sandbox sandbox.Sandbox
}

// NewCodeExecutor creates a code analysis executor.
func NewCodeExecutor(sb sandbox.Sandbox, logger *zap.Logger) *CodeExecutor {
	return &CodeExecutor{logger: logger, sandbox: sb}
}

// Execute dispatches code analysis actions.
func (e *CodeExecutor) Execute(ctx context.Context, action types.TypedAction) types.ExecutorResult {
	start := time.Now()
	switch a := action.(type) {
	case types.CodeAnalyzeAction:
		return e.analyze(ctx, a, start)
	case types.CodeLintAction:
		return e.lint(ctx, a, start)
	case types.CodeSymbolsAction:
		return e.symbols(ctx, a, start)
	default:
		return types.ErrorResult{Err: fmt.Sprintf("code executor: unsupported action %T", action)}
	}
}

// isGoLang returns true if the language is Go or unspecified (default).
func isGoLang(lang string) bool {
	return lang == "" || lang == "Go"
}

// isPython returns true if the language matches Python.
func isPython(lang string) bool {
	return strings.EqualFold(lang, "Python")
}

// isJavaScript returns true if the language matches JavaScript or TypeScript.
func isJavaScript(lang string) bool {
	lang = strings.ToLower(lang)
	return lang == "javascript" || lang == "typescript" || lang == "javascript/typescript"
}

func (e *CodeExecutor) analyze(ctx context.Context, a types.CodeAnalyzeAction, start time.Time) types.ExecutorResult {
	if !isGoLang(a.Language) {
		return e.cliAnalysis(ctx, a.TargetPath, a.Language, a.Checks, start)
	}
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

func (e *CodeExecutor) lint(ctx context.Context, a types.CodeLintAction, start time.Time) types.ExecutorResult {
	if !isGoLang(a.Language) {
		return e.cliAnalysis(ctx, a.TargetPath, a.Language, a.Rules, start)
	}
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

func (e *CodeExecutor) symbols(ctx context.Context, a types.CodeSymbolsAction, start time.Time) types.ExecutorResult {
	if !isGoLang(a.Language) {
		return e.cliSymbols(ctx, a.TargetPath, a.Language, start)
	}
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, a.TargetPath, nil, parser.ImportsOnly) //nolint:staticcheck // SA1019
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
