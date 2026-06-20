package autotest

import (
	"os"
	"os/exec"
	"path/filepath"
)

// PythonProjectDetector detects Python projects with pytest and coverage.py
type PythonProjectDetector struct{}

func (d *PythonProjectDetector) Type() ProjectType { return ProjectTypePython }

func (d *PythonProjectDetector) Detect(projectDir string) (bool, float64, string) {
	// Check for Python project markers
	configFile := ""
	for _, name := range []string{"requirements.txt", "setup.py", "pyproject.toml"} {
		if _, err := os.Stat(filepath.Join(projectDir, name)); err == nil {
			configFile = name
			break
		}
	}
	if configFile == "" {
		return false, 0, ""
	}

	// Check if requirements.txt contains pytest and coverage
	hasPytest := false
	hasCoverage := false
	if configFile == "requirements.txt" {
		data, err := os.ReadFile(filepath.Join(projectDir, configFile))
		if err != nil {
			return false, 0.5, ""
		}
		content := string(data)
		hasPytest = containsRequirement(content, "pytest")
		hasCoverage = containsRequirement(content, "coverage")
	} else {
		// For setup.py or pyproject.toml, just check if files exist
		// (parsing these formats is complex; skip for now)
		hasPytest = true
		hasCoverage = true
	}

	if !hasPytest {
		return false, 0.7, "" // Python project but pytest not in requirements
	}

	if !hasCoverage {
		return false, 0.85, "" // Has pytest but no coverage in requirements
	}

	// Find Python interpreter
	pythonCmd := findPythonCmd(projectDir)
	if pythonCmd == "" {
		return false, 0.9, "" // All requirements met but no interpreter found
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

// containsRequirement checks if a requirements.txt file contains a given package
func containsRequirement(content, packageName string) bool {
	// Simple check: does the package name appear in the content?
	// This handles cases like "pytest", "pytest==7.0.0", "pytest>=7.0", etc.
	return containsWord(content, packageName)
}

// containsWord checks if content contains a word as a whole word, not as substring
func containsWord(content, word string) bool {
	for i := 0; i <= len(content)-len(word); i++ {
		match := content[i:i+len(word)] == word

		// Check if character before and after are delimiters (or boundaries)
		beforeOk := i == 0 || isDelimiter(rune(content[i-1]))
		afterOk := i+len(word) >= len(content) || isDelimiter(rune(content[i+len(word)]))

		if match && beforeOk && afterOk {
			return true
		}
	}
	return false
}

// isDelimiter checks if a character is a delimiter
func isDelimiter(c rune) bool {
	return c == ' ' || c == '\n' || c == '\t' || c == '=' || c == '>' || c == '<' || c == '#'
}
