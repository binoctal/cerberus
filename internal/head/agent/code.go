package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"time"

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

func checkUnhandledErrors(f *ast.File, fset *token.FileSet) []types.CodeFinding {
	var findings []types.CodeFinding
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}

		// Phase 1: Collect all err variables assigned from function calls.
		type errAssign struct {
			name string
			pos  token.Pos
		}
		var assignments []errAssign
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			stmt, ok := n.(*ast.AssignStmt)
			if !ok {
				return true
			}
			hasCall := false
			for _, rhs := range stmt.Rhs {
				if _, isCall := rhs.(*ast.CallExpr); isCall {
					hasCall = true
					break
				}
			}
			if !hasCall {
				return true
			}
			for _, lhs := range stmt.Lhs {
				if ident, ok := lhs.(*ast.Ident); ok {
					name := ident.Name
					if name == "err" || (strings.HasPrefix(strings.ToLower(name), "err") && len(name) > 3) {
						assignments = append(assignments, errAssign{name: name, pos: stmt.Pos()})
					}
				}
			}
			return true
		})

		if len(assignments) == 0 {
			continue
		}

		// Phase 2: Walk the function body and check if each err name appears
		// in any context other than its own assignment LHS.
		for _, ea := range assignments {
			referenced := false
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				ident, ok := n.(*ast.Ident)
				if !ok {
					return true
				}
				if ident.Name != ea.name {
					return true
				}
				// Check if this ident is the LHS of its own assignment (skip it).
				if ident.Pos() == ea.pos || (ident.Pos() >= ea.pos && ident.Pos() <= ea.pos+token.Pos(len(ea.name))) {
					return true
				}
				referenced = true
				return false
			})
			if !referenced {
				p := fset.Position(ea.pos)
				findings = append(findings, types.CodeFinding{
					Line:     p.Line,
					Rule:     "unhandled_error",
					Message:  fmt.Sprintf("error variable %s returned from call may not be handled", ea.name),
					Severity: "warning",
				})
			}
		}
	}
	return findings
}

// checkDeadCode finds unexported functions that are declared but never called
// within the same package. Exported functions, main, init, and test functions
// are excluded since they may be called externally.
func checkDeadCode(pkg *ast.Package, fset *token.FileSet) []types.CodeFinding { //nolint:staticcheck // SA1019
	declared := make(map[string]string) // name -> file path
	called := make(map[string]bool)

	for path, f := range pkg.Files {
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			name := fn.Name.Name
			// Skip exported, main, init, and test functions.
			if ast.IsExported(name) || name == "main" || name == "init" ||
				strings.HasPrefix(name, "Test") ||
				strings.HasPrefix(name, "Benchmark") ||
				strings.HasPrefix(name, "Fuzz") {
				continue
			}
			declared[name] = path
		}

		// Collect all call references.
		ast.Inspect(f, func(n ast.Node) bool {
			switch expr := n.(type) {
			case *ast.CallExpr:
				switch fun := expr.Fun.(type) {
				case *ast.Ident:
					called[fun.Name] = true
				case *ast.SelectorExpr:
					called[fun.Sel.Name] = true
				}
			}
			return true
		})
	}

	var findings []types.CodeFinding
	for name, path := range declared {
		if !called[name] {
			findings = append(findings, types.CodeFinding{
				File:     path,
				Rule:     "dead_code",
				Message:  fmt.Sprintf("function %s is declared but never called within this package", name),
				Severity: "info",
			})
		}
	}
	return findings
}

// containsCheck returns true if the check name is in the list.
func containsCheck(checks []string, name string) bool {
	for _, c := range checks {
		if c == name {
			return true
		}
	}
	return false
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

// --- CLI-based analysis for non-Go languages ---

// cliAnalysis runs an external linter (ruff/eslint) and parses JSON output.
func (e *CodeExecutor) cliAnalysis(ctx context.Context, targetPath, language string, checks []string, start time.Time) types.ExecutorResult {
	var cmd string
	var args []string

	switch {
	case isPython(language):
		cmd = "ruff"
		args = []string{"check", "--output-format", "json", targetPath}
	case isJavaScript(language):
		cmd = "npx"
		args = []string{"eslint", "--format", "json", targetPath}
	default:
		return types.CodeResult{
			OK:      false,
			Err:     fmt.Sprintf("unsupported language for CLI analysis: %s", language),
			Latency: time.Since(start),
		}
	}

	policy := sandbox.DefaultCodePolicy(targetPath)
	stdout, stderr, exitCode, err := e.sandbox.ExecCommand(ctx, cmd, args, nil, targetPath, policy)
	if err != nil {
		return types.CodeResult{
			OK:      false,
			Err:     fmt.Sprintf("run %s: %v\n%s", cmd, err, stderr),
			Latency: time.Since(start),
		}
	}

	// Many linters exit non-zero when findings exist — that's OK.
	_ = exitCode

	var findings []types.CodeFinding
	switch {
	case isPython(language):
		findings = parseRuffJSON(stdout)
	case isJavaScript(language):
		findings = parseESLintJSON(stdout)
	}

	return types.CodeResult{
		OK:       len(findings) == 0,
		Findings: findings,
		Stats:    types.CodeStats{FilesAnalyzed: 1},
		Latency:  time.Since(start),
	}
}

// cliSymbols returns a placeholder symbol result for non-Go languages.
// Full symbol extraction would require language servers; here we count files.
func (e *CodeExecutor) cliSymbols(ctx context.Context, targetPath, language string, start time.Time) types.ExecutorResult {
	// For non-Go, just report that the target was scanned.
	// A production implementation would use language servers or tree-sitter.
	return types.CodeResult{
		OK: true,
		Stats: types.CodeStats{
			FilesAnalyzed: 1,
		},
		Latency: time.Since(start),
	}
}

// --- JSON output parsers ---

// ruffResult maps the JSON structure from `ruff check --output-format json`.
type ruffResult struct {
	Filename string `json:"filename"`
	Line     int    `json:"line_no"`
	Rule     string `json:"code"`
	Message  string `json:"message"`
	Severity string `json:"severity"`
}

func parseRuffJSON(stdout string) []types.CodeFinding {
	var results []ruffResult
	if err := json.Unmarshal([]byte(stdout), &results); err != nil {
		return nil
	}
	findings := make([]types.CodeFinding, 0, len(results))
	for _, r := range results {
		severity := "warning"
		if r.Severity == "error" || r.Severity == "fatal" {
			severity = "error"
		}
		findings = append(findings, types.CodeFinding{
			File:     r.Filename,
			Line:     r.Line,
			Rule:     r.Rule,
			Message:  r.Message,
			Severity: severity,
		})
	}
	return findings
}

// eslintResult maps the JSON structure from `eslint --format json`.
type eslintResult struct {
	FilePath     string          `json:"filePath"`
	Messages     []eslintMessage `json:"messages"`
	ErrorCount   int             `json:"errorCount"`
	WarningCount int             `json:"warningCount"`
}

type eslintMessage struct {
	RuleID  string `json:"ruleId"`
	Message string `json:"message"`
	Line    int    `json:"line"`
	Sev     int    `json:"severity"` // 0=off, 1=warn, 2=error
}

func parseESLintJSON(stdout string) []types.CodeFinding {
	var results []eslintResult
	if err := json.Unmarshal([]byte(stdout), &results); err != nil {
		return nil
	}
	var findings []types.CodeFinding
	for _, file := range results {
		for _, msg := range file.Messages {
			severity := "warning"
			if msg.Sev >= 2 {
				severity = "error"
			}
			findings = append(findings, types.CodeFinding{
				File:     file.FilePath,
				Line:     msg.Line,
				Rule:     msg.RuleID,
				Message:  msg.Message,
				Severity: severity,
			})
		}
	}
	return findings
}
