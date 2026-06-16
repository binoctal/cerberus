package validation

import (
	"testing"
)

func TestValidator_ValidateAIResults(t *testing.T) {
	validator := NewValidator()

	results := &AIResults{
		ProjectPath: ".",
	}

	report, err := validator.ValidateAIResults(results)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if report == nil {
		t.Error("Expected report to be returned, got nil")
	}
}

func TestValidator_CompleteFlow(t *testing.T) {
	validator := NewValidator()

	results := &AIResults{
		ProjectPath:    ".",
		GeneratedTests: []string{"test_1.go", "test_2.go"},
	}

	report, err := validator.ValidateAIResults(results)
	if err != nil {
		t.Fatalf("Validation failed: %v", err)
	}

	// Verify all validation steps were performed
	if report == nil {
		t.Error("Expected report to be returned, got nil")
	}

	// Verify comparison was performed
	if report.Comparison == nil {
		t.Error("Expected comparison result, got nil")
	}
}
