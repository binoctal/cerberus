package autotest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Gaps extracts uncovered lines from a coverage report
func (p *NodeCoverageProvider) Gaps(report *CoverageReport) []CoverageGap {
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

// NoTestFileGaps walks projectDir for *.js source files that have no sibling *.test.js
func (p *NodeCoverageProvider) NoTestFileGaps(projectDir string) []CoverageGap {
	var gaps []CoverageGap

	_ = filepath.Walk(projectDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}

		// Skip non-JS files
		if !strings.HasSuffix(path, ".js") {
			return nil
		}

		// Skip test files themselves
		if strings.HasSuffix(path, ".test.js") || strings.HasSuffix(path, ".spec.js") {
			return nil
		}

		// Skip certain directories
		if shouldSkipNodeFile(path) {
			return nil
		}

		// Check for test file
		testFile := strings.TrimSuffix(path, ".js") + ".test.js"
		if _, statErr := os.Stat(testFile); os.IsNotExist(statErr) {
			// Also check for .spec.js variant
			specFile := strings.TrimSuffix(path, ".js") + ".spec.js"
			if _, specErr := os.Stat(specFile); os.IsNotExist(specErr) {
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
