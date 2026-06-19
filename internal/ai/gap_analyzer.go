package ai

import (
	"sort"

	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/pkg/business"
)

// GapAnalyzer identifies coverage gaps using AI
type GapAnalyzer struct {
	llmClient     llm.Client
	businessModel *business.BusinessModel
}

// CoverageReport represents test coverage results
type CoverageReport struct {
	TotalCoverage    float64
	CoveredFiles     []string
	UncoveredFiles   []string
	CoveredLines     int
	TotalLines       int
	FunctionCoverage map[string]float64
	LineCoverage     map[string]float64
	BranchCoverage   map[string]float64
}

// CoverageGap represents a gap in test coverage
type CoverageGap struct {
	Type        string // "rule_combination" | "edge_case" | "error_path" | "hidden"
	Description string
	Reason      string
	Difficulty  string // "simple" | "medium" | "complex"
	Priority    int
}

// NewGapAnalyzer creates a new gap analyzer
func NewGapAnalyzer(llmClient llm.Client, businessModel *business.BusinessModel) *GapAnalyzer {
	return &GapAnalyzer{
		llmClient:     llmClient,
		businessModel: businessModel,
	}
}

// IdentifyGaps identifies coverage gaps in the test suite
func (a *GapAnalyzer) IdentifyGaps(report *CoverageReport, model *business.BusinessModel) []CoverageGap {
	gaps := []CoverageGap{}

	// Check for rule combination gaps
	if !a.coversRuleCombinations(report, model) {
		gaps = append(gaps, CoverageGap{
			Type:        "rule_combination",
			Description: "Some business rule combinations are not tested",
			Reason:      "AI analysis found untested rule combinations",
			Difficulty:  "medium",
			Priority:    2,
		})
	}

	// Check for edge case gaps
	if !a.coversEdgeCases(report, model) {
		gaps = append(gaps, CoverageGap{
			Type:        "edge_case",
			Description: "Some edge cases are not tested",
			Reason:      "Boundary conditions not fully covered",
			Difficulty:  "simple",
			Priority:    1,
		})
	}

	// Check for error path gaps
	if !a.coversErrorPaths(report, model) {
		gaps = append(gaps, CoverageGap{
			Type:        "error_path",
			Description: "Some error handling paths are not tested",
			Reason:      "Error scenarios not fully covered",
			Difficulty:  "medium",
			Priority:    3,
		})
	}

	// Check for hidden gaps (deep logic paths)
	if !a.coversHiddenPaths(report, model) {
		gaps = append(gaps, CoverageGap{
			Type:        "hidden",
			Description: "Some hidden code paths are not tested",
			Reason:      "Complex conditional logic not fully covered",
			Difficulty:  "complex",
			Priority:    4,
		})
	}

	// Sort by priority (lower number = higher priority)
	sort.Slice(gaps, func(i, j int) bool {
		return gaps[i].Priority < gaps[j].Priority
	})

	return gaps
}

// coversRuleCombinations checks if rule combinations are covered
func (a *GapAnalyzer) coversRuleCombinations(report *CoverageReport, model *business.BusinessModel) bool {
	// Stub implementation - will be enhanced with LLM in later tasks
	// For now, assume combinations are not covered if coverage < 80%
	if report.TotalCoverage < 0.8 {
		return false
	}

	// If we have rules but low branch coverage, combinations likely not covered
	avgBranchCoverage := a.calculateAverageCoverage(report.BranchCoverage)
	if len(model.Rules) > 1 && avgBranchCoverage < 0.7 {
		return false
	}

	return true
}

// coversEdgeCases checks if edge cases are covered
func (a *GapAnalyzer) coversEdgeCases(report *CoverageReport, model *business.BusinessModel) bool {
	// Stub implementation - will be enhanced with LLM in later tasks
	// For now, check if we have edge cases defined and coverage is sufficient
	if len(model.EdgeCases) == 0 {
		return true // No edge cases to cover
	}

	// Check coverage against edge case count
	avgLineCoverage := a.calculateAverageCoverage(report.LineCoverage)
	return avgLineCoverage >= 0.85
}

// coversErrorPaths checks if error handling paths are covered
func (a *GapAnalyzer) coversErrorPaths(report *CoverageReport, model *business.BusinessModel) bool {
	// Stub implementation - will be enhanced with LLM in later tasks
	// For now, check if we have error scenarios defined
	if len(model.ErrorScenarios) == 0 {
		return true // No error scenarios to cover
	}

	// Check branch coverage (error paths usually in branches)
	avgBranchCoverage := a.calculateAverageCoverage(report.BranchCoverage)
	return avgBranchCoverage >= 0.75
}

// coversHiddenPaths checks if hidden/complex paths are covered
func (a *GapAnalyzer) coversHiddenPaths(report *CoverageReport, model *business.BusinessModel) bool {
	// Stub implementation - will be enhanced with LLM in later tasks
	// Hidden paths are complex conditional logic paths
	// Check for high cyclomatic complexity indicators

	// If overall coverage is very high, hidden paths likely covered
	if report.TotalCoverage >= 0.95 {
		return true
	}

	// Check line coverage (hidden paths often in rarely-executed lines)
	avgLineCoverage := a.calculateAverageCoverage(report.LineCoverage)
	return avgLineCoverage >= 0.90
}

// calculateAverageCoverage calculates average coverage from a map
func (a *GapAnalyzer) calculateAverageCoverage(coverageMap map[string]float64) float64 {
	if len(coverageMap) == 0 {
		return 0.0
	}

	sum := 0.0
	for _, coverage := range coverageMap {
		sum += coverage
	}

	return sum / float64(len(coverageMap))
}
