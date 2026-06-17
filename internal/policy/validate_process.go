package policy

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/binoctal/cerberus/internal/types"
)

// validateProcessExecAction validates process execution actions
func (p *DefaultActionPolicy) validateProcessExecAction(a types.ProcessExecAction) error {
	if !p.allowedCmds[a.Command] {
		return fmt.Errorf("command not allowed: %s", a.Command)
	}

	if err := p.validateWorkDir(a.WorkDir); err != nil {
		return err
	}

	if err := p.validateEnvKeys(a.Env); err != nil {
		return err
	}

	return p.validateArgs(a.Args)
}

// validateWorkDir checks if workdir is within project
func (p *DefaultActionPolicy) validateWorkDir(workDir string) error {
	if workDir == "" {
		return nil
	}
	abs, _ := filepath.Abs(workDir)
	rel, err := filepath.Rel(p.projectRoot, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return fmt.Errorf("workdir escapes project: %s", workDir)
	}
	return nil
}

// validateEnvKeys checks if any env keys are denied
func (p *DefaultActionPolicy) validateEnvKeys(env map[string]string) error {
	for k := range env {
		for _, denied := range p.deniedEnvKeys {
			if strings.EqualFold(k, denied) {
				return fmt.Errorf("env key denied: %s", k)
			}
		}
	}
	return nil
}

// validateArgs checks if args contain shell metacharacters
func (p *DefaultActionPolicy) validateArgs(args []string) error {
	for _, arg := range args {
		if strings.ContainsAny(arg, "|&;`$()") {
			return fmt.Errorf("arg contains shell metacharacters: %s", arg)
		}
	}
	return nil
}
