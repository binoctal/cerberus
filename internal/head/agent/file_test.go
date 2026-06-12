package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/binoctal/cerberus/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestFileExecutor_ReadWriteRoundtrip(t *testing.T) {
	dir := t.TempDir()
	fe := NewFileExecutor(dir, zap.NewNop())

	// Write a file.
	writeResult := fe.Execute(context.Background(), types.FileWriteAction{
		Path:    "test.txt",
		Content: "hello world",
	})
	fr, ok := writeResult.(types.FileResult)
	require.True(t, ok)
	assert.True(t, fr.OK)
	assert.Equal(t, filepath.Join(dir, "test.txt"), fr.Path)

	// Read it back.
	readResult := fe.Execute(context.Background(), types.FileReadAction{
		Path: "test.txt",
	})
	fr2, ok := readResult.(types.FileResult)
	require.True(t, ok)
	assert.True(t, fr2.OK)
	assert.Equal(t, "hello world", fr2.Content)
}

func TestFileExecutor_ReadNonexistent(t *testing.T) {
	dir := t.TempDir()
	fe := NewFileExecutor(dir, zap.NewNop())

	result := fe.Execute(context.Background(), types.FileReadAction{
		Path: "nope.txt",
	})
	fr, ok := result.(types.FileResult)
	require.True(t, ok)
	assert.False(t, fr.OK)
	assert.Contains(t, fr.Err, "no such file")
}

func TestFileExecutor_Exists(t *testing.T) {
	dir := t.TempDir()
	fe := NewFileExecutor(dir, zap.NewNop())

	// File does not exist.
	result := fe.Execute(context.Background(), types.FileExistsAction{
		Path: "missing.txt",
	})
	fr, ok := result.(types.FileResult)
	require.True(t, ok)
	assert.True(t, fr.OK)
	assert.False(t, fr.Exists)

	// Create file, then check.
	os.WriteFile(filepath.Join(dir, "present.txt"), []byte("x"), 0644)
	result2 := fe.Execute(context.Background(), types.FileExistsAction{
		Path: "present.txt",
	})
	fr2, ok := result2.(types.FileResult)
	require.True(t, ok)
	assert.True(t, fr2.OK)
	assert.True(t, fr2.Exists)
}

func TestFileExecutor_Glob(t *testing.T) {
	dir := t.TempDir()
	fe := NewFileExecutor(dir, zap.NewNop())

	os.WriteFile(filepath.Join(dir, "a.go"), []byte("p1"), 0644)
	os.WriteFile(filepath.Join(dir, "b.go"), []byte("p2"), 0644)
	os.WriteFile(filepath.Join(dir, "c.txt"), []byte("p3"), 0644)

	result := fe.Execute(context.Background(), types.FileGlobAction{
		Pattern: "*.go",
	})
	fr, ok := result.(types.FileResult)
	require.True(t, ok)
	assert.True(t, fr.OK)
	assert.Len(t, fr.Matches, 2)
}

func TestFileExecutor_PathTraversal(t *testing.T) {
	dir := t.TempDir()
	fe := NewFileExecutor(dir, zap.NewNop())

	result := fe.Execute(context.Background(), types.FileReadAction{
		Path: "../../etc/passwd",
	})
	fr, ok := result.(types.FileResult)
	require.True(t, ok)
	assert.False(t, fr.OK)
	assert.Contains(t, fr.Err, "escapes project root")
}

func TestFileExecutor_UnsupportedAction(t *testing.T) {
	dir := t.TempDir()
	fe := NewFileExecutor(dir, zap.NewNop())

	result := fe.Execute(context.Background(), types.HTTPAction{Method: "GET", URL: "http://x"})
	errResult, ok := result.(types.ErrorResult)
	require.True(t, ok)
	assert.Contains(t, errResult.Err, "unsupported action")
}

func TestFileExecutor_WriteToSubdirectory(t *testing.T) {
	dir := t.TempDir()
	fe := NewFileExecutor(dir, zap.NewNop())

	sub := filepath.Join(dir, "sub")
	require.NoError(t, os.Mkdir(sub, 0755))

	result := fe.Execute(context.Background(), types.FileWriteAction{
		Path:    "sub/nested.txt",
		Content: "nested content",
	})
	fr, ok := result.(types.FileResult)
	require.True(t, ok)
	assert.True(t, fr.OK)

	data, err := os.ReadFile(filepath.Join(dir, "sub", "nested.txt"))
	require.NoError(t, err)
	assert.Equal(t, "nested content", string(data))
}

func TestFileExecutor_SafePath(t *testing.T) {
	dir := t.TempDir()
	fe := NewFileExecutor(dir, zap.NewNop())

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{"simple file", "foo.txt", false},
		{"subdirectory", "sub/bar.txt", false},
		{"dot prefix", "./foo.txt", false},
		{"parent escape", "../escape.txt", true},
		{"deep escape", "a/b/../../../escape.txt", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := fe.safePath(tt.path)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
