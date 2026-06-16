package architecture

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Analyzer performs architecture analysis
type Analyzer struct {
	projectPath string
	maxLinesFile string
}

// NewAnalyzer creates a new architecture analyzer
func NewAnalyzer(projectPath string) *Analyzer {
	return &Analyzer{
		projectPath: projectPath,
	}
}

// Analyze performs comprehensive architecture analysis
func (a *Analyzer) Analyze() (*ArchitectureReport, error) {
	report := &ArchitectureReport{
		ProjectPath: a.projectPath,
		AnalyzedAt:  time.Now(),
		Issues:      []ArchitectureIssue{},
		Metrics:     &ArchitectureMetrics{},
		Summary:     &ReportSummary{
			CategoryScores: make(map[string]int),
		},
	}
	
	// 1. Analyze complexity
	if err := a.analyzeComplexity(report); err != nil {
		return nil, fmt.Errorf("complexity analysis failed: %w", err)
	}
	
	// 2. Analyze dependencies
	if err := a.analyzeDependencies(report); err != nil {
		return nil, fmt.Errorf("dependency analysis failed: %w", err)
	}
	
	// 3. Analyze abstractions
	if err := a.analyzeAbstractions(report); err != nil {
		return nil, fmt.Errorf("abstraction analysis failed: %w", err)
	}
	
	// 4. Analyze SOLID principles
	if err := a.analyzeSOLID(report); err != nil {
		return nil, fmt.Errorf("SOLID analysis failed: %w", err)
	}
	
	// 5. Analyze scenarios
	if err := a.analyzeScenarios(report); err != nil {
		return nil, fmt.Errorf("scenario analysis failed: %w", err)
	}
	
	// Calculate summary statistics
	report.calculateSummary()
	
	// Calculate health score
	report.CalculateHealthScore()
	
	return report, nil
}

// analyzeComplexity analyzes code complexity
func (a *Analyzer) analyzeComplexity(report *ArchitectureReport) error {
	// Walk through Go files
	return filepath.Walk(a.projectPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		
		// Skip directories and non-Go files
		if info.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		
		// Skip test files and generated files
		if strings.Contains(path, "_test.go") || strings.Contains(path, "vendor/") {
			return nil
		}
		
		// Analyze file complexity
		fileIssues, metrics, err := a.analyzeFileComplexity(path, report)
		if err != nil {
			// Log but continue with other files
			fmt.Printf("Warning: failed to analyze %s: %v\n", path, err)
			return nil
		}

		report.Issues = append(report.Issues, fileIssues...)
		report.Metrics.TotalFiles++
		report.Metrics.TotalLines += metrics.Lines
		report.Metrics.TotalFunctions += metrics.Functions

		// Analyze function complexity (parameters, nesting, cyclomatic complexity)
		funcIssues, err := a.analyzeFunctionComplexity(path, report)
		if err != nil {
			fmt.Printf("Warning: failed to analyze functions in %s: %v\n", path, err)
		} else {
			report.Issues = append(report.Issues, funcIssues...)
		}

		return nil
	})
}

// FileComplexityMetrics represents metrics for a single file
type FileComplexityMetrics struct {
	Lines      int
	Functions  int
	MaxParams  int
	MaxDepth   int
}

// analyzeFileComplexity analyzes complexity of a single file
func (a *Analyzer) analyzeFileComplexity(filePath string, report *ArchitectureReport) ([]ArchitectureIssue, *FileComplexityMetrics, error) {
	issues := []ArchitectureIssue{}
	metrics := &FileComplexityMetrics{}
	
	// Read file content
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, nil, err
	}
	
	lines := strings.Split(string(content), "\n")
	metrics.Lines = len(lines)
	
	// Count code lines (excluding comments and blank lines)
	codeLines := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !strings.HasPrefix(trimmed, "//") && !strings.HasPrefix(trimmed, "/*") {
			codeLines++
		}
	}
	
	// Check: File too long
	if codeLines > 150 {
		relPath, _ := filepath.Rel(a.projectPath, filePath)
		issues = append(issues, ArchitectureIssue{
			ID:          fmt.Sprintf("file-too-long-%s", strings.ReplaceAll(relPath, "/", "-")),
			Type:        OverEngineering,
			Severity:    SeverityWarning,
			File:        relPath,
			Line:        0,
			Description: fmt.Sprintf("文件有 %d 行代码，超过阈值 150 行", codeLines),
			Rationale:   "长文件通常包含过多职责，难以维护和测试",
			Suggestion:  "考虑拆分为多个文件，每个文件单一职责",
			Confidence:  1.0,
			Evidence:    []string{fmt.Sprintf("实际行数: %d", codeLines)},
		})
		
		// Track max
		if codeLines > report.Metrics.MaxLinesPerFile {
			report.Metrics.MaxLinesPerFile = codeLines
			a.maxLinesFile = relPath
		}
	}
	
	// Count functions
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "func ") {
			metrics.Functions++
		}
	}
	
	metrics.Lines = codeLines
	
	return issues, metrics, nil
}

// calculateSummary calculates summary statistics
func (r *ArchitectureReport) calculateSummary() {
	if r.Summary == nil {
		r.Summary = &ReportSummary{
			CategoryScores: make(map[string]int),
		}
	}
	
	r.Summary.TotalIssues = len(r.Issues)
	
	for _, issue := range r.Issues {
		switch issue.Severity {
		case SeverityError:
			r.Summary.ErrorCount++
		case SeverityWarning:
			r.Summary.WarningCount++
		case SeverityInfo:
			r.Summary.InfoCount++
		}
	}
}
