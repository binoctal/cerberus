package ai

import (
	"fmt"
)

// generateQuestionFromComment generates a clarifying question from a comment
func (mi *MinimalInteraction) generateQuestionFromComment(comment *Comment) string {
	if comment.Semantics == nil {
		return fmt.Sprintf("Clarify the business intent of this comment: %s", truncateText(comment.Text, 100))
	}

	switch comment.Semantics.Purpose {
	case "business_rule":
		return fmt.Sprintf("What is the complete business rule described here: %s?", truncateText(comment.Text, 80))
	case "validation":
		return fmt.Sprintf("What are the validation requirements: %s?", truncateText(comment.Text, 80))
	case "workflow":
		return fmt.Sprintf("Describe the workflow steps: %s?", truncateText(comment.Text, 80))
	case "state_transition":
		return fmt.Sprintf("What are the state transitions: %s?", truncateText(comment.Text, 80))
	default:
		return fmt.Sprintf("Clarify the business meaning: %s?", truncateText(comment.Text, 80))
	}
}

// generateQuestionFromPattern generates a clarifying question from a pattern
func (mi *MinimalInteraction) generateQuestionFromPattern(pattern *Pattern) string {
	return fmt.Sprintf("Can you elaborate on the %s pattern: %s?", pattern.Type.String(), pattern.Name)
}
