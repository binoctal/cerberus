package autotest

import "go.uber.org/zap"

// NodeCoverageProvider implements CoverageProvider for Node.js Jest projects
type NodeCoverageProvider struct {
	config *CoverageConfig
	logger *zap.Logger
}

// JestCoverageJSON represents the Jest JSON coverage format
type JestCoverageJSON map[string]*JestFileCoverage

// JestFileCoverage represents coverage data for a single file
type JestFileCoverage struct {
	StatementMap map[string]*JestRange `json:"statementMap"`
	S            map[string]int        `json:"s"`         // Statement counts
	Functions    map[string]int        `json:"functions"` // Function counts
	BranchMap    map[string]*JestRange `json:"b"`         // Branch map (optional)
	B            map[string]int        `json:"branchMap"` // Branch counts (optional)
}

// JestRange represents a code range in Jest coverage
type JestRange struct {
	Start *JestPosition `json:"start"`
	End   *JestPosition `json:"end"`
	Skip  bool          `json:"skip,omitempty"`
}

// JestPosition represents a position in source code
type JestPosition struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}
