package ai

import (
	"testing"

	"github.com/binoctal/cerberus/pkg/business"
)

func TestScenarioGenerator_GenerateScenarios(t *testing.T) {
	generator := NewScenarioGenerator(nil, nil)

	scenarios := generator.GenerateScenarios("TestFunc", []business.BusinessRule{})
	if scenarios == nil {
		t.Error("Expected scenarios to be returned, got nil")
	}
}

func TestScenarioGenerator_GenerateAllTypes(t *testing.T) {
	generator := NewScenarioGenerator(nil, nil)

	scenarios := generator.GenerateScenarios("CalculateDiscount", []business.BusinessRule{
		{Name: "VIP discount", Condition: "isVIP == true", Effect: "discount += 0.1"},
	})

	// Should generate scenarios for all types
	foundTypes := make(map[string]bool)
	for _, s := range scenarios {
		foundTypes[s.Type] = true
	}

	expectedTypes := []string{"normal", "edge", "error", "combination"}
	for _, expectedType := range expectedTypes {
		if !foundTypes[expectedType] {
			t.Errorf("Expected scenario type '%s' not found", expectedType)
		}
	}
}
