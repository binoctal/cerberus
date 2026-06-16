package autotest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// MochaProjectDetector detects Mocha + nyc projects
type MochaProjectDetector struct{}

func (d *MochaProjectDetector) Type() ProjectType { return ProjectTypeMocha }

func (d *MochaProjectDetector) Detect(projectDir string) (bool, float64, string) {
	// Base check (0.5): package.json exists
	pkgPath := filepath.Join(projectDir, "package.json")
	if _, err := os.Stat(pkgPath); os.IsNotExist(err) {
		return false, 0, ""
	}

	// Loose check (0.7): node_modules exists, no Jest
	nodeModules := filepath.Join(projectDir, "node_modules")
	stat, err := os.Stat(nodeModules)
	if err != nil || !stat.IsDir() {
		return false, 0.5, ""
	}

	// Check if Jest exists (don't interfere with Jest projects)
	data, err := os.ReadFile(pkgPath)
	if err != nil {
		return false, 0.6, ""
	}

	var pkg struct {
		DevDependencies map[string]string `json:"devDependencies"`
		Dependencies    map[string]string `json:"dependencies"`
		Scripts         struct {
			Test string `json:"test"`
		} `json:"scripts"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return false, 0.6, ""
	}

	// If Jest exists, let Jest detector handle it
	hasJest := pkg.DevDependencies["jest"] != "" || pkg.Dependencies["jest"] != ""
	if hasJest {
		return false, 0, ""
	}

	// Strict check (0.9): mocha or nyc in dependencies
	hasMocha := pkg.DevDependencies["mocha"] != "" || pkg.Dependencies["mocha"] != ""
	hasNyc := pkg.DevDependencies["nyc"] != "" || pkg.Dependencies["nyc"] != ""
	hasMochaInScript := strings.Contains(pkg.Scripts.Test, "mocha")

	if hasMocha || hasNyc || hasMochaInScript {
		// Validation check (1.0): find executable
		nycPath := filepath.Join(nodeModules, ".bin", "nyc")
		if _, err := os.Stat(nycPath); err == nil {
			return true, 1.0, nycPath
		}

		mochaPath := filepath.Join(nodeModules, ".bin", "mocha")
		if _, err := os.Stat(mochaPath); err == nil {
			return true, 0.95, mochaPath
		}

		// No executable found but dependencies indicate Mocha project
		return true, 0.9, ""
	}

	// Node project but no Mocha detected
	return false, 0.7, ""
}
