package types

import (
	"encoding/json"
	"fmt"
)

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

// UnmarshalJSON tolerates Timeout as either a string ("30s") or a number
// (seconds), matching WaitAction.Duration. Non-Claude models often emit a bare
// numeric timeout. Also covers BuildAction, which embeds ProcessExecAction.
// Other fields decode through an alias to avoid recursion.
func (a *ProcessExecAction) UnmarshalJSON(data []byte) error {
	type alias ProcessExecAction
	var tmp struct {
		alias
		Timeout json.RawMessage `json:"timeout,omitempty"`
	}
	if err := json.Unmarshal(data, &tmp); err != nil {
		return err
	}
	*a = ProcessExecAction(tmp.alias)
	t, err := coerceDurationRaw(tmp.Timeout)
	if err != nil {
		return fmt.Errorf("process timeout: %w", err)
	}
	a.Timeout = t
	return nil
}

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
func (a BuildAction) Target() string            { return a.Command }
func (a BuildAction) Validate() error {
	return a.ProcessExecAction.Validate()
}
