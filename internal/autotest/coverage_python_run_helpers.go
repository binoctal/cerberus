package autotest

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"go.uber.org/zap"
)

// pythonCmdContext holds context for command execution
type pythonCmdContext struct {
	pythonCmd  string
	projectDir string
	args       []string
	cmd        *exec.Cmd
	ctx        context.Context
	config     *CoverageConfig
	logger     *zap.Logger
}

// determinePythonCommand finds the Python command to use from config or default
func determinePythonCommand(config *CoverageConfig) string {
	pythonCmd := "python3"
	if len(config.Env) > 0 {
		for _, env := range config.Env {
			if strings.HasPrefix(env, "PYTHON_CMD=") {
				pythonCmd = strings.TrimPrefix(env, "PYTHON_CMD=")
				break
			}
		}
	}
	return pythonCmd
}

// buildPythonTestCommand creates the command context for running tests
func buildPythonTestCommand(ctx context.Context, pythonCmd string, config *CoverageConfig, projectDir string, logger *zap.Logger) *pythonCmdContext {
	args := append([]string{pythonCmd}, config.TestCommand...)
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = projectDir
	if config.Env != nil {
		cmd.Env = append(os.Environ(), config.Env...)
	}

	logger.Info("running python coverage",
		zap.String("cmd", strings.Join(args, " ")),
		zap.String("dir", projectDir))

	return &pythonCmdContext{
		pythonCmd:  pythonCmd,
		projectDir: projectDir,
		args:       args,
		cmd:        cmd,
		ctx:        ctx,
		config:     config,
		logger:     logger,
	}
}

// applyTimeout applies timeout to the command context if configured
func (pc *pythonCmdContext) applyTimeout() context.CancelFunc {
	if pc.config.Timeout > 0 {
		var cancel context.CancelFunc
		pc.ctx, cancel = context.WithTimeout(pc.ctx, pc.config.Timeout)
		pc.cmd = exec.CommandContext(pc.ctx, pc.args[0], pc.args[1:]...)
		pc.cmd.Dir = pc.projectDir
		if pc.config.Env != nil {
			pc.cmd.Env = append(os.Environ(), pc.config.Env...)
		}
		return cancel
	}
	return nil
}

// executeTestCommand runs the test command and returns output
func (pc *pythonCmdContext) executeTestCommand() ([]byte, error) {
	output, err := pc.cmd.CombinedOutput()
	if err != nil {
		// Check if it was a timeout
		if pc.ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("python coverage: timed out after %v", pc.config.Timeout)
		}
		// Tests may have failed but coverage report might still exist
		pc.logger.Warn("python coverage test had errors", zap.Error(err), zap.String("output", string(output)))
	}
	return output, nil
}

// generateCoverageReport runs the coverage report generation command
func (pc *pythonCmdContext) generateCoverageReport() error {
	if len(pc.config.CoverageArgs) == 0 {
		return nil
	}

	reportArgs := append([]string{pc.pythonCmd}, pc.config.CoverageArgs...)
	reportCmd := exec.Command(reportArgs[0], reportArgs[1:]...)
	reportCmd.Dir = pc.projectDir
	if runErr := reportCmd.Run(); runErr != nil {
		pc.logger.Warn("python coverage report generation failed", zap.Error(runErr))
	}
	return nil
}
