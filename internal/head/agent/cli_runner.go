package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/binoctal/cerberus/internal/sandbox"
	"github.com/binoctal/cerberus/internal/types"
)

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
