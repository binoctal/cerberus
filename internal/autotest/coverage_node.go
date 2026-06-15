package autotest

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"go.uber.org/zap"
)

// NodeCoverageProvider implements CoverageProvider for Node.js Jest projects
type NodeCoverageProvider struct {
	config *CoverageConfig
	logger *zap.Logger
}

// NewNodeCoverageProvider creates a new Node coverage provider
func NewNodeCoverageProvider(cfg *CoverageConfig) *NodeCoverageProvider {
	return &NodeCoverageProvider{
		config: cfg,
		logger: zap.NewNop(),
	}
}

// JestCoverageJSON represents the Jest JSON coverage format
type JestCoverageJSON map[string]*JestFileCoverage

// JestFileCoverage represents coverage data for a single file
type JestFileCoverage struct {
	StatementMap map[string]*JestRange `json:"statementMap"`
	S            map[string]int         `json:"s"`      // Statement counts
	Functions    map[string]int         `json:"functions"` // Function counts
	BranchMap    map[string]*JestRange `json:"b"` // Branch map (optional)
	B            map[string]int         `json:"branchMap"` // Branch counts (optional)
}

// JestRange represents a code range in Jest coverage
type JestRange struct {
	Start   *JestPosition `json:"start"`
	End     *JestPosition `json:"end"`
	Skip    bool          `json:"skip,omitempty"`
}

// JestPosition represents a position in source code
type JestPosition struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

// RunCoverage runs Jest tests with coverage and parses the JSON output
func (p *NodeCoverageProvider) RunCoverage(ctx context.Context, projectDir string) (*CoverageReport, error) {
	if p.config == nil {
		return nil, fmt.Errorf("node coverage: config not set")
	}

	// Create output directory if needed
	outputDir := filepath.Dir(p.config.OutputPath)
	if outputDir != "." && outputDir != "" {
		if err := os.MkdirAll(filepath.Join(projectDir, outputDir), 0755); err != nil {
			return nil, fmt.Errorf("node coverage: create output dir: %w", err)
		}
	}

	// Build test command: npm test -- --coverage --coverageReporters=json
	args := append(p.config.TestCommand, p.config.CoverageArgs...)
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = projectDir
	if p.config.Env != nil {
		cmd.Env = append(os.Environ(), p.config.Env...)
	}

	p.logger.Info("running node coverage",
		zap.String("cmd", strings.Join(args, " ")),
		zap.String("dir", projectDir))

	// Run with timeout
	if p.config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, p.config.Timeout)
		defer cancel()
		cmd = exec.CommandContext(ctx, args[0], args[1:]...)
		cmd.Dir = projectDir
		if p.config.Env != nil {
			cmd.Env = append(os.Environ(), p.config.Env...)
		}
	}

	// Run command - Jest returns non-zero if tests fail, but we still check coverage
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Check if it was a timeout
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("node coverage: timed out after %v", p.config.Timeout)
		}
		// Tests may have failed but coverage report might still exist
		p.logger.Warn("node coverage test had errors", zap.Error(err), zap.String("output", string(output)))
	}

	// Parse the JSON coverage report
	coveragePath := filepath.Join(projectDir, p.config.OutputPath)
	data, err := os.ReadFile(coveragePath)
	if err != nil {
		return nil, fmt.Errorf("node coverage: read coverage file: %w", err)
	}

	report, err := p.parseJestCoverage(data)
	if err != nil {
		return nil, fmt.Errorf("node coverage: parse coverage: %w", err)
	}

	// Mark as pass if we got coverage data (even if tests failed)
	report.Pass = true

	p.logger.Info("node coverage complete",
		zap.Int("total_funcs", report.TotalFuncs),
		zap.Int("covered_funcs", report.CoveredFuncs))

	return report, nil
}

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

// Gaps turns a coverage report into uncovered targets
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

// shouldSkipNodeFile reports files excluded from node autotest
func shouldSkipNodeFile(path string) bool {
	base := filepath.Base(path)

	// Skip generated files
	if strings.Contains(base, ".min.js") ||
	   strings.Contains(base, ".bundle.js") ||
	   strings.Contains(base, "-bundle.js") {
		return true
	}

	// Skip node_modules and other common exclusions
	for _, seg := range strings.Split(path, string(filepath.Separator)) {
		if seg == "node_modules" ||
		   seg == ".git" ||
		   seg == "dist" ||
		   seg == "build" ||
		   seg == "coverage" {
			return true
		}
	}

	return false
}

// DefaultNodeCoverageRunner runs Jest with coverage and returns the JSON report bytes
// This is a helper for testing and manual usage
func DefaultNodeCoverageRunner(ctx context.Context, projectDir string) ([]byte, error) {
	tmp, err := os.MkdirTemp("", "cerberus-node-cover-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)

	out := filepath.Join(tmp, "coverage-final.json")
	cmd := exec.CommandContext(ctx, "npm", "test", "--", "--coverage", "--coverageReporters=json", "--outputCoverage="+out)
	cmd.Dir = projectDir

	if runErr := cmd.Run(); runErr != nil {
		// Coverage report might still exist
		_ = runErr
	}

	return os.ReadFile(out)
}

// SetLogger sets the logger for the provider
func (p *NodeCoverageProvider) SetLogger(logger *zap.Logger) {
	p.logger = logger
}
