package autotest

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"go.uber.org/zap"
	_ "modernc.org/sqlite"
)

// PythonCoverageProvider implements CoverageProvider for Python pytest+coverage.py projects
type PythonCoverageProvider struct {
	config *CoverageConfig
	logger *zap.Logger
}

// NewPythonCoverageProvider creates a new Python coverage provider
func NewPythonCoverageProvider(cfg *CoverageConfig) *PythonCoverageProvider {
	return &PythonCoverageProvider{
		config: cfg,
		logger: zap.NewNop(),
	}
}

// CoverageJSON represents the coverage.py JSON output format
type CoverageJSON struct {
	Files map[string]*PythonFileCoverage `json:"files"`
	Meta *PythonCoverageMeta              `json:"meta"`
}

// PythonFileCoverage represents coverage data for a single Python file
type PythonFileCoverage struct {
	Summary      *PythonCoverageSummary  `json:"summary"`
	Lines        map[string]int           `json:"lines"`
	Functions    map[string]*PythonFuncCoverage `json:"functions"`
}

// PythonCoverageSummary contains summary statistics
type PythonCoverageSummary struct {
	NumStatements int `json:"num_statements"`
	CoveredLines   int `json:"covered_lines"`
	PercentCovered float64 `json:"percent_covered"`
	MissingLines   string `json:"missing_lines"`
}

// PythonFuncCoverage represents function-level coverage
type PythonFuncCoverage struct {
	ExecutedCount int      `json:"executed_count"`
	MissingLines  []string `json:"missing_lines"`
}

// PythonCoverageMeta contains metadata
type PythonCoverageMeta struct {
	BranchCoverage bool    `json:"branch_coverage"`
	Timestamp      string  `json:"timestamp"`
}

// RunCoverage runs pytest with coverage and parses the output
func (p *PythonCoverageProvider) RunCoverage(ctx context.Context, projectDir string) (*CoverageReport, error) {
	if p.config == nil {
		return nil, fmt.Errorf("python coverage: config not set")
	}

	// Determine Python command
	pythonCmd := "python3"
	if p.config.Env != nil && len(p.config.Env) > 0 {
		// Check if PYTHON_CMD is set in env
		for _, env := range p.config.Env {
			if strings.HasPrefix(env, "PYTHON_CMD=") {
				pythonCmd = strings.TrimPrefix(env, "PYTHON_CMD=")
				break
			}
		}
	}

	// Run coverage.py + pytest
	args := append([]string{pythonCmd}, p.config.TestCommand...)
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = projectDir
	if p.config.Env != nil {
		cmd.Env = append(os.Environ(), p.config.Env...)
	}

	p.logger.Info("running python coverage",
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

	// Run command
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Check if it was a timeout
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("python coverage: timed out after %v", p.config.Timeout)
		}
		// Tests may have failed but coverage report might still exist
		p.logger.Warn("python coverage test had errors", zap.Error(err), zap.String("output", string(output)))
	}

	// Generate JSON report
	if p.config.CoverageArgs != nil && len(p.config.CoverageArgs) > 0 {
		reportArgs := append([]string{pythonCmd}, p.config.CoverageArgs...)
		reportCmd := exec.Command(reportArgs[0], reportArgs[1:]...)
		reportCmd.Dir = projectDir
		if runErr := reportCmd.Run(); runErr != nil {
			p.logger.Warn("python coverage report generation failed", zap.Error(runErr))
		}
	}

	// Parse the JSON coverage report
	coveragePath := filepath.Join(projectDir, p.config.OutputPath)
	data, err := os.ReadFile(coveragePath)
	if err != nil {
		// Try SQLite database as fallback
		return p.parseSQLiteCoverage(projectDir)
	}

	report, err := p.parseJSONCoverage(data)
	if err != nil {
		// Try SQLite as fallback
		return p.parseSQLiteCoverage(projectDir)
	}

	// Mark as pass if we got coverage data
	report.Pass = true

	p.logger.Info("python coverage complete",
		zap.Int("total_funcs", report.TotalFuncs),
		zap.Int("covered_funcs", report.CoveredFuncs))

	return report, nil
}

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
		if fileData.Lines == nil {
			continue
		}

		// Process lines
		for lineStr, count := range fileData.Lines {
			lineNum := 0
			if _, err := fmt.Sscanf(lineStr, "%d", &lineNum); err == nil {
				report.Profile = append(report.Profile, CoverageLine{
					File:  file,
					Start: lineNum,
					End:    lineNum + 1,
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

// parseSQLiteCoverage parses coverage.py SQLite database directly
func (p *PythonCoverageProvider) parseSQLiteCoverage(projectDir string) (*CoverageReport, error) {
	dbPath := filepath.Join(projectDir, p.config.DatabasePath)
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("python coverage: database not found: %s", dbPath)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("python coverage: open database: %w", err)
	}
	defer db.Close()

	report := &CoverageReport{
		Profile: make([]CoverageLine, 0),
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
	defer rows.Close()

	for rows.Next() {
		var filePath string
		var lineNum, count int

		if err := rows.Scan(&filePath, &lineNum, &count); err != nil {
			continue
		}

		report.Profile = append(report.Profile, CoverageLine{
			File:  filePath,
			Start: lineNum,
			End:    lineNum + 1,
			Count:  count,
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

// Gaps turns a coverage report into uncovered targets
func (p *PythonCoverageProvider) Gaps(report *CoverageReport) []CoverageGap {
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

// NoTestFileGaps walks projectDir for *.py source files that have no sibling test_*.py
func (p *PythonCoverageProvider) NoTestFileGaps(projectDir string) []CoverageGap {
	var gaps []CoverageGap

	_ = filepath.Walk(projectDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}

		// Skip non-Python files
		if !strings.HasSuffix(path, ".py") {
			return nil
		}

		// Skip test files themselves
		if strings.HasPrefix(filepath.Base(path), "test_") ||
		   strings.HasSuffix(path, "_test.py") {
			return nil
		}

		// Skip __pycache__ and other exclusions
		if shouldSkipPythonFile(path) {
			return nil
		}

		// Check for test file in tests/ directory first
		projectRoot := findProjectRoot(projectDir)
		relPath, _ := filepath.Rel(projectRoot, path)

		// Try tests/ directory
		testsDir := filepath.Join(projectRoot, "tests")
		if _, err := os.Stat(testsDir); err == nil {
			testPath := filepath.Join(testsDir, "test_"+filepath.Base(path))
			if _, statErr := os.Stat(testPath); os.IsNotExist(statErr) {
				// Try with subdirectory structure
				subTestPath := filepath.Join(testsDir, filepath.Dir(relPath), "test_"+filepath.Base(path))
				if _, subErr := os.Stat(subTestPath); os.IsNotExist(subErr) {
					gaps = append(gaps, CoverageGap{
						File:   path,
						Reason: ReasonNoTestFile,
					})
				}
			}
		} else {
			// No tests/ directory, check same directory
			testFile := filepath.Join(filepath.Dir(path), "test_"+filepath.Base(path))
			if _, statErr := os.Stat(testFile); os.IsNotExist(statErr) {
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

// shouldSkipPythonFile reports files excluded from python autotest
func shouldSkipPythonFile(path string) bool {
	base := filepath.Base(path)

	// Skip __pycache__, .pyc files
	if strings.HasSuffix(base, ".pyc") ||
	   base == "__init__.py" ||
	   strings.Contains(path, "__pycache__") {
		return true
	}

	// Skip common exclusion directories
	for _, seg := range strings.Split(path, string(filepath.Separator)) {
		if seg == ".git" ||
		   seg == "venv" ||
		   seg == ".venv" ||
		   seg == "env" ||
		   seg == "dist" ||
		   seg == "build" ||
		   seg == ".pytest_cache" {
			return true
		}
	}

	return false
}

// findProjectRoot attempts to find the project root directory
func findProjectRoot(startDir string) string {
	// Look for common project markers
	markers := []string{
		"requirements.txt",
		"setup.py",
		"pyproject.toml",
		".git",
	}

	current := startDir
	for {
		for _, marker := range markers {
			if _, err := os.Stat(filepath.Join(current, marker)); err == nil {
				return current
			}
		}

		parent := filepath.Dir(current)
		if parent == current {
			break // Reached root
		}
		current = parent
	}

	return startDir
}

// DefaultPythonCoverageRunner runs pytest with coverage and returns the JSON bytes
func DefaultPythonCoverageRunner(ctx context.Context, projectDir string) ([]byte, error) {
	tmp, err := os.MkdirTemp("", "cerberus-python-cover-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)

	out := filepath.Join(tmp, "coverage.json")
	cmd := exec.CommandContext(ctx, "coverage", "run", "-m", "pytest", "--cov-report=json:"+out)
	cmd.Dir = projectDir

	if runErr := cmd.Run(); runErr != nil {
		_ = runErr // Coverage report might still exist
	}

	return os.ReadFile(out)
}

// SetLogger sets the logger for the provider
func (p *PythonCoverageProvider) SetLogger(logger *zap.Logger) {
	p.logger = logger
}
