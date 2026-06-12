package scout

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDetectProjectType_Go(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/test\ngo 1.25\n"), 0o644)

	info := DetectProjectType(dir)
	assert.Equal(t, ProjectGo, info.Type)
	assert.Equal(t, "go build ./...", info.BuildCmd)
	assert.Equal(t, "go test ./...", info.TestCmd)
	assert.Equal(t, "go vet ./...", info.LintCmd)
	assert.Equal(t, "Go", info.Language)
	assert.Equal(t, dir, info.RootDir)
}

func TestDetectProjectType_Node(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"test"}`), 0o644)

	info := DetectProjectType(dir)
	assert.Equal(t, ProjectNode, info.Type)
	assert.Equal(t, "npm install", info.BuildCmd)
	assert.Equal(t, "npm test", info.TestCmd)
	assert.Equal(t, "JavaScript/TypeScript", info.Language)
}

func TestDetectProjectType_Python(t *testing.T) {
	for _, marker := range []string{"pyproject.toml", "setup.py", "requirements.txt"} {
		t.Run(marker, func(t *testing.T) {
			dir := t.TempDir()
			_ = os.WriteFile(filepath.Join(dir, marker), []byte(""), 0o644)

			info := DetectProjectType(dir)
			assert.Equal(t, ProjectPython, info.Type)
			assert.Equal(t, "pytest", info.TestCmd)
			assert.Equal(t, "ruff check", info.LintCmd)
		})
	}
}

func TestDetectProjectType_Unknown(t *testing.T) {
	dir := t.TempDir()

	info := DetectProjectType(dir)
	assert.Equal(t, ProjectUnknown, info.Type)
}

func TestDetectProjectType_HTTP(t *testing.T) {
	info := DetectProjectType("")
	assert.Equal(t, ProjectHTTP, info.Type)
}

func TestDetectProjectType_GoTakesPrecedence(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{}`), 0o644)

	info := DetectProjectType(dir)
	assert.Equal(t, ProjectGo, info.Type)
}
