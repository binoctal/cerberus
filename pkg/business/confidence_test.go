package business

import "testing"

func TestConfidence_IsLow(t *testing.T) {
	tests := []struct {
		name       string
		confidence float64
		threshold  float64
		expected   bool
	}{
		{"below threshold", 0.5, 0.6, true},
		{"at threshold", 0.6, 0.6, false},
		{"above threshold", 0.8, 0.6, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsConfidenceLow(tt.confidence, tt.threshold)
			if result != tt.expected {
				t.Errorf("IsConfidenceLow(%f, %f) = %v, want %v",
					tt.confidence, tt.threshold, result, tt.expected)
			}
		})
	}
}

func TestConfidence_CalculateWeightedAverage(t *testing.T) {
	values := []float64{0.8, 0.9, 0.7}
	weights := []float64{0.5, 0.3, 0.2}

	result := CalculateWeightedAverage(values, weights)
	expected := 0.8*0.5 + 0.9*0.3 + 0.7*0.2

	if result != expected {
		t.Errorf("Expected %f, got %f", expected, result)
	}
}

func TestConfidence_CalculateWeightedAverage_EdgeCases(t *testing.T) {
	// Empty slices
	result := CalculateWeightedAverage([]float64{}, []float64{})
	if result != 0.0 {
		t.Errorf("Expected 0.0 for empty slices, got %f", result)
	}

	// Mismatched lengths
	result = CalculateWeightedAverage([]float64{0.5}, []float64{})
	if result != 0.0 {
		t.Errorf("Expected 0.0 for mismatched lengths, got %f", result)
	}
}

func TestConfidence_CountLowConfidenceRules(t *testing.T) {
	rules := []BusinessRule{
		{Confidence: 0.8},
		{Confidence: 0.4},
		{Confidence: 0.9},
		{Confidence: 0.3},
	}

	count := CountLowConfidenceRules(rules, 0.5)
	if count != 2 {
		t.Errorf("Expected 2 low confidence rules, got %d", count)
	}
}
