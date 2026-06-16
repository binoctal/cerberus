package autotest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// parseIstanbulCoverage parses Istanbul JSON coverage format
// Istanbul JSON format is identical to Jest JSON format
func (p *MochaCoverageProvider) parseIstanbulCoverage(data []byte) (*CoverageReport, error) {
	// Reuse Jest coverage parsing logic - same format!
	var istanbulData JestCoverageJSON
	if err := json.Unmarshal(data, &istanbulData); err != nil {
		return nil, fmt.Errorf("unmarshal istanbul coverage: %w", err)
	}

	report := &CoverageReport{
		Profile: make([]CoverageLine, 0),
	}

	// Process each file's coverage
	for file, fileData := range istanbulData {
		for stmtIdx, count := range fileData.S {
			if stmtRange, ok := fileData.StatementMap[stmtIdx]; ok && stmtRange != nil {
				startLine := stmtRange.Start.Line
				endLine := stmtRange.End.Line

				report.Profile = append(report.Profile, CoverageLine{
					File:  file,
					Start: startLine,
					End:   endLine,
					Count: count,
				})

				report.TotalFuncs++
				if count > 0 {
					report.CoveredFuncs++
				}
			}
		}
	}

	return report, nil
}

// Gaps turns a coverage report into uncovered targets
func (p *MochaCoverageProvider) Gaps(report *CoverageReport) []CoverageGap {
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

// NoTestFileGaps walks projectDir for *.js source files that have no test file
// Supports both test/ directory and same-directory organization
func (p *MochaCoverageProvider) NoTestFileGaps(projectDir string) []CoverageGap {
	var gaps []CoverageGap

	_ = filepath.Walk(projectDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}

		// Skip non-JS files
		if !strings.HasSuffix(path, ".js") {
			return nil
		}

		// Skip test files themselves (.test.js and .spec.js)
		if strings.HasSuffix(path, ".test.js") || strings.HasSuffix(path, ".spec.js") {
			return nil
		}

		// Skip certain directories
		if shouldSkipNodeFile(path) {
			return nil
		}

		// Check for test file using intelligent path detection
		testFile := MochaTestFilePath(path, projectDir)
		if _, statErr := os.Stat(testFile); os.IsNotExist(statErr) {
			gaps = append(gaps, CoverageGap{
				File:   path,
				Reason: ReasonNoTestFile,
			})
		}

		return nil
	})

	return gaps
}
