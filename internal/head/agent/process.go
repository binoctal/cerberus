package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/sandbox"
	"github.com/binoctal/cerberus/internal/types"
)

// ProcessExecutor runs system commands via the sandbox layer.
type ProcessExecutor struct {
	sandbox sandbox.Sandbox
	logger  *zap.Logger
}

// NewProcessExecutor creates a process executor with sandbox isolation.
func NewProcessExecutor(sb sandbox.Sandbox, logger *zap.Logger) *ProcessExecutor {
	return &ProcessExecutor{sandbox: sb, logger: logger}
}

// Execute runs a process action.
func (e *ProcessExecutor) Execute(ctx context.Context, action types.TypedAction) types.ExecutorResult {
	start := time.Now()

	// Unwrap BuildAction to get the inner ProcessExecAction.
	var a types.ProcessExecAction
	switch v := action.(type) {
	case types.ProcessExecAction:
		a = v
	case types.BuildAction:
		a = v.Unwrap()
	default:
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

	// Build sandbox policy based on action type.
	sbPolicy := sandboxPolicyFor(action)

	// Convert env map to slice.
	var envSlice []string
	for k, v := range a.Env {
		envSlice = append(envSlice, k+"="+v)
	}

	// For build actions, run dependency installation first.
	if action.GetActionType() == types.ActionProcessBuild {
		if depCmd, depArgs := detectDepInstall(a.WorkDir); depCmd != "" {
			e.logger.Info("running dependency installation before build",
				zap.String("cmd", depCmd), zap.String("dir", a.WorkDir))
			depOut, depErr, depExit, depErr2 := e.sandbox.ExecCommand(ctx, depCmd, depArgs, envSlice, a.WorkDir, sbPolicy)
			if depErr2 != nil || depExit != 0 {
				return types.ProcessResult{
					OK: false, ExitCode: depExit,
					Stdout: depOut, Stderr: depErr,
					Latency: time.Since(start),
					Err:     fmt.Sprintf("dependency install failed (exit %d): %s", depExit, depErr),
				}
			}
		}
	}

	stdout, stderr, exitCode, err := e.sandbox.ExecCommand(ctx, a.Command, a.Args, envSlice, a.WorkDir, sbPolicy)
	if err != nil {
		return types.ProcessResult{
			OK: false, ExitCode: -1, Stderr: err.Error(),
			Latency: time.Since(start), Err: err.Error(),
		}
	}

	return types.ProcessResult{
		OK: exitCode == 0, ExitCode: exitCode,
		Stdout: stdout, Stderr: stderr,
		Latency: time.Since(start),
	}
}

// sandboxPolicyFor returns a sandbox policy appropriate for the action type.
func sandboxPolicyFor(action types.TypedAction) sandbox.Policy {
	switch action.GetActionType() {
	case types.ActionProcessBuild:
		// Build actions need broader filesystem access.
		return sandbox.Policy{
			FS:        sandbox.FSPolicy{ReadWrite: []string{"."}},
			Network:   sandbox.NetPolicy{AllowOutbound: true}, // may need to download deps
			Resources: sandbox.ResPolicy{Timeout: 120},
		}
	default:
		return sandbox.DefaultProcessPolicy(".")
	}
}

// detectDepInstall returns the dependency install command for the detected language.
// Returns ("", nil) if no dependency installation is needed.
func detectDepInstall(dir string) (string, []string) {
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
		return "go", []string{"mod", "download"}
	}
	if _, err := os.Stat(filepath.Join(dir, "package.json")); err == nil {
		return "npm", []string{"install"}
	}
	return "", nil
}
