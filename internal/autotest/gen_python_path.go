package autotest

import (
	"os"
	"path/filepath"
	"strings"
)

// pythonTestFilePath returns the test file path for a Python source file
func pythonTestFilePath(sourceFile string) string {
	// Check if there's a tests/ directory at project root
	projectRoot := findProjectRoot(sourceFile)
	testsDir := filepath.Join(projectRoot, "tests")

	if _, err := os.Stat(testsDir); err == nil {
		// Use tests/ directory
		relPath, _ := filepath.Rel(projectRoot, sourceFile)
		testRelPath := "test_" + filepath.Base(relPath)
		return filepath.Join(testsDir, testRelPath)
	}

	// Default: same directory as source
	dir := filepath.Dir(sourceFile)
	base := filepath.Base(sourceFile)
	name := strings.TrimSuffix(base, ".py")
	return filepath.Join(dir, "test_"+name+".py")
}
