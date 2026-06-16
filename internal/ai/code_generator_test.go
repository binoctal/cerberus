package ai

import (
	"testing"
)

func TestCodeGenerator_GenerateTestCode(t *testing.T) {
	generator := NewCodeGenerator(nil)

	scenario := &Scenario{
		Name:        "TestVIPDiscount",
		Description: "VIP user gets 10% discount",
		Type:        "normal",
	}

	funcInfo := &FuncInfo{
		Name:     "CalculateDiscount",
		Language: "go",
		Package:  "service",
	}

	code, err := generator.GenerateTestCode(scenario, funcInfo)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if code == "" {
		t.Error("Expected test code to be generated, got empty string")
	}
}

func TestCodeGenerator_MultiLanguage(t *testing.T) {
	generator := NewCodeGenerator(nil)

	scenario := &Scenario{
		Name: "TestDiscount",
		Type: "normal",
	}

	// Test Go code generation
	goCode, err := generator.GenerateTestCode(scenario, &FuncInfo{
		Name:     "CalculateDiscount",
		Language: "go",
		Package:  "service",
	})
	if err != nil {
		t.Fatalf("Go generation failed: %v", err)
	}
	if !containsString(goCode, "func TestDiscount") {
		t.Error("Go code missing function declaration")
	}

	// Test Python code generation
	pyCode, err := generator.GenerateTestCode(scenario, &FuncInfo{
		Name:     "calculate_discount",
		Language: "python",
		Package:  "service",
	})
	if err != nil {
		t.Fatalf("Python generation failed: %v", err)
	}
	if !containsString(pyCode, "def test_") {
		t.Error("Python code missing function declaration")
	}

	// Test JavaScript code generation
	jsCode, err := generator.GenerateTestCode(scenario, &FuncInfo{
		Name:     "calculateDiscount",
		Language: "javascript",
		Package:  "service",
	})
	if err != nil {
		t.Fatalf("JavaScript generation failed: %v", err)
	}
	if !containsString(jsCode, "test(") {
		t.Error("JavaScript code missing test declaration")
	}
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && indexOfString(s, substr) >= 0)
}

func indexOfString(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
