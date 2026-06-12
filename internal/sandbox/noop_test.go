package sandbox

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNoOpSandbox_ExecCommand_Success(t *testing.T) {
	sb := NoOpSandbox{}
	stdout, stderr, exitCode, err := sb.ExecCommand(
		context.Background(), "echo", []string{"hello"}, nil, ".", Policy{},
	)
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "hello")
	assert.Empty(t, stderr)
}

func TestNoOpSandbox_ExecCommand_Failure(t *testing.T) {
	sb := NoOpSandbox{}
	_, _, exitCode, err := sb.ExecCommand(
		context.Background(), "false", nil, nil, ".", Policy{},
	)
	require.NoError(t, err)
	assert.NotEqual(t, 0, exitCode)
}

func TestNoOpSandbox_ExecCommand_Timeout(t *testing.T) {
	sb := NoOpSandbox{}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, _, exitCode, _ := sb.ExecCommand(ctx, "sleep", []string{"10"}, nil, ".", Policy{})
	// exec.CommandContext kills the process on context cancel, returns non-zero exit.
	assert.NotEqual(t, 0, exitCode)
}

func TestNoOpSandbox_ExecCommand_WithEnv(t *testing.T) {
	sb := NoOpSandbox{}
	stdout, _, exitCode, err := sb.ExecCommand(
		context.Background(), "sh", []string{"-c", "echo $MY_VAR"}, []string{"MY_VAR=testval"}, ".", Policy{},
	)
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "testval")
}

func TestNoOpSandbox_ExecCommand_NotFound(t *testing.T) {
	sb := NoOpSandbox{}
	_, _, _, err := sb.ExecCommand(
		context.Background(), "nonexistent_command_xyz", nil, nil, ".", Policy{},
	)
	require.Error(t, err)
}
