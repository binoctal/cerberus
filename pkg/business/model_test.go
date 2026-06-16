package business

import (
	"math"
	"testing"
)

func TestBusinessModel_Validation(t *testing.T) {
	model := &BusinessModel{
		ID:          "test-001",
		ProjectPath: "/test/project",
		Domain:      "e-commerce",
		Confidence:  0.85,
	}

	err := model.Validate()
	if err != nil {
		t.Fatalf("Expected valid model, got error: %v", err)
	}

	if model.Confidence < 0 || model.Confidence > 1 {
		t.Errorf("Confidence must be between 0 and 1, got: %f", model.Confidence)
	}
}

func TestBusinessModel_InvalidConfidence(t *testing.T) {
	model := &BusinessModel{
		Confidence: 1.5, // Invalid
	}

	err := model.Validate()
	if err == nil {
		t.Error("Expected error for invalid confidence, got nil")
	}
}

func TestBusinessModel_CalculateOverallConfidence(t *testing.T) {
	model := &BusinessModel{
		Concepts: []BusinessConcept{
			{Confidence: 0.8},
			{Confidence: 0.9},
		},
		Rules: []BusinessRule{
			{Confidence: 0.7},
		},
	}

	confidence := model.CalculateOverallConfidence()

	// (0.8+0.9)/2 = 0.85 (concepts)
	// 0.7 (rules)
	// 0.85*0.4 + 0.7*0.6 = 0.76
	expected := 0.76
	if confidence != expected {
		t.Errorf("Expected confidence %f, got %f", expected, confidence)
	}
}

func TestBusinessModel_CalculateOverallConfidence_EmptyConcepts(t *testing.T) {
	model := &BusinessModel{
		Rules: []BusinessRule{
			{Confidence: 0.8},
		},
	}

	confidence := model.CalculateOverallConfidence()
	expected := 0.48 // 0.8 * 0.6
	if confidence != expected {
		t.Errorf("Expected confidence %f, got %f", expected, confidence)
	}
}

func TestBusinessModel_CalculateOverallConfidence_EmptyRules(t *testing.T) {
	model := &BusinessModel{
		Concepts: []BusinessConcept{
			{Confidence: 0.9},
		},
	}

	confidence := model.CalculateOverallConfidence()
	expected := 0.36 // 0.9 * 0.4
	if math.Abs(confidence-expected) > 0.0001 {
		t.Errorf("Expected confidence %f, got %f", expected, confidence)
	}
}

func TestBusinessModel_CalculateOverallConfidence_BothEmpty(t *testing.T) {
	model := &BusinessModel{}

	confidence := model.CalculateOverallConfidence()
	expected := 0.0
	if confidence != expected {
		t.Errorf("Expected confidence %f, got %f", expected, confidence)
	}
}
