package autotest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Gaps extracts uncovered lines from a coverage report
func (p *PythonCoverageProvider) Gaps(report *CoverageReport) []CoverageGap {
	var gaps []CoverageGap

	for _, line := range report.Profile {
		if line.Count == 0 {
			gaps = append(gaps, CoverageGap{
				File:   line.File,
				Func:   fmt.Sprintf("%s:L%d", filepath.Base(line.File), line.Start),
				Reason: ReasonZeroCover,
			})
		}
	}

	return gaps
}

// NoTestFileGaps walks projectDir for *.py source files that have no sibling test_*.py
func (p *PythonCoverageProvider) NoTestFileGaps(projectDir string) []CoverageGap {
	var gaps []CoverageGap

	_ = filepath.Walk(projectDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}

		// Skip non-Python files
		if !strings.HasSuffix(path, ".py") {
			return nil
		}

		// Skip test files themselves
		if strings.HasPrefix(filepath.Base(path), "test_") ||
			strings.HasSuffix(path, "_test.py") {
			return nil
		}

		// Skip __pycache__ and other exclusions
		if shouldSkipPythonFile(path) {
			return nil
		}

		// Check for test file in tests/ directory first
		projectRoot := findProjectRoot(projectDir)
		relPath, _ := filepath.Rel(projectRoot, path)

		// Try tests/ directory
		testsDir := filepath.Join(projectRoot, "tests")
		if _, err := os.Stat(testsDir); err == nil {
			testPath := filepath.Join(testsDir, "test_"+filepath.Base(path))
			if _, statErr := os.Stat(testPath); os.IsNotExist(statErr) {
				// Try with subdirectory structure
				subTestPath := filepath.Join(testsDir, filepath.Dir(relPath), "test_"+filepath.Base(path))
				if _, subErr := os.Stat(subTestPath); os.IsNotExist(subErr) {
					gaps = append(gaps, CoverageGap{
						File:   path,
						Reason: ReasonNoTestFile,
					})
				}
			}
		} else {
			// No tests/ directory, check same directory
			testFile := filepath.Join(filepath.Dir(path), "test_"+filepath.Base(path))
			if _, statErr := os.Stat(testFile); os.IsNotExist(statErr) {
				gaps = append(gaps, CoverageGap{
					File:   path,
					Reason: ReasonNoTestFile,
				})
			}
		}

		return nil
	})

	return gaps
}
