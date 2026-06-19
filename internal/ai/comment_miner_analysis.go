package ai

import "strings"

// isBusinessComment determines if a comment contains business-relevant information
func (m *CommentMiner) isBusinessComment(text string) bool {
	businessKeywords := []string{
		"business", "rule", "policy", "constraint", "validation",
		"requirement", "spec", "workflow", "process", "domain",
		"entity", "state", "transition", "condition", "exception",
	}

	lowerText := strings.ToLower(text)
	for _, keyword := range businessKeywords {
		if strings.Contains(lowerText, keyword) {
			return true
		}
	}

	return false
}

// inferPurpose infers the purpose of a comment based on its content
func (m *CommentMiner) inferPurpose(text string) string {
	lowerText := strings.ToLower(text)

	// Check for various patterns
	if strings.Contains(lowerText, "must") || strings.Contains(lowerText, "required") {
		return "validation"
	}
	if strings.Contains(lowerText, "when") || strings.Contains(lowerText, "then") {
		return "workflow"
	}
	if strings.Contains(lowerText, "state") || strings.Contains(lowerText, "transition") {
		return "state_transition"
	}
	if strings.Contains(lowerText, "business") || strings.Contains(lowerText, "rule") {
		return "business_rule"
	}

	return "general"
}

// calculateConfidence calculates a confidence score for comment semantics
func (m *CommentMiner) calculateConfidence(comment *Comment) float64 {
	// Base confidence
	confidence := 0.5

	// Increase confidence if comment has clear purpose
	if comment.Semantics != nil && comment.Semantics.Purpose != "" {
		confidence += 0.2
	}

	// Increase confidence if comment has business terms
	if comment.Semantics != nil && comment.Semantics.BusinessTerm != "" {
		confidence += 0.2
	}

	// Increase confidence for TODO/FIXME/NOTE comments
	if comment.Source == TODOComments || comment.Source == FIXMEComments || comment.Source == NOTEComments {
		confidence += 0.1
	}

	if confidence > 1.0 {
		confidence = 1.0
	}

	return confidence
}
