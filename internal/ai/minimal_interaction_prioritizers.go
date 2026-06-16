package ai

// determineCriticality determines the criticality level of a comment
func (mi *MinimalInteraction) determineCriticality(comment *Comment) string {
	if mi.isBusinessCritical(comment) {
		return "high"
	}

	if comment.Source == WARNINGComments || comment.Source == FIXMEComments {
		return "high"
	}

	if comment.Source == TODOComments || comment.Source == NOTEComments {
		return "medium"
	}

	return "low"
}

// determinePatternCriticality determines the criticality level of a pattern
func (mi *MinimalInteraction) determinePatternCriticality(pattern *Pattern) string {
	if mi.isPatternBusinessCritical(pattern) {
		return "high"
	}

	if pattern.Type == StateMachinePattern || pattern.Type == WorkflowPattern {
		return "medium"
	}

	return "low"
}

// calculateQuestionPriority calculates the priority of a question from a comment
func (mi *MinimalInteraction) calculateQuestionPriority(comment *Comment) int {
	priority := 50 // Base priority

	// Increase priority for business-critical comments
	if mi.isBusinessCritical(comment) {
		priority += 30
	}

	// Increase priority for warning/fixme comments
	if comment.Source == WARNINGComments || comment.Source == FIXMEComments {
		priority += 20
	}

	// Increase priority based on confidence (lower = higher priority)
	if comment.Semantics != nil {
		priority += int((1.0 - comment.Semantics.Confidence) * 20)
	}

	return priority
}

// calculatePatternQuestionPriority calculates the priority of a question from a pattern
func (mi *MinimalInteraction) calculatePatternQuestionPriority(pattern *Pattern) int {
	priority := 50 // Base priority

	// Increase priority for business-critical patterns
	if mi.isPatternBusinessCritical(pattern) {
		priority += 30
	}

	// Increase priority based on number of locations (more locations = more impact)
	if len(pattern.Locations) > 5 {
		priority += 15
	}

	// Increase priority based on confidence (lower = higher priority)
	priority += int((1.0 - pattern.Confidence) * 20)

	return priority
}

// sortQuestionsByPriority sorts questions by priority (highest first)
func (mi *MinimalInteraction) sortQuestionsByPriority(questions []*Question) {
	// Simple bubble sort (sufficient for small lists)
	n := len(questions)
	for i := 0; i < n-1; i++ {
		for j := 0; j < n-i-1; j++ {
			if questions[j].Priority < questions[j+1].Priority {
				questions[j], questions[j+1] = questions[j+1], questions[j]
			}
		}
	}
}

// truncateText truncates text to a maximum length
func truncateText(text string, maxLen int) string {
	if len(text) <= maxLen {
		return text
	}
	return text[:maxLen] + "..."
}
