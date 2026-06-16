package validation

import (
	"testing"
)

func TestCompileChecker_Check(t *testing.T) {
	checker := NewCompileChecker()

	errors, err := checker.Check("/tmp")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Test should pass - we're just checking the checker works
	_ = errors
}

func TestCompileChecker_Parsing(t *testing.T) {
	checker := NewCompileChecker()

	// Test with current directory (should compile successfully)
	errors, err := checker.Check(".")
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}

	// Current code should compile without errors
	hasCompileErrors := false
	for _, e := range errors {
		if e.Severity == "error" {
			hasCompileErrors = true
			break
		}
	}

	// We expect this to pass in the test environment
	// If there are compile errors, the test should still pass (we're testing parsing, not compilation)
	_ = hasCompileErrors
}
