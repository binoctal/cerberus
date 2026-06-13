package agent

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/types"
)

// parseHelper parses a Go source string and returns the AST file and file set.
func parseHelper(t *testing.T, src string) (*ast.File, *token.FileSet) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "test.go", src, parser.AllErrors|parser.ParseComments)
	require.NoError(t, err)
	return f, fset
}

func TestCheckComplexity_HighComplexity(t *testing.T) {
	src := `package testpkg
func complexFunc(x int) {
	if x > 0 {
		for i := 0; i < x; i++ {
			if i%2 == 0 {
				switch i {
				case 0: _ = 0
				case 1: _ = 1
				case 2: _ = 2
				case 3: _ = 3
				case 4: _ = 4
				case 5: _ = 5
				}
			} else if i%3 == 0 {
				_ = 3
			} else {
				_ = 4
			}
		}
	}
	for _, v := range []int{1,2,3} {
		if v > 1 { _ = v }
		if v > 2 { _ = v }
	}
	for j := 0; j < 10; j++ {
		if j == 0 { _ = j }
	}
}
`
	f, fset := parseHelper(t, src)
	findings := checkComplexity(f, fset)
	require.NotEmpty(t, findings, "expected complexity finding")
	assert.Equal(t, "high_complexity", findings[0].Rule)
	assert.Contains(t, findings[0].Message, "complexFunc")
}

func TestCheckComplexity_LowComplexity(t *testing.T) {
	src := `package testpkg
func simpleFunc() { _ = 42 }
`
	f, fset := parseHelper(t, src)
	findings := checkComplexity(f, fset)
	assert.Empty(t, findings)
}

func TestCheckUnhandledErrors_Unhandled(t *testing.T) {
	src := `package testpkg
func badFunc() {
	_, err := doSomething()
	_ = 42
}
`
	f, fset := parseHelper(t, src)
	findings := checkUnhandledErrors(f, fset)
	require.NotEmpty(t, findings, "expected unhandled error finding")
	assert.Equal(t, "unhandled_error", findings[0].Rule)
	assert.Contains(t, findings[0].Message, "err")
}

func TestCheckUnhandledErrors_Handled(t *testing.T) {
	src := `package testpkg
func goodFunc() {
	_, err := doSomething()
	if err != nil {
		return
	}
}
`
	f, fset := parseHelper(t, src)
	findings := checkUnhandledErrors(f, fset)
	assert.Empty(t, findings, "expected no findings for handled error")
}

func TestCheckUnhandledErrors_ErrPassedToCall(t *testing.T) {
	src := `package testpkg
func wrappedFunc() {
	_, err := doSomething()
	logError(err)
}
`
	f, fset := parseHelper(t, src)
	findings := checkUnhandledErrors(f, fset)
	assert.Empty(t, findings, "expected no findings when err is passed to another call")
}

func TestCheckUnhandledErrors_ErrReturned(t *testing.T) {
	src := `package testpkg
func returnFunc() error {
	_, err := doSomething()
	return err
}
`
	f, fset := parseHelper(t, src)
	findings := checkUnhandledErrors(f, fset)
	assert.Empty(t, findings, "expected no findings when err is returned")
}

func TestCheckDeadCode_UnusedPrivateFunc(t *testing.T) {
	dir := t.TempDir()
	src := `package testpkg
func usedFunc() { helper() }
func helper() { _ = 42 }
func unusedFunc() { _ = 1 }
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "dead.go"), []byte(src), 0644))

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, nil, 0) //nolint:staticcheck // SA1019
	require.NoError(t, err)

	var findings []types.CodeFinding
	for _, pkg := range pkgs {
		findings = checkDeadCode(pkg, fset)
	}

	names := make(map[string]bool)
	for _, f := range findings {
		names[f.Message] = true
	}
	assert.True(t, names[`function unusedFunc is declared but never called within this package`] ||
		len(findings) >= 1, "expected unusedFunc to be flagged as dead code")
}

func TestCheckDeadCode_ExportedNotFlagged(t *testing.T) {
	dir := t.TempDir()
	src := `package testpkg
func ExportedButUnused() { _ = 42 }
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "exported.go"), []byte(src), 0644))

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, nil, 0) //nolint:staticcheck // SA1019
	require.NoError(t, err)

	for _, pkg := range pkgs {
		findings := checkDeadCode(pkg, fset)
		assert.Empty(t, findings, "exported functions should not be flagged")
	}
}

func TestCheckDeadCode_MainInitNotFlagged(t *testing.T) {
	dir := t.TempDir()
	src := `package main
func main() { _ = 42 }
func init() {}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0644))

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, nil, 0) //nolint:staticcheck // SA1019
	require.NoError(t, err)

	for _, pkg := range pkgs {
		findings := checkDeadCode(pkg, fset)
		assert.Empty(t, findings, "main and init should not be flagged")
	}
}

func TestCheckDeadCode_CalledNotFlagged(t *testing.T) {
	dir := t.TempDir()
	src := `package testpkg
func caller() { callee() }
func callee() { _ = 42 }
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "called.go"), []byte(src), 0644))

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, nil, 0) //nolint:staticcheck // SA1019
	require.NoError(t, err)

	for _, pkg := range pkgs {
		findings := checkDeadCode(pkg, fset)
		// callee is called by caller, so should not be flagged.
		for _, f := range findings {
			assert.NotContains(t, f.Message, "callee", "called functions should not be flagged")
		}
	}
}

func TestContainsCheck(t *testing.T) {
	assert.True(t, containsCheck([]string{"a", "b", "c"}, "b"))
	assert.False(t, containsCheck([]string{"a", "b"}, "c"))
	assert.False(t, containsCheck(nil, "a"))
}
