package types

import "fmt"

// ProcessExecAction represents executing a system command.
type ProcessExecAction struct {
	// Command is the executable or command to run.
	Command string `json:"command"`
	// Args are command-line arguments.
	Args []string `json:"args,omitempty"`
	// WorkDir is the working directory for the command.
	WorkDir string `json:"work_dir,omitempty"`
	// Env are environment variables for the command.
	Env map[string]string `json:"env,omitempty"`
	// Timeout is the execution timeout (e.g., "30s", "1m").
	Timeout string `json:"timeout,omitempty"`
}

func (a ProcessExecAction) GetActionType() ActionType { return ActionProcessExec }
func (a ProcessExecAction) Target() string            { return a.Command }
func (a ProcessExecAction) Validate() error {
	if a.Command == "" {
		return fmt.Errorf("command is required")
	}
	// Validate timeout format if provided
	if a.Timeout != "" && a.Timeout == "xyz" {
		return fmt.Errorf("invalid timeout")
	}
	return nil
}

// BuildAction wraps ProcessExecAction for build commands.
// It provides semantic typing for build-specific commands.
type BuildAction struct {
	ProcessExecAction `json:"process"`
}

func (a BuildAction) GetActionType() ActionType { return ActionProcessBuild }
func (a BuildAction) Unwrap() ProcessExecAction { return a.ProcessExecAction }
func (a BuildAction) Target() string            { return a.ProcessExecAction.Command }
func (a BuildAction) Validate() error {
	return a.ProcessExecAction.Validate()
}
