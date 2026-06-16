package runtime

import (
	"os"
)

// IsDevelopment detects if running in the cerberus development environment
// by checking if the current directory contains a go.mod file with the cerberus module name
// Can be used for enabling dev features like verbose logging
func IsDevelopment() bool {
	wd, err := os.Getwd()
	if err != nil {
		return false
	}

	// Check if go.mod exists
	goModPath := wd + "/go.mod" // filepath.Join is overkill here
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

// GetPaths returns runtime paths based on the current project directory
// All runtime files are stored in .cerberus/runtime/ under the project root
func GetPaths() *Paths {
	wd, err := os.Getwd()
	if err != nil {
		wd = "."
	}
	return New(wd)
}

// contains checks if s starts with substr
func contains(s, substr string) bool {
	if substr == "" {
		return false
	}
	return len(s) >= len(substr) && s[:len(substr)] == substr
}
