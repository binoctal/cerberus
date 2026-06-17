package autotest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// packageJsonData represents the parsed package.json structure
type packageJsonData struct {
	DevDependencies map[string]string `json:"devDependencies"`
	Dependencies    map[string]string `json:"dependencies"`
	Scripts         struct {
		Test string `json:"test"`
	} `json:"scripts"`
}

// checkBaseRequirements verifies package.json exists
func checkBaseRequirements(projectDir string) (bool, float64, string) {
	pkgPath := filepath.Join(projectDir, "package.json")
	if _, err := os.Stat(pkgPath); os.IsNotExist(err) {
		return false, 0, ""
	}
	return true, 0.5, ""
}

// checkNodeModulesExists verifies node_modules directory exists
func checkNodeModulesExists(projectDir string, currentScore float64) (bool, float64, string) {
	nodeModules := filepath.Join(projectDir, "node_modules")
	stat, err := os.Stat(nodeModules)
	if err != nil || !stat.IsDir() {
		return false, currentScore, ""
	}
	return true, currentScore, ""
}

// readPackageJson reads and parses package.json
func readPackageJson(projectDir string, currentScore float64) (*packageJsonData, float64, string) {
	pkgPath := filepath.Join(projectDir, "package.json")
	data, err := os.ReadFile(pkgPath)
	if err != nil {
		return nil, currentScore, ""
	}

	var pkg packageJsonData
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil, currentScore, ""
	}

	return &pkg, currentScore, ""
}

// hasJestDependency checks if Jest is in dependencies
func hasJestDependency(pkg *packageJsonData) bool {
	return pkg.DevDependencies["jest"] != "" || pkg.Dependencies["jest"] != ""
}

// hasMochaDependency checks if Mocha or nyc is in dependencies or scripts
func hasMochaDependency(pkg *packageJsonData) bool {
	hasMocha := pkg.DevDependencies["mocha"] != "" || pkg.Dependencies["mocha"] != ""
	hasNyc := pkg.DevDependencies["nyc"] != "" || pkg.Dependencies["nyc"] != ""
	hasMochaInScript := strings.Contains(pkg.Scripts.Test, "mocha")
	return hasMocha || hasNyc || hasMochaInScript
}

// findMochaExecutable searches for Mocha executables in node_modules
func findMochaExecutable(projectDir string) (bool, float64, string) {
	nodeModules := filepath.Join(projectDir, "node_modules")

	// Prefer nyc (Mocha with coverage)
	nycPath := filepath.Join(nodeModules, ".bin", "nyc")
	if _, err := os.Stat(nycPath); err == nil {
		return true, 1.0, nycPath
	}

	// Fallback to mocha
	mochaPath := filepath.Join(nodeModules, ".bin", "mocha")
	if _, err := os.Stat(mochaPath); err == nil {
		return true, 0.95, mochaPath
	}

	// No executable found but dependencies indicate Mocha project
	return true, 0.9, ""
}
