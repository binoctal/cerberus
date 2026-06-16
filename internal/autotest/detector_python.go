package autotest

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// PythonProjectDetector detects Python projects with pytest and coverage.py
type PythonProjectDetector struct{}

func (d *PythonProjectDetector) Type() ProjectType { return ProjectTypePython }

func (d *PythonProjectDetector) Detect(projectDir string) (bool, float64, string) {
	// Check for Python project markers
	hasConfig := false
	for _, name := range []string{"requirements.txt", "setup.py", "pyproject.toml"} {
		if _, err := os.Stat(filepath.Join(projectDir, name)); err == nil {
			hasConfig = true
			break
		}
	}
	if !hasConfig {
		return false, 0, ""
	}

	// Find Python interpreter
	pythonCmd := findPythonCmd(projectDir)
	if pythonCmd == "" {
		return false, 0.5, "" // Python project but no interpreter found
	}

	// Check if pytest is available
	if !checkPythonModule(pythonCmd, "pytest") {
		return false, 0.7, "" // Python project but pytest not installed
	}

	// Check if coverage.py is available
	if !checkPythonModule(pythonCmd, "coverage") {
		return false, 0.85, "" // Has pytest but no coverage
	}

	return true, 1.0, pythonCmd
}

// findPythonCmd finds the Python interpreter in venv or system PATH
func findPythonCmd(projectDir string) string {
	// Check virtual environments in priority order
	venvPaths := []string{
		"venv/bin/python",
		".venv/bin/python",
		"env/bin/python",
	}

	for _, venvPath := range venvPaths {
		fullPath := filepath.Join(projectDir, venvPath)
		if _, err := os.Stat(fullPath); err == nil {
			return fullPath
		}
	}

	// Check system PATH
	for _, cmd := range []string{"python3", "python"} {
		if path, err := exec.LookPath(cmd); err == nil {
			return path
		}
	}

	return ""
}

// checkPythonModule checks if a Python module is available
func checkPythonModule(pythonCmd, module string) bool {
	cmd := exec.Command(pythonCmd, "-c", fmt.Sprintf("import %s", module))
	return cmd.Run() == nil
}

// hasPythonConfig checks if a Python project config file exists
func hasPythonConfig(projectDir string) bool {
	for _, name := range []string{"requirements.txt", "setup.py", "pyproject.toml"} {
		if _, err := os.Stat(filepath.Join(projectDir, name)); err == nil {
			return true
		}
	}
	return false
}
