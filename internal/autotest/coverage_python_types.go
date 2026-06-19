package autotest

import "go.uber.org/zap"

// PythonCoverageProvider implements CoverageProvider for Python pytest+coverage.py projects
type PythonCoverageProvider struct {
	config *CoverageConfig
	logger *zap.Logger
}

// CoverageJSON represents the coverage.py JSON output format
type CoverageJSON struct {
	Files map[string]*PythonFileCoverage `json:"files"`
	Meta  *PythonCoverageMeta            `json:"meta"`
}

// PythonFileCoverage represents coverage data for a single Python file
type PythonFileCoverage struct {
	Summary        *PythonCoverageSummary         `json:"summary"`
	ExecutedLines  []int                           `json:"executed_lines"`
	MissingLines   []int                           `json:"missing_lines"`
	ExcludedLines  []int                           `json:"excluded_lines"`
	Functions      map[string]*PythonFuncCoverage  `json:"functions"`
	Lines          map[string]int                 `json:"lines"` // Deprecated: legacy format
}

// PythonCoverageSummary contains summary statistics
type PythonCoverageSummary struct {
	NumStatements  int     `json:"num_statements"`
	CoveredLines   int     `json:"covered_lines"`
	PercentCovered float64 `json:"percent_covered"`
	MissingLines   int     `json:"missing_lines"`
	ExcludedLines  int     `json:"excluded_lines"`
}

// PythonFuncCoverage represents function-level coverage
type PythonFuncCoverage struct {
	ExecutedCount int      `json:"executed_count"`
	MissingLines  []int    `json:"missing_lines"`
	ExcludedLines []int    `json:"excluded_lines"`
	ExecutedLines []int    `json:"executed_lines"`
	Summary       *PythonCoverageSummary `json:"summary"`
	StartLine     int      `json:"start_line"`
}

// PythonCoverageMeta contains metadata
type PythonCoverageMeta struct {
	BranchCoverage bool   `json:"branch_coverage"`
	Timestamp      string `json:"timestamp"`
}
