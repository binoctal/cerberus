package policy

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/binoctal/cerberus/internal/types"
)

// validateMCPCallAction validates MCP call actions
func (p *DefaultActionPolicy) validateMCPCallAction(a types.MCPCallAction) error {
	if !p.allowedMCP[a.Method] {
		return fmt.Errorf("MCP method not allowed: %s", a.Method)
	}
	return nil
}

// validateCodeActions validates code analysis/lint/symbols actions
func (p *DefaultActionPolicy) validateCodeActions(action types.TypedAction) error {
	target := action.Target()
	abs, _ := filepath.Abs(target)
	rel, err := filepath.Rel(p.projectRoot, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return fmt.Errorf("target path escapes project: %s", target)
	}
	return nil
}
