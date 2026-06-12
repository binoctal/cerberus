package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/binoctal/cerberus/internal/types"
	"go.uber.org/zap"
)

// FileExecutor handles file read/write/exists/glob operations.
type FileExecutor struct {
	projectRoot string
	logger      *zap.Logger
}

// NewFileExecutor creates a file executor scoped to projectRoot.
func NewFileExecutor(projectRoot string, logger *zap.Logger) *FileExecutor {
	abs, _ := filepath.Abs(projectRoot)
	return &FileExecutor{projectRoot: abs, logger: logger}
}

// safePath resolves and validates that path stays within projectRoot.
func (e *FileExecutor) safePath(p string) (string, error) {
	abs, err := filepath.Abs(filepath.Join(e.projectRoot, p))
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	rel, err := filepath.Rel(e.projectRoot, abs)
	if err != nil {
		return "", fmt.Errorf("relative path: %w", err)
	}
	if strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("path escapes project root: %s", p)
	}
	return abs, nil
}

// Execute dispatches file actions.
func (e *FileExecutor) Execute(ctx context.Context, action types.TypedAction) types.ExecutorResult {
	start := time.Now()
	switch a := action.(type) {
	case types.FileReadAction:
		return e.readFile(a, start)
	case types.FileWriteAction:
		return e.writeFile(a, start)
	case types.FileExistsAction:
		return e.existsFile(a, start)
	case types.FileGlobAction:
		return e.globFiles(a, start)
	default:
		return types.ErrorResult{Err: fmt.Sprintf("file executor: unsupported action %T", action)}
	}
}

func (e *FileExecutor) readFile(a types.FileReadAction, start time.Time) types.ExecutorResult {
	path, err := e.safePath(a.Path)
	if err != nil {
		return types.FileResult{OK: false, Path: a.Path, Err: err.Error(), Latency: time.Since(start)}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return types.FileResult{OK: false, Path: a.Path, Err: err.Error(), Latency: time.Since(start)}
	}
	return types.FileResult{OK: true, Path: path, Content: string(data), Latency: time.Since(start)}
}

func (e *FileExecutor) writeFile(a types.FileWriteAction, start time.Time) types.ExecutorResult {
	path, err := e.safePath(a.Path)
	if err != nil {
		return types.FileResult{OK: false, Path: a.Path, Err: err.Error(), Latency: time.Since(start)}
	}
	if err := os.WriteFile(path, []byte(a.Content), 0644); err != nil {
		return types.FileResult{OK: false, Path: a.Path, Err: err.Error(), Latency: time.Since(start)}
	}
	return types.FileResult{OK: true, Path: path, Latency: time.Since(start)}
}

func (e *FileExecutor) existsFile(a types.FileExistsAction, start time.Time) types.ExecutorResult {
	path, err := e.safePath(a.Path)
	if err != nil {
		return types.FileResult{OK: false, Path: a.Path, Err: err.Error(), Latency: time.Since(start)}
	}
	_, statErr := os.Stat(path)
	return types.FileResult{OK: true, Path: path, Exists: statErr == nil, Latency: time.Since(start)}
}

func (e *FileExecutor) globFiles(a types.FileGlobAction, start time.Time) types.ExecutorResult {
	pattern := filepath.Join(e.projectRoot, a.Pattern)
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return types.FileResult{OK: false, Path: a.Pattern, Err: err.Error(), Latency: time.Since(start)}
	}
	return types.FileResult{OK: true, Path: a.Pattern, Matches: matches, Latency: time.Since(start)}
}
