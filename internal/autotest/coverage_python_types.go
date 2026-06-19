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
	Summary   *PythonCoverageSummary         `json:"summary"`
	Lines     map[string]int                 `json:"lines"`
	Functions map[string]*PythonFuncCoverage `json:"functions"`
}

// PythonCoverageSummary contains summary statistics
type PythonCoverageSummary struct {
	NumStatements  int     `json:"num_statements"`
	CoveredLines   int     `json:"covered_lines"`
	PercentCovered float64 `json:"percent_covered"`
	MissingLines   string  `json:"missing_lines"`
}

// PythonFuncCoverage represents function-level coverage
type PythonFuncCoverage struct {
	ExecutedCount int      `json:"executed_count"`
	MissingLines  []string `json:"missing_lines"`
}

// PythonCoverageMeta contains metadata
type PythonCoverageMeta struct {
	BranchCoverage bool   `json:"branch_coverage"`
	Timestamp      string `json:"timestamp"`
}
