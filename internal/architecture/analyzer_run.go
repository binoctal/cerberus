package architecture

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Analyze performs comprehensive architecture analysis
func (a *Analyzer) Analyze() (*ArchitectureReport, error) {
	report := &ArchitectureReport{
		ProjectPath: a.projectPath,
		AnalyzedAt:  time.Now(),
		Issues:      []ArchitectureIssue{},
		Metrics:     &ArchitectureMetrics{},
		Summary: &ReportSummary{
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
