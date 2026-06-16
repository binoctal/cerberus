package autotest

import (
	"encoding/json"
	"fmt"
)

// parseJestCoverage parses Jest JSON coverage format
func (p *NodeCoverageProvider) parseJestCoverage(data []byte) (*CoverageReport, error) {
	var jestData JestCoverageJSON
	if err := json.Unmarshal(data, &jestData); err != nil {
		return nil, fmt.Errorf("unmarshal jest coverage: %w", err)
	}

	report := &CoverageReport{
		Profile: make([]CoverageLine, 0),
	}

	// Process each file's coverage
	for file, fileData := range jestData {
		// Process statements
		for stmtIdx, count := range fileData.S {
			if stmtRange, ok := fileData.StatementMap[stmtIdx]; ok && stmtRange != nil {
				startLine := stmtRange.Start.Line
				endLine := stmtRange.End.Line

				report.Profile = append(report.Profile, CoverageLine{
					File:  file,
					Start: startLine,
					End:    endLine,
					Count:  count,
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
