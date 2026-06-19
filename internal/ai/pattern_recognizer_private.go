package ai

// calculatePatternConfidence calculates a confidence score for a pattern
func (pr *PatternRecognizer) calculatePatternConfidence(pattern *Pattern) float64 {
	confidence := 0.5 // Base confidence

	// Increase confidence if pattern has rich metadata
	if len(pattern.Metadata) > 0 {
		confidence += 0.2
	}

	// Increase confidence if description is detailed
	if len(pattern.Description) > 20 {
		confidence += 0.1
	}

	// Increase confidence if location information is complete
	if len(pattern.Locations) > 0 && pattern.Locations[0].FilePath != "" {
		confidence += 0.1
	}

	// Adjust based on pattern type
	switch pattern.Type {
	case BusinessPatterns:
		confidence += 0.1 // Business rules are typically well-defined
	case EdgeCasePattern:
		confidence += 0.05 // Edge cases are explicit
	}

	// Ensure confidence is between 0 and 1
	if confidence > 1.0 {
		confidence = 1.0
	}

	return confidence
}

// addPattern adds a pattern to the appropriate category in the database
func (pr *PatternRecognizer) addPattern(pattern *Pattern) {
	switch pattern.Type {
	case BusinessPatterns:
		pr.database.BusinessPatterns = append(pr.database.BusinessPatterns, pattern)
	case DomainPattern:
		pr.database.DomainPatterns = append(pr.database.DomainPatterns, pattern)
	case WorkflowPattern:
		pr.database.WorkflowPatterns = append(pr.database.WorkflowPatterns, pattern)
	case StateMachinePattern:
		pr.database.StateMachinePatterns = append(pr.database.StateMachinePatterns, pattern)
	case RulePattern:
		pr.database.RulePatterns = append(pr.database.RulePatterns, pattern)
	case EdgeCasePattern:
		pr.database.EdgeCasePatterns = append(pr.database.EdgeCasePatterns, pattern)
	case ErrorHandlingPattern:
		pr.database.ErrorHandlingPatterns = append(pr.database.ErrorHandlingPatterns, pattern)
	}
}

// getPatternsByType retrieves all patterns of a specific type
func (pr *PatternRecognizer) getPatternsByType(patternType PatternType) []*Pattern {
	switch patternType {
	case BusinessPatterns:
		return pr.database.BusinessPatterns
	case DomainPattern:
		return pr.database.DomainPatterns
	case WorkflowPattern:
		return pr.database.WorkflowPatterns
	case StateMachinePattern:
		return pr.database.StateMachinePatterns
	case RulePattern:
		return pr.database.RulePatterns
	case EdgeCasePattern:
		return pr.database.EdgeCasePatterns
	case ErrorHandlingPattern:
		return pr.database.ErrorHandlingPatterns
	default:
		return []*Pattern{}
	}
}
