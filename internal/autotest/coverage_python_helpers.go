package autotest

import (
	"os"
	"path/filepath"
	"strings"

	"go.uber.org/zap"
)

// shouldSkipPythonFile determines if a Python file should be excluded from coverage
func shouldSkipPythonFile(path string) bool {
	base := filepath.Base(path)

	// Skip __pycache__, .pyc files
	if strings.HasSuffix(base, ".pyc") ||
		base == "__init__.py" ||
		strings.Contains(path, "__pycache__") {
		return true
	}

	// Skip common exclusion directories
	for _, seg := range strings.Split(path, string(filepath.Separator)) {
		if seg == ".git" ||
			seg == "venv" ||
			seg == ".venv" ||
			seg == "env" ||
			seg == "dist" ||
			seg == "build" ||
			seg == ".pytest_cache" {
			return true
		}
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
