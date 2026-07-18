package autotest

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// DefaultGoCoverageRunner shells out to `go test -coverprofile` in a real Go
// module and returns the profile bytes. Build a minimal stdlib-only module so
// the test exercises the runner end-to-end without external deps.
func TestDefaultGoCoverageRunner(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"),
		[]byte("module coverfix\n\ngo 1.25\n"), 0o644))
	pkgDir := filepath.Join(dir, "pkg")
	require.NoError(t, os.MkdirAll(pkgDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pkgDir, "a.go"),
		[]byte("package pkg\n\nfunc A() int { return 1 }\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(pkgDir, "a_test.go"),
		[]byte("package pkg\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) { if A() != 1 { t.Fatal(\"x\") } }\n"), 0o644))

	data, err := DefaultGoCoverageRunner(context.Background(), dir)
	require.NoError(t, err)
	assert.NotEmpty(t, data)
	assert.Contains(t, string(data), "mode:")
	assert.Contains(t, string(data), "a.go")
}
