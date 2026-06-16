package autotest

import (
	"encoding/json"
	"os"
	"path/filepath"
)

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
		return false, 0, "" // Not a Jest project, let Mocha detector handle it
	}

	// Check for Jest executable
	jestPath := filepath.Join(nodeModules, ".bin", "jest")
	if _, err := os.Stat(jestPath); err == nil {
		return true, 1.0, jestPath
	}

	// Fall back to npx jest
	return true, 0.9, "jest"
}
