package agent

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/binoctal/cerberus/internal/sandbox"
	"github.com/binoctal/cerberus/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func codeExec(t *testing.T) *CodeExecutor {
	t.Helper()
	return NewCodeExecutor(sandbox.NoOpSandbox{}, zap.NewNop())
}


// projectRoot returns the cerberus project root directory.
func projectRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot determine test file path")
	}
	// This file is at internal/head/agent/code_execute_test.go
	return filepath.Join(filepath.Dir(filename), "..", "..", "..")
}

func TestCodeExecutor_Execute_GoAnalyze(t *testing.T) {
	exec := codeExec(t)
	result := exec.Execute(context.Background(), types.CodeAnalyzeAction{
		TargetPath: filepath.Join(projectRoot(t), "internal", "ai"),
		Language:   "Go",
		Checks:     []string{"complexity", "unhandled_error"},
	})

	cr, ok := result.(types.CodeResult)
	require.True(t, ok, "expected CodeResult, got %T", result)
	assert.True(t, cr.Stats.FilesAnalyzed > 0, "should analyze at least 1 file")
	assert.Empty(t, cr.Err, "should not have error: %s", cr.Err)
}

func TestCodeExecutor_Execute_GoLint(t *testing.T) {
	// Lint the project's own executor.go — it is complex enough to trigger findings.
	exec := codeExec(t)
	result := exec.Execute(context.Background(), types.CodeLintAction{
		TargetPath: filepath.Join(projectRoot(t), "internal", "head", "agent"),
		Language:   "Go",
		// No Rules — should default to ["unhandled_error", "complexity"].
	})

	cr, ok := result.(types.CodeResult)
	require.True(t, ok)
	assert.Empty(t, cr.Err, "should not error: %s", cr.Err)
	assert.Greater(t, cr.Stats.FilesAnalyzed, 0, "should analyze files")
}

func TestCodeExecutor_Execute_GoSymbols(t *testing.T) {
	exec := codeExec(t)
	result := exec.Execute(context.Background(), types.CodeSymbolsAction{
		TargetPath: filepath.Join(projectRoot(t), "internal", "types"),
		Language:   "Go",
	})

	cr, ok := result.(types.CodeResult)
	require.True(t, ok)
	assert.True(t, cr.OK)
	assert.Greater(t, cr.Stats.SymbolCount, 0, "should find symbols in types package")
}

func TestCodeExecutor_Execute_UnsupportedAction(t *testing.T) {
	exec := codeExec(t)
	result := exec.Execute(context.Background(), types.HTTPAction{Method: "GET", URL: "http://test"})

	errResult, ok := result.(types.ErrorResult)
	require.True(t, ok)
	assert.Contains(t, errResult.Err, "unsupported action")
}

func TestCodeExecutor_Execute_GoAnalyze_InvalidPath(t *testing.T) {
	exec := codeExec(t)
	result := exec.Execute(context.Background(), types.CodeAnalyzeAction{
		TargetPath: "/nonexistent/path/that/does/not/exist",
		Language:   "Go",
	})

	cr, ok := result.(types.CodeResult)
	require.True(t, ok)
	assert.False(t, cr.OK)
	assert.NotEmpty(t, cr.Err)
}

func TestCodeExecutor_Execute_NonGoSymbols(t *testing.T) {
	exec := codeExec(t)
	result := exec.Execute(context.Background(), types.CodeSymbolsAction{
		TargetPath: ".",
		Language:   "Python",
	})

	cr, ok := result.(types.CodeResult)
	require.True(t, ok)
	assert.True(t, cr.OK, "non-Go symbols should return placeholder OK")
	assert.Equal(t, 1, cr.Stats.FilesAnalyzed)
}

func TestCodeExecutor_Execute_UnknownLanguageLint(t *testing.T) {
	exec := codeExec(t)
	result := exec.Execute(context.Background(), types.CodeLintAction{
		TargetPath: ".",
		Language:   "Rust",
	})

	cr, ok := result.(types.CodeResult)
	require.True(t, ok)
	assert.False(t, cr.OK)
	assert.Contains(t, cr.Err, "unsupported language")
}

func TestParseRuffJSON(t *testing.T) {
	input := `[{"filename":"main.py","line_no":10,"code":"E501","message":"line too long","severity":"warning"}]`
	findings := parseRuffJSON(input)
	require.Len(t, findings, 1)
	assert.Equal(t, "main.py", findings[0].File)
	assert.Equal(t, 10, findings[0].Line)
	assert.Equal(t, "E501", findings[0].Rule)
	assert.Equal(t, "warning", findings[0].Severity)
}

func TestParseRuffJSON_ErrorSeverity(t *testing.T) {
	input := `[{"filename":"a.py","line_no":1,"code":"F401","message":"unused import","severity":"fatal"}]`
	findings := parseRuffJSON(input)
	require.Len(t, findings, 1)
	assert.Equal(t, "error", findings[0].Severity)
}

func TestParseRuffJSON_InvalidJSON(t *testing.T) {
	assert.Nil(t, parseRuffJSON("not json"))
}

func TestParseRuffJSON_Empty(t *testing.T) {
	assert.Empty(t, parseRuffJSON("[]"))
}

func TestParseESLintJSON(t *testing.T) {
	input := `[{"filePath":"app.js","messages":[{"ruleId":"no-unused-vars","message":"'x' is defined but never used","line":5,"severity":1}],"errorCount":0,"warningCount":1}]`
	findings := parseESLintJSON(input)
	require.Len(t, findings, 1)
	assert.Equal(t, "app.js", findings[0].File)
	assert.Equal(t, 5, findings[0].Line)
	assert.Equal(t, "no-unused-vars", findings[0].Rule)
	assert.Equal(t, "warning", findings[0].Severity)
}

func TestParseESLintJSON_ErrorSeverity(t *testing.T) {
	input := `[{"filePath":"b.js","messages":[{"ruleId":"no-undef","message":"undef","line":1,"severity":2}],"errorCount":1,"warningCount":0}]`
	findings := parseESLintJSON(input)
	require.Len(t, findings, 1)
	assert.Equal(t, "error", findings[0].Severity)
}

func TestParseESLintJSON_InvalidJSON(t *testing.T) {
	assert.Nil(t, parseESLintJSON("bad"))
}

func TestParseESLintJSON_Empty(t *testing.T) {
	assert.Nil(t, parseESLintJSON("[]"))
}

func TestIsGoLang(t *testing.T) {
	tests := []struct{ lang string; want bool }{
		{"", true},
		{"Go", true},
		{"go", false},
		{"Python", false},
		{"JavaScript", false},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, isGoLang(tt.lang), "isGoLang(%q)", tt.lang)
	}
}

func TestIsPython(t *testing.T) {
	tests := []struct{ lang string; want bool }{
		{"Python", true},
		{"python", true},
		{"PYTHON", true},
		{"Go", false},
		{"", false},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, isPython(tt.lang), "isPython(%q)", tt.lang)
	}
}

func TestIsJavaScript(t *testing.T) {
	tests := []struct{ lang string; want bool }{
		{"JavaScript", true},
		{"javascript", true},
		{"TypeScript", true},
		{"typescript", true},
		{"JavaScript/TypeScript", true},
		{"javascript/typescript", true},
		{"Go", false},
		{"", false},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, isJavaScript(tt.lang), "isJavaScript(%q)", tt.lang)
	}
}
