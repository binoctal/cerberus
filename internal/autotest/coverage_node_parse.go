package autotest

import (
	"encoding/json"
	"fmt"
	"sort"
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

	// Map iteration order is non-deterministic; sort the profile into a stable
	// source order (file, then start line, then end line) so output is
	// reproducible and callers can assert against positional results.
	sort.Slice(report.Profile, func(i, j int) bool {
		if report.Profile[i].File != report.Profile[j].File {
			return report.Profile[i].File < report.Profile[j].File
		}
		if report.Profile[i].Start != report.Profile[j].Start {
			return report.Profile[i].Start < report.Profile[j].Start
		}
		return report.Profile[i].End < report.Profile[j].End
	})

	return report, nil
}
