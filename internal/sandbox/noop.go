package sandbox

import (
	"bytes"
	"context"
	"os"
	"os/exec"
)

// NoOpSandbox is a no-op sandbox that imposes no restrictions.
type NoOpSandbox struct{}

// Apply returns the context unchanged with a no-op cleanup.
func (NoOpSandbox) Apply(ctx context.Context, _ Policy) (context.Context, func(), error) {
	return ctx, func() {}, nil
}

// ExecCommand runs a command without sandbox isolation (direct os/exec).
func (NoOpSandbox) ExecCommand(ctx context.Context, cmd string, args []string, env []string, dir string, _ Policy) (string, string, int, error) {
	c := exec.CommandContext(ctx, cmd, args...)
	if dir != "" {
		c.Dir = dir
	}
	if len(env) > 0 {
		c.Env = os.Environ()
		for _, e := range env {
			c.Env = append(c.Env, e)
		}
	}
	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr

	err := c.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return "", "", -1, err
		}
	}
	return stdout.String(), stderr.String(), exitCode, nil
}
