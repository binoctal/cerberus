package architecture

import (
	"os"
	"path/filepath"
	"strings"
)

// analyzeSOLID analyzes SOLID principle compliance across the project
// This is the main entry point that coordinates SRP and OCP analysis
func (a *Analyzer) analyzeSOLID(report *ArchitectureReport) error {
	// Traverse all Go files in the project
	err := filepath.Walk(a.projectPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories and non-Go files
		if info.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}

		// Skip test files and vendor directory
		if strings.Contains(path, "_test.go") || strings.Contains(path, "vendor/") {
			return nil
		}

		// Analyze SRP compliance
		srpIssues := a.analyzeSRP(path, report)
		report.Issues = append(report.Issues, srpIssues...)

		// Analyze OCP compliance
		ocpIssues := a.analyzeOCP(path, report)
		report.Issues = append(report.Issues, ocpIssues...)

		return nil
	})

	return err
}
