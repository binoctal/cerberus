package policy

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/binoctal/cerberus/internal/types"
)

// validateFileWriteAction validates file write actions
func (p *DefaultActionPolicy) validateFileWriteAction(a types.FileWriteAction) error {
	abs, _ := filepath.Abs(a.Path)
	if err := p.checkDeniedPath(abs, a.Path); err != nil {
		return err
	}
	rel, err := filepath.Rel(p.projectRoot, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return fmt.Errorf("path escapes project: %s", a.Path)
	}
	return nil
}

// validateFileReadAction validates file read actions
func (p *DefaultActionPolicy) validateFileReadAction(a types.FileReadAction) error {
	return p.checkDeniedPathSimple(a.Path)
}

// validateFileExistsAction validates file exists actions
func (p *DefaultActionPolicy) validateFileExistsAction(a types.FileExistsAction) error {
	return p.checkDeniedPathSimple(a.Path)
}

// checkDeniedPath checks if a path is denied
func (p *DefaultActionPolicy) checkDeniedPath(abs, path string) error {
	for _, denied := range p.deniedPaths {
		if abs == denied {
			return fmt.Errorf("path denied: %s", path)
		}
	}
	return nil
}

// checkDeniedPathSimple checks denied path without escaping check
func (p *DefaultActionPolicy) checkDeniedPathSimple(path string) error {
	abs, _ := filepath.Abs(path)
	for _, denied := range p.deniedPaths {
		if abs == denied {
			return fmt.Errorf("path denied: %s", path)
		}
	}
	return nil
}
