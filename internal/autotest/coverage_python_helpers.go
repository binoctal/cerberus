package autotest

import (
	"os"
	"path/filepath"

	"go.uber.org/zap"
)

// shouldSkipPythonFile determines if a Python file should be excluded from coverage
func shouldSkipPythonFile(path string) bool {
	// Check for Python cache artifacts
	if isPythonCacheArtifact(path) {
		return true
	}

	// Check for excluded directories
	if isInExcludedDir(path) {
		return true
	}

	return false
}

// findProjectRoot attempts to find the project root directory
func findProjectRoot(startDir string) string {
	// Look for common project markers
	markers := []string{
		"requirements.txt",
		"setup.py",
		"pyproject.toml",
		".git",
	}

	current := startDir
	for {
		for _, marker := range markers {
			if _, err := os.Stat(filepath.Join(current, marker)); err == nil {
				return current
			}
		}

		parent := filepath.Dir(current)
		if parent == current {
			break // Reached root
		}
		current = parent
	}

	return startDir
}

// SetLogger sets the logger for the provider
func (p *PythonCoverageProvider) SetLogger(logger *zap.Logger) {
	p.logger = logger
}
