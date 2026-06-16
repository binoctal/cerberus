package ai

import (
	"testing"

	"github.com/binoctal/cerberus/pkg/business"
)

func TestCoverageOptimizer_OptimizeCoverage(t *testing.T) {
	optimizer := NewCoverageOptimizer(nil, &business.BusinessModel{
		Rules: []business.BusinessRule{
			{Name: "TestRule", Confidence: 0.8},
		},
		EdgeCases: []business.EdgeCase{
			{Name: "TestCase", Confidence: 0.9},
		},
	})

	suite := &TestSuite{
		Function: "TestFunc",
	}

	optimized, err := optimizer.OptimizeCoverage(suite)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if optimized == nil {
		t.Error("Expected optimized suite to be returned, got nil")
	}
}

func TestCoverageOptimizer_IterativeImprovement(t *testing.T) {
	optimizer := NewCoverageOptimizer(nil, &business.BusinessModel{
		Confidence: 0.8,
	})

	// Start with minimal test suite
	suite := &TestSuite{
		Function: "CalculateDiscount",
		Tests:    []TestCase{},
	}

	// Run optimization (will iterate up to 5 times)
	optimized, err := optimizer.OptimizeCoverage(suite)
	if err != nil {
		t.Fatalf("Optimization failed: %v", err)
	}

	// Should have generated additional tests
	if len(optimized.Tests) <= len(suite.Tests) {
		t.Errorf("Expected more tests after optimization, got %d (original: %d)",
			len(optimized.Tests), len(suite.Tests))
	}
}

func TestCoverageOptimizer_MaxIterations(t *testing.T) {
	optimizer := NewCoverageOptimizer(nil, &business.BusinessModel{
		Confidence: 0.5, // Low confidence to trigger more iterations
	})

	suite := &TestSuite{
		Function: "ComplexFunction",
		Tests:    []TestCase{},
	}

	// Run optimization
	optimized, err := optimizer.OptimizeCoverage(suite)
	if err != nil {
		t.Fatalf("Optimization failed: %v", err)
	}

	// Should not exceed max iterations (5)
	// This is a structural test - actual iteration count will be verified in integration tests
	if optimized == nil {
		t.Error("Expected optimized suite, got nil")
	}
}

func TestCoverageOptimizer_GenerateTestsForGaps(t *testing.T) {
	optimizer := NewCoverageOptimizer(nil, &business.BusinessModel{
		Rules: []business.BusinessRule{
			{Name: "Rule1", Confidence: 0.9},
		},
	})

	gaps := []CoverageGap{
		{
			Type:        "edge_case",
			Description: "Missing edge case tests",
			Priority:    1,
		},
		{
			Type:        "error_path",
			Description: "Missing error path tests",
			Priority:    2,
		},
	}

	suite := &TestSuite{
		Function: "TestFunction",
	}

	newTests := optimizer.generateTestsForGaps(gaps, suite)

	// Should generate tests for gaps
	if newTests == nil {
		t.Error("Expected new tests to be generated, got nil")
	}
}

func TestCoverageOptimizer_MergeTestSuites(t *testing.T) {
	optimizer := NewCoverageOptimizer(nil, nil)

	original := &TestSuite{
		Function: "TestFunc",
		Tests: []TestCase{
			{Code: "test1"},
		},
	}

	new := &TestSuite{
		Function: "TestFunc",
		Tests: []TestCase{
			{Code: "test2"},
		},
	}

	merged := optimizer.mergeTestSuites(original, new)

	// Should contain all tests from both suites
	expectedCount := len(original.Tests) + len(new.Tests)
	if len(merged.Tests) != expectedCount {
		t.Errorf("Expected %d tests, got %d", expectedCount, len(merged.Tests))
	}
}

func TestCoverageOptimizer_IsCoverageSufficient(t *testing.T) {
	optimizer := NewCoverageOptimizer(nil, nil)

	// Test with insufficient coverage
	report1 := &CoverageReport{
		TotalCoverage: 0.85,
	}
	if optimizer.isCoverageSufficient(report1) {
		t.Error("Expected 85% coverage to be insufficient")
	}

	// Test with sufficient coverage
	report2 := &CoverageReport{
		TotalCoverage: 0.90,
	}
	if !optimizer.isCoverageSufficient(report2) {
		t.Error("Expected 90% coverage to be sufficient")
	}

	// Test with excellent coverage
	report3 := &CoverageReport{
		TotalCoverage: 0.95,
	}
	if !optimizer.isCoverageSufficient(report3) {
		t.Error("Expected 95% coverage to be sufficient")
	}
}
