package autotest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSourcePath_StripsModulePrefix(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module github.com/x/y\n\ngo 1.25\n"), 0644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal/pkg"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "internal/pkg/f.go"), []byte("package pkg"), 0644))

	p := sourcePath("github.com/x/y/internal/pkg/f.go", dir)
	_, err := os.ReadFile(p)
	assert.NoError(t, err, "module-qualified path should resolve to a real file")
}

func TestSourcePath_RelativePathUnchanged(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module github.com/x/y\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "f.go"), []byte("x"), 0644))

	p := sourcePath("f.go", dir)
	_, err := os.ReadFile(p)
	assert.NoError(t, err, "already-relative path should resolve under projectDir")
}

func TestModuleName(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module github.com/binoctal/cerberus\n\ngo 1.25\n"), 0644))
	assert.Equal(t, "github.com/binoctal/cerberus", moduleName(dir))
}
