package autotest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractFunc_ByLineLabel(t *testing.T) {
	src := []byte("package foo\n\nfunc Add(a, b int) int {\n\treturn a + b\n}\n\nfunc Sub(a, b int) int {\n\treturn a - b\n}\n")
	// Add body spans lines 3-5; Sub spans 7-9. L4 → Add.
	pkg, snippet := extractFunc(src, "foo.go:L4")
	assert.Equal(t, "foo", pkg)
	assert.Contains(t, snippet, "func Add")
	assert.NotContains(t, snippet, "func Sub", "L4 should match Add, not Sub")
}

func TestExtractFunc_ByName(t *testing.T) {
	src := []byte("package foo\nfunc Add(a, b int) int { return a + b }\nfunc Sub(a, b int) int { return a - b }\n")
	_, snippet := extractFunc(src, "Sub")
	assert.Contains(t, snippet, "func Sub")
	assert.NotContains(t, snippet, "func Add")
}

func TestParseLine(t *testing.T) {
	assert.Equal(t, 42, parseLine("foo.go:L42"))
	assert.Equal(t, 0, parseLine("Add"))
	assert.Equal(t, 0, parseLine("foo.go:Labc"))
}

func TestExportedFuncs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.go")
	require.NoError(t, os.WriteFile(path, []byte("package foo\nfunc Add(a, b int) int { return a + b }\nfunc private() {}\nfunc Sub(a, b int) int { return a - b }\n"), 0644))
	assert.Equal(t, []string{"Add", "Sub"}, exportedFuncs(path))
}
