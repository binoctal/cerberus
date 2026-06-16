package business

// IsConfidenceLow checks if confidence is below threshold
func IsConfidenceLow(confidence, threshold float64) bool {
	return confidence < threshold
}

// CalculateWeightedAverage calculates weighted average of values
func CalculateWeightedAverage(values, weights []float64) float64 {
	if len(values) != len(weights) {
		return 0.0
	}

	if len(values) == 0 {
		return 0.0
	}

	sum := 0.0
	weightSum := 0.0

	for i, v := range values {
		sum += v * weights[i]
		weightSum += weights[i]
	}

	if weightSum == 0 {
		return 0.0
	}

	return sum / weightSum
}

// CountLowConfidenceRules counts rules with confidence below threshold
func CountLowConfidenceRules(rules []BusinessRule, threshold float64) int {
	count := 0
	for _, rule := range rules {
		if rule.Confidence < threshold {
			count++
		}
	}
	return count
}
