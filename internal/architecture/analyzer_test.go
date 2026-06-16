package architecture

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAnalyzer_Analyze(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir := t.TempDir()
	
	// Create a test Go file with 200 lines (over threshold)
	testFile := filepath.Join(tmpDir, "complex.go")
	content := ""
	for i := 0; i < 200; i++ {
		content += "// Comment line\n"
		content += "package test\n\n"
	}
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	
	// Create analyzer
	analyzer := NewAnalyzer(tmpDir)
	
	// Run analysis
	report, err := analyzer.Analyze()
	if err != nil {
		t.Fatalf("Analysis failed: %v", err)
	}
	
	// Verify report structure
	if report == nil {
		t.Fatal("Expected report to be returned, got nil")
	}
	
	if report.ProjectPath != tmpDir {
		t.Errorf("Expected project path %s, got %s", tmpDir, report.ProjectPath)
	}
	
	if report.Metrics == nil {
		t.Error("Expected metrics to be initialized, got nil")
	}
	
	if report.Summary == nil {
		t.Error("Expected summary to be initialized, got nil")
	}
	
	// Verify at least one file was analyzed
	if report.Metrics.TotalFiles == 0 {
		t.Error("Expected at least one file to be analyzed")
	}
	
	// Verify health score is calculated
	if report.Summary.HealthScore < 0 || report.Summary.HealthScore > 100 {
		t.Errorf("Expected health score between 0 and 100, got %d", report.Summary.HealthScore)
	}
}

func TestAnalyzer_Analyze_EmptyProject(t *testing.T) {
	// Test with empty directory
	tmpDir := t.TempDir()
	
	analyzer := NewAnalyzer(tmpDir)
	
	report, err := analyzer.Analyze()
	if err != nil {
		t.Fatalf("Analysis failed: %v", err)
	}
	
	// Should succeed with no issues
	if report.Metrics.TotalFiles != 0 {
		t.Errorf("Expected 0 files in empty directory, got %d", report.Metrics.TotalFiles)
	}
	
	if len(report.Issues) != 0 {
		t.Errorf("Expected 0 issues in empty directory, got %d", len(report.Issues))
	}
	
	// Health score should be 100 for empty project
	if report.Summary.HealthScore != 100 {
		t.Errorf("Expected health score 100 for empty project, got %d", report.Summary.HealthScore)
	}
}

func TestAnalyzer_DetectsLongFiles(t *testing.T) {
	tmpDir := t.TempDir()
	
	// Create a file with >150 code lines
	longFile := filepath.Join(tmpDir, "long.go")
	content := "package test\n\n"
	for i := 0; i < 160; i++ {
		content += "func testFunction" + string(rune('A'+i%26)) + "() {}\n"
	}
	
	if err := os.WriteFile(longFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	
	analyzer := NewAnalyzer(tmpDir)
	report, err := analyzer.Analyze()
	if err != nil {
		t.Fatalf("Analysis failed: %v", err)
	}
	
	// Should detect over-engineering issue
	found := false
	for _, issue := range report.Issues {
		if issue.Type == OverEngineering && issue.File == "long.go" {
			found = true
			if issue.Severity != SeverityWarning {
				t.Errorf("Expected warning severity, got %s", issue.Severity)
			}
			break
		}
	}
	
	if !found {
		t.Error("Expected to detect over-engineering issue for long file")
	}
}

func TestReport_CalculateHealthScore(t *testing.T) {
	report := &ArchitectureReport{
		Issues: []ArchitectureIssue{
			{
				Type:     OverEngineering,
				Severity: SeverityWarning,
			},
			{
				Type:     CircularDependency,
				Severity: SeverityError,
			},
		},
		Metrics: &ArchitectureMetrics{},
		Summary: &ReportSummary{
			CategoryScores: make(map[string]int),
		},
	}
	
	// Calculate health score
	score := report.CalculateHealthScore()
	
	// Score should be between 0 and 100
	if score < 0 || score > 100 {
		t.Errorf("Expected health score between 0 and 100, got %d", score)
	}
	
	// Should have deducted points for issues
	if score == 100 {
		t.Error("Expected health score < 100 for project with issues")
	}
}

func TestReport_CalculateCategoryScores(t *testing.T) {
	report := &ArchitectureReport{
		Issues: []ArchitectureIssue{
			{Type: OverEngineering, Severity: SeverityWarning},
			{Type: PrematureAbstraction, Severity: SeverityInfo},
		},
		Summary: &ReportSummary{
			CategoryScores: make(map[string]int),
		},
	}
	
	report.CalculateCategoryScores()
	
	// Should have calculated category scores
	if len(report.Summary.CategoryScores) == 0 {
		t.Error("Expected category scores to be calculated")
	}
	
	// Verify specific categories exist
	for _, category := range []string{"complexity", "simplicity", "maintainability"} {
		if _, exists := report.Summary.CategoryScores[category]; !exists {
			t.Errorf("Expected category %s to have a score", category)
		}
	}
}
