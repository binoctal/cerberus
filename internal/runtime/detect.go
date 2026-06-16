package runtime

import (
	"os"
	"path/filepath"
)

// IsDevelopment detects if running in the cerberus development environment
// by checking if the current directory contains a go.mod file with the cerberus module name
func IsDevelopment() bool {
	wd, err := os.Getwd()
	if err != nil {
		return false
	}

	// Check if go.mod exists
	goModPath := filepath.Join(wd, "go.mod")
	if _, err := os.Stat(goModPath); err != nil {
		return false
	}

	// Read go.mod to check module name
	content, err := os.ReadFile(goModPath)
	if err != nil {
		return false
	}

	// Check if it's the cerberus module
	return contains(string(content), "module github.com/binoctal/cerberus")
}

// GetPaths returns appropriate paths based on the current environment
// If in development environment, returns project-local paths
// Otherwise, returns system-standard paths
func GetPaths() *Paths {
	if IsDevelopment() {
		wd, err := os.Getwd()
		if err != nil {
			// Fallback to system paths if we can't get working directory
			return New()
		}
		return newDevelopmentPaths(wd)
	}
	return New()
}

// contains checks if s starts with substr
func contains(s, substr string) bool {
	if substr == "" {
		return false
	}
	return len(s) >= len(substr) && s[:len(substr)] == substr
}
