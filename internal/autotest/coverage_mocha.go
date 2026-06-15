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

// MochaCoverageProvider implements CoverageProvider for Mocha + nyc projects
type MochaCoverageProvider struct {
	config *CoverageConfig
	logger *zap.Logger
}

// NewMochaCoverageProvider creates a new Mocha coverage provider
func NewMochaCoverageProvider(cfg *CoverageConfig) *MochaCoverageProvider {
	return &MochaCoverageProvider{
		config: cfg,
		logger: zap.NewNop(),
	}
}

// RunCoverage runs nyc mocha with coverage and parses the Istanbul JSON output
func (p *MochaCoverageProvider) RunCoverage(ctx context.Context, projectDir string) (*CoverageReport, error) {
	if p.config == nil {
		return nil, fmt.Errorf("mocha coverage: config not set")
	}

	// Create output directory if needed
	outputDir := filepath.Dir(p.config.OutputPath)
	if outputDir != "." && outputDir != "" {
		if err := os.MkdirAll(filepath.Join(projectDir, outputDir), 0755); err != nil {
			return nil, fmt.Errorf("mocha coverage: create output dir: %w", err)
		}
	}

	// Build test command: npm test -- --coverage --coverage-reporter=json
	args := append(p.config.TestCommand, p.config.CoverageArgs...)
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = projectDir
	if p.config.Env != nil {
		cmd.Env = append(os.Environ(), p.config.Env...)
	}

	p.logger.Info("running mocha coverage",
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

	// Run command - tests may fail but coverage report might still exist
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Check if it was a timeout
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("mocha coverage: timed out after %v", p.config.Timeout)
		}
		// Tests may have failed but coverage report might still exist
		p.logger.Warn("mocha tests failed but coverage might exist", zap.Error(err), zap.String("output", string(output)))
	}

	// Parse the JSON coverage report
	coveragePath := filepath.Join(projectDir, p.config.OutputPath)
	data, err := os.ReadFile(coveragePath)
	if err != nil {
		return nil, fmt.Errorf("mocha coverage: read coverage file: %w", err)
	}

	report, err := p.parseIstanbulCoverage(data)
	if err != nil {
		return nil, fmt.Errorf("mocha coverage: parse coverage: %w", err)
	}

	// Mark as pass if we got coverage data (even if tests failed)
	report.Pass = true

	p.logger.Info("mocha coverage complete",
		zap.Int("total_funcs", report.TotalFuncs),
		zap.Int("covered_funcs", report.CoveredFuncs))

	return report, nil
}

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

// SetLogger sets the logger for the provider
func (p *MochaCoverageProvider) SetLogger(logger *zap.Logger) {
	p.logger = logger
}
