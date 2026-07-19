package autotest

import (
	"encoding/json"
	"fmt"
)

// parseJSONCoverage parses coverage.py JSON format
func (p *PythonCoverageProvider) parseJSONCoverage(data []byte) (*CoverageReport, error) {
	var covData CoverageJSON
	if err := json.Unmarshal(data, &covData); err != nil {
		return nil, fmt.Errorf("unmarshal python coverage: %w", err)
	}

	report := &CoverageReport{
		Profile: make([]CoverageLine, 0),
	}

	// Process each file's coverage
	for file, fileData := range covData.Files {
		// Use new format (executed/missing line arrays) if available
		if len(fileData.ExecutedLines) > 0 || len(fileData.MissingLines) > 0 {
			// Mark executed lines with count > 0
			for _, lineNum := range fileData.ExecutedLines {
				report.Profile = append(report.Profile, CoverageLine{
					File:  file,
					Start: lineNum,
					End:   lineNum + 1,
					Count: 1,
				})
				report.TotalFuncs++
				report.CoveredFuncs++
			}
			// Mark missing lines with count = 0
			for _, lineNum := range fileData.MissingLines {
				report.Profile = append(report.Profile, CoverageLine{
					File:  file,
					Start: lineNum,
					End:   lineNum + 1,
					Count: 0,
				})
				report.TotalFuncs++
			}
		} else if fileData.Lines != nil {
			// Fallback to legacy format (lines map)
			for lineStr, count := range fileData.Lines {
				lineNum := 0
				if _, err := fmt.Sscanf(lineStr, "%d", &lineNum); err == nil {
					report.Profile = append(report.Profile, CoverageLine{
						File:  file,
						Start: lineNum,
						End:   lineNum + 1,
						Count: count,
					})

					report.TotalFuncs++
					if count > 0 {
						report.CoveredFuncs++
					}
				}
			}
		}
	}

	return report, nil
}
