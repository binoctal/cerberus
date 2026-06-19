package validation

import (
	"fmt"

	"github.com/binoctal/cerberus/internal/architecture"
)

// ArchitectureValidator performs architecture quality checks
type ArchitectureValidator struct {
	projectPath string
}

// NewArchitectureValidator creates a new architecture validator
func NewArchitectureValidator(projectPath string) *ArchitectureValidator {
	return &ArchitectureValidator{
		projectPath: projectPath,
	}
}

// Validate performs architecture validation
func (v *ArchitectureValidator) Validate() (*ValidationResult, error) {
	// Create architecture analyzer
	analyzer := architecture.NewAnalyzer(v.projectPath)

	// Run analysis
	report, err := analyzer.Analyze()
	if err != nil {
		return nil, fmt.Errorf("architecture analysis failed: %w", err)
	}

	// Convert architecture report to validation result
	result := &ValidationResult{
		Passed:      len(report.Issues) == 0,
		Description: fmt.Sprintf("架构质量检查: 发现 %d 个问题", len(report.Issues)),
		Details:     []string{},
	}

	// Add issues as details
	for _, issue := range report.Issues {
		detail := fmt.Sprintf("[%s] %s (%s:%d) - %s",
			issue.Severity,
			issue.Type,
			issue.File,
			issue.Line,
			issue.Description,
		)
		result.Details = append(result.Details, detail)

		// Mark as failed if there are errors
		if issue.Severity == architecture.SeverityError {
			result.Passed = false
		}
	}

	// Add health score
	if report.Summary != nil {
		result.Details = append(result.Details,
			fmt.Sprintf("架构健康度: %d/100", report.Summary.HealthScore),
		)

		// Add category scores
		for category, score := range report.Summary.CategoryScores {
			result.Details = append(result.Details,
				fmt.Sprintf("  %s: %d/100", category, score),
			)
		}
	}

	return result, nil
}
