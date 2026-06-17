package policy

import (
	"path/filepath"

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
		return p.validateProcessExecAction(a)
	case types.FileWriteAction:
		return p.validateFileWriteAction(a)
	case types.FileReadAction:
		return p.validateFileReadAction(a)
	case types.FileExistsAction:
		return p.validateFileExistsAction(a)
	case types.MCPCallAction:
		return p.validateMCPCallAction(a)
	case types.CodeAnalyzeAction, types.CodeLintAction, types.CodeSymbolsAction:
		return p.validateCodeActions(action)
	default:
		return nil
	}
}
