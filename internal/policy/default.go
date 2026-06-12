package policy

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/binoctal/cerberus/internal/types"
)

// DefaultActionPolicy implements ActionPolicy with allowlist/denylist checks.
type DefaultActionPolicy struct {
	projectRoot   string
	allowedCmds   map[string]bool
	deniedPaths   []string
	deniedEnvKeys []string
	allowedMCP    map[string]bool
}

// NewDefaultActionPolicy creates a policy with sensible defaults.
func NewDefaultActionPolicy(projectRoot string) *DefaultActionPolicy {
	abs, _ := filepath.Abs(projectRoot)
	return &DefaultActionPolicy{
		projectRoot: abs,
		allowedCmds: map[string]bool{
			"go": true, "node": true, "npm": true, "npx": true,
			"python": true, "pytest": true, "ruff": true,
			"eslint": true, "tsc": true, "mypy": true,
			"make": true, "cargo": true, "git": true,
			"golangci-lint": true, "gofmt": true, "goimports": true,
		},
		deniedPaths: []string{
			"/etc/shadow", "/etc/passwd", "/root/.ssh",
			"/.env", "/var/run/docker.sock",
		},
		deniedEnvKeys: []string{
			"HOME", "USER", "SUDO_USER", "SSH_AUTH_SOCK",
		},
		allowedMCP: map[string]bool{
			"tools/call": true, "tools/list": true,
			"resources/read": true,
		},
	}
}

// Validate checks the action against policy rules.
func (p *DefaultActionPolicy) Validate(action types.TypedAction) error {
	switch a := action.(type) {
	case types.ProcessExecAction:
		if !p.allowedCmds[a.Command] {
			return fmt.Errorf("command not allowed: %s", a.Command)
		}
		if a.WorkDir != "" {
			abs, _ := filepath.Abs(a.WorkDir)
			rel, err := filepath.Rel(p.projectRoot, abs)
			if err != nil || strings.HasPrefix(rel, "..") {
				return fmt.Errorf("workdir escapes project: %s", a.WorkDir)
			}
		}
		for k := range a.Env {
			for _, denied := range p.deniedEnvKeys {
				if strings.EqualFold(k, denied) {
					return fmt.Errorf("env key denied: %s", k)
				}
			}
		}
		for _, arg := range a.Args {
			if strings.ContainsAny(arg, "|&;`$()") {
				return fmt.Errorf("arg contains shell metacharacters: %s", arg)
			}
		}

	case types.FileWriteAction:
		abs, _ := filepath.Abs(a.Path)
		for _, denied := range p.deniedPaths {
			if abs == denied {
				return fmt.Errorf("path denied: %s", a.Path)
			}
		}
		rel, err := filepath.Rel(p.projectRoot, abs)
		if err != nil || strings.HasPrefix(rel, "..") {
			return fmt.Errorf("path escapes project: %s", a.Path)
		}

	case types.FileReadAction:
		abs, _ := filepath.Abs(a.Path)
		for _, denied := range p.deniedPaths {
			if abs == denied {
				return fmt.Errorf("path denied: %s", a.Path)
			}
		}

	case types.FileExistsAction:
		abs, _ := filepath.Abs(a.Path)
		for _, denied := range p.deniedPaths {
			if abs == denied {
				return fmt.Errorf("path denied: %s", a.Path)
			}
		}

	case types.MCPCallAction:
		if !p.allowedMCP[a.Method] {
			return fmt.Errorf("MCP method not allowed: %s", a.Method)
		}

	case types.CodeAnalyzeAction, types.CodeLintAction, types.CodeSymbolsAction:
		abs, _ := filepath.Abs(a.Target())
		rel, err := filepath.Rel(p.projectRoot, abs)
		if err != nil || strings.HasPrefix(rel, "..") {
			return fmt.Errorf("target path escapes project: %s", a.Target())
		}
	}
	return nil
}
