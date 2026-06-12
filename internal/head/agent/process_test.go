package agent

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/binoctal/cerberus/internal/sandbox"
	"github.com/binoctal/cerberus/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestProcessExecutor_Success(t *testing.T) {
	sb := sandbox.NoOpSandbox{}
	executor := NewProcessExecutor(sb, zap.NewNop())

	result := executor.Execute(context.Background(), types.ProcessExecAction{
		Command: "echo",
		Args:    []string{"hello from test"},
		WorkDir: ".",
	})

	pr, ok := result.(types.ProcessResult)
	require.True(t, ok)
	assert.True(t, pr.OK)
	assert.Equal(t, 0, pr.ExitCode)
	assert.Contains(t, pr.Stdout, "hello from test")
}

func TestProcessExecutor_Failure(t *testing.T) {
	sb := sandbox.NoOpSandbox{}
	executor := NewProcessExecutor(sb, zap.NewNop())

	result := executor.Execute(context.Background(), types.ProcessExecAction{
		Command: "false",
		WorkDir: ".",
	})

	pr, ok := result.(types.ProcessResult)
	require.True(t, ok)
	assert.False(t, pr.OK)
	assert.NotEqual(t, 0, pr.ExitCode)
}

func TestProcessExecutor_UnsupportedAction(t *testing.T) {
	sb := sandbox.NoOpSandbox{}
	executor := NewProcessExecutor(sb, zap.NewNop())

	result := executor.Execute(context.Background(), types.HTTPAction{Method: "GET", URL: "http://test"})

	errResult, ok := result.(types.ErrorResult)
	require.True(t, ok)
	assert.Contains(t, errResult.Err, "unsupported action")
}

func TestProcessExecutor_BuildAction(t *testing.T) {
	sb := sandbox.NoOpSandbox{}
	executor := NewProcessExecutor(sb, zap.NewNop())

	// BuildAction should trigger dependency install + build command.
	result := executor.Execute(context.Background(), types.BuildAction{
		ProcessExecAction: types.ProcessExecAction{
			Command: "go",
			Args:    []string{"version"},
			WorkDir: ".",
		},
	})

	pr, ok := result.(types.ProcessResult)
	require.True(t, ok)
	assert.True(t, pr.OK)
	assert.Contains(t, pr.Stdout, "go")
}

func TestDetectDepInstall(t *testing.T) {
	t.Run("go project", func(t *testing.T) {
		root := filepath.Join("..", "..", "..")
		cmd, args := detectDepInstall(root)
		assert.Equal(t, "go", cmd)
		assert.Equal(t, []string{"mod", "download"}, args)
	})

	t.Run("unknown dir", func(t *testing.T) {
		cmd, args := detectDepInstall("/nonexistent")
		assert.Empty(t, cmd)
		assert.Nil(t, args)
	})
}
