package architecture

import (
	"os"
	"path/filepath"
	"strings"
)

// collectInterfaces walks the project to collect all interfaces
func (a *Analyzer) collectInterfaces() (map[string]*InterfaceInfo, error) {
	interfaces := make(map[string]*InterfaceInfo)

	err := filepath.Walk(a.projectPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !a.shouldAnalyzeFile(path, info) {
			return nil
		}

		// Parse file to find interfaces
		if err := a.analyzeFileInterfaces(path, interfaces); err != nil {
			// Continue with other files
			return nil
		}

		return nil
	})

	return interfaces, err
}

// shouldAnalyzeFile checks if a file should be analyzed for interfaces
func (a *Analyzer) shouldAnalyzeFile(path string, info os.FileInfo) bool {
	// Skip directories and non-Go files
	if info.IsDir() || !strings.HasSuffix(path, ".go") {
		return false
	}

	// Skip test files and vendor
	if strings.Contains(path, "_test.go") || strings.Contains(path, "vendor/") {
		return false
	}

	return true
}
