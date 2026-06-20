package autotest

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
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

// parseSQLiteCoverage parses coverage.py SQLite database directly
func (p *PythonCoverageProvider) parseSQLiteCoverage(projectDir string) (*CoverageReport, error) {
	dbPath := filepath.Join(projectDir, p.config.DatabasePath)
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		// No coverage database exists yet (e.g., no tests written yet)
		// Return empty report indicating no coverage
		return &CoverageReport{
			Profile:      make([]CoverageLine, 0),
			TotalFuncs:   0,
			CoveredFuncs: 0,
			Pass:         true,
		}, nil
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("python coverage: open database: %w", err)
	}
	defer func() { _ = db.Close() }()

	report := &CoverageReport{
		Profile: make([]CoverageLine, 0),
	}

	// Check if the expected 'line' table exists in the coverage.py database
	// Coverage.py schema varies by version; we only parse if our expected schema exists.
	var tableName string
	err = db.QueryRowContext(context.Background(), `
		SELECT name FROM sqlite_master WHERE type='table' AND name='line'
	`).Scan(&tableName)
	if err != nil {
		if err == sql.ErrNoRows {
			// The 'line' table doesn't exist in this version of coverage.py
			// or the database is empty (no tests run yet). Return empty report.
			return report, nil
		}
		return nil, fmt.Errorf("python coverage: check schema: %w", err)
	}

	// Query line coverage
	rows, err := db.QueryContext(context.Background(), `
		SELECT file.path, line.number, COALESCE(line_hits.number, 0)
		FROM line
		LEFT JOIN (
			SELECT line_id, COUNT(*) as number
			FROM line_hits
			GROUP BY line_id
		) line_hits ON line.id = line_hits.line_id
		JOIN file ON line.file_id = file.id
	`)
	if err != nil {
		return nil, fmt.Errorf("python coverage: query database: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var filePath string
		var lineNum, count int

		if err := rows.Scan(&filePath, &lineNum, &count); err != nil {
			continue
		}

		report.Profile = append(report.Profile, CoverageLine{
			File:  filePath,
			Start: lineNum,
			End:   lineNum + 1,
			Count: count,
		})

		report.TotalFuncs++
		if count > 0 {
			report.CoveredFuncs++
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("python coverage: scan rows: %w", err)
	}

	return report, nil
}
