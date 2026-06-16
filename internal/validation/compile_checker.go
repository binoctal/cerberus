package validation

import (
	"bytes"
	"os/exec"
	"strings"
)

// CompileChecker validates compilation errors
type CompileChecker struct{}

// CompileError represents a compilation error
type CompileError struct {
	File       string
	LineNumber int
	Message    string
	Severity   string // "error" | "warning"
}

// NewCompileChecker creates a new compile checker
func NewCompileChecker() *CompileChecker {
	return &CompileChecker{}
}

// Check performs compilation check on project
func (c *CompileChecker) Check(projectPath string) ([]CompileError, error) {
	// Run go build
	cmd := exec.Command("go", "build", "./...")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Dir = projectPath

	err := cmd.Run()

	errors := []CompileError{}

	if err != nil {
		// Parse compilation errors
		errorLines := strings.Split(stderr.String(), "\n")
		for _, line := range errorLines {
			if line == "" {
				continue
			}

			// Parse error format: file.go:line: error message
			parts := strings.Split(line, ":")
			if len(parts) >= 3 {
				errors = append(errors, CompileError{
					File:     strings.TrimSpace(parts[0]),
					Severity: "error",
					Message:  strings.TrimSpace(strings.Join(parts[2:], ":")),
				})
			}
		}
	}

	return errors, nil
}
