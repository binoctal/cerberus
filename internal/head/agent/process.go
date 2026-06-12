package agent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/binoctal/cerberus/internal/types"
	"go.uber.org/zap"
)

// ProcessExecutor runs system commands.
type ProcessExecutor struct {
	logger *zap.Logger
}

// NewProcessExecutor creates a process executor.
func NewProcessExecutor(logger *zap.Logger) *ProcessExecutor {
	return &ProcessExecutor{logger: logger}
}

// Execute runs a process action.
func (e *ProcessExecutor) Execute(ctx context.Context, action types.TypedAction) types.ExecutorResult {
	start := time.Now()
	a, ok := action.(types.ProcessExecAction)
	if !ok {
		return types.ErrorResult{Err: fmt.Sprintf("process executor: unsupported action %T", action)}
	}

	timeout := 60 * time.Second
	if a.Timeout != "" {
		if d, err := time.ParseDuration(a.Timeout); err == nil {
			timeout = d
		}
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, a.Command, a.Args...)
	if a.WorkDir != "" {
		cmd.Dir = a.WorkDir
	}
	if len(a.Env) > 0 {
		cmd.Env = os.Environ()
		for k, v := range a.Env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			return types.ProcessResult{
				OK: false, ExitCode: -1, Stderr: err.Error(),
				Latency: time.Since(start), Err: err.Error(),
			}
		}
	}

	return types.ProcessResult{
		OK: exitCode == 0, ExitCode: exitCode,
		Stdout: stdout.String(), Stderr: stderr.String(),
		Latency: time.Since(start),
	}
}
