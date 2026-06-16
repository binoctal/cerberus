package ai

import (
	"testing"

	"github.com/binoctal/cerberus/pkg/business"
)

func TestAITestGenerator_GenerateTestSuite(t *testing.T) {
	generator := NewAITestGenerator(nil, nil)

	suite, err := generator.GenerateTestSuite("CalculateDiscount")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if suite == nil {
		t.Error("Expected test suite to be returned, got nil")
	}
}

func TestAITestGenerator_FullFlow(t *testing.T) {
	generator := NewAITestGenerator(&business.BusinessModel{
		Rules: []business.BusinessRule{
			{Name: "VIP discount", Confidence: 0.9},
		},
	}, nil)

	suite, err := generator.GenerateTestSuite("CalculateDiscount")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Verify suite structure
	if suite.Function != "CalculateDiscount" {
		t.Errorf("Expected function 'CalculateDiscount', got '%s'", suite.Function)
	}

	if len(suite.Scenarios) == 0 {
		t.Error("Expected at least one scenario, got none")
	}

	if len(suite.Tests) != len(suite.Scenarios) {
		t.Errorf("Expected %d tests (one per scenario), got %d", len(suite.Scenarios), len(suite.Tests))
	}
}
