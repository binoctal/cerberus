package autotest

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// ProjectType represents the type of project (Go, Node, Python)
type ProjectType string

const (
	ProjectTypeGo     ProjectType = "go"
	ProjectTypeNode   ProjectType = "node"
	ProjectTypePython ProjectType = "python"
)

// ProjectDetector detects if a project supports a specific test framework
type ProjectDetector interface {
	Detect(projectDir string) (supported bool, confidence float64, toolPath string)
	Type() ProjectType
}

// DetectProjectType automatically detects the project type
func DetectProjectType(projectDir string) (ProjectType, float64, error) {
	detectors := []ProjectDetector{
		&GoProjectDetector{},
		&NodeProjectDetector{},
		&PythonProjectDetector{},
	}

	var bestType ProjectType
	var bestConfidence float64

	for _, detector := range detectors {
		supported, confidence, _ := detector.Detect(projectDir)
		if supported && confidence > bestConfidence {
			bestType = detector.Type()
			bestConfidence = confidence
		}
	}

	if bestConfidence >= 0.9 {
		return bestType, bestConfidence, nil
	}

	return "", 0, fmt.Errorf("no supported project type detected (confidence threshold 0.9 not met)")
}

// GoProjectDetector detects Go projects
type GoProjectDetector struct{}

func (d *GoProjectDetector) Type() ProjectType { return ProjectTypeGo }

func (d *GoProjectDetector) Detect(projectDir string) (bool, float64, string) {
	// Check for go.mod
	goModPath := filepath.Join(projectDir, "go.mod")
	if _, err := os.Stat(goModPath); os.IsNotExist(err) {
		return false, 0, ""
	}

	// Verify go command is available
	_, err := exec.LookPath("go")
	if err != nil {
		return false, 0.5, "" // go.mod exists but Go not installed
	}

	return true, 1.0, "go"
}

// NodeProjectDetector detects Node.js projects with Jest
type NodeProjectDetector struct{}

func (d *NodeProjectDetector) Type() ProjectType { return ProjectTypeNode }

func (d *NodeProjectDetector) Detect(projectDir string) (bool, float64, string) {
	// Check for package.json
	pkgPath := filepath.Join(projectDir, "package.json")
	if _, err := os.Stat(pkgPath); os.IsNotExist(err) {
		return false, 0, ""
	}

	// Check if node_modules exists and is accessible
	nodeModules := filepath.Join(projectDir, "node_modules")
	stat, err := os.Stat(nodeModules)
	if err != nil || !stat.IsDir() {
		return false, 0.5, "" // Node project but dependencies not installed
	}

	// Check if Jest is in dependencies
	data, err := os.ReadFile(pkgPath)
	if err != nil {
		return false, 0.6, ""
	}

	var pkg struct {
		DevDependencies map[string]string `json:"devDependencies"`
		Dependencies    map[string]string `json:"dependencies"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return false, 0.6, ""
	}

	hasJest := pkg.DevDependencies["jest"] != "" || pkg.Dependencies["jest"] != ""
	if !hasJest {
		return false, 0.7, "" // Node project but Jest not installed
	}

	// Check for Jest executable
	jestPath := filepath.Join(nodeModules, ".bin", "jest")
	if _, err := os.Stat(jestPath); err == nil {
		return true, 1.0, jestPath
	}

	// Fall back to npx jest
	return true, 0.9, "jest"
}

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

// quickDetect performs a quick check for project markers without tool availability
func quickDetect(projectDir string) []ProjectType {
	var types []ProjectType

	// Check for Go
	if _, err := os.Stat(filepath.Join(projectDir, "go.mod")); err == nil {
		types = append(types, ProjectTypeGo)
	}

	// Check for Node
	if _, err := os.Stat(filepath.Join(projectDir, "package.json")); err == nil {
		types = append(types, ProjectTypeNode)
	}

	// Check for Python
	if hasPythonConfig(projectDir) {
		types = append(types, ProjectTypePython)
	}

	return types
}

// CreateProvider creates a provider and generator for the detected project type
func CreateProvider(typ ProjectType, driver interface{}, cfg *CoverageConfig) (CoverageProvider, TestGenerator, error) {
	switch typ {
	case ProjectTypeGo:
		// Use existing Go provider (driver is *ai.Driver)
		// This will be handled by the existing code path
		return nil, nil, fmt.Errorf("Go provider should use existing path")
	case ProjectTypeNode:
		return NewNodeCoverageProvider(cfg), NewNodeTestGenerator(driver), nil
	case ProjectTypePython:
		return NewPythonCoverageProvider(cfg), NewPythonTestGenerator(driver), nil
	default:
		return nil, nil, fmt.Errorf("unsupported project type: %s", typ)
	}
}

// DetectAndCreateProvider auto-detects project type and creates appropriate provider
func DetectAndCreateProvider(projectDir string, driver interface{}) (CoverageProvider, TestGenerator, error) {
	typ, confidence, err := DetectProjectType(projectDir)
	if err != nil {
		return nil, nil, fmt.Errorf("detect project type: %w (confidence: %.2f)", err, confidence)
	}

	// Use default config for the detected type
	var cfg *CoverageConfig
	switch typ {
	case ProjectTypeNode:
		cfg = DefaultNodeCoverageConfig()
	case ProjectTypePython:
		cfg = DefaultPythonCoverageConfig()
	default:
		cfg = &CoverageConfig{}
	}

	return CreateProvider(typ, driver, cfg)
}
