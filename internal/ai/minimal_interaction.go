package ai

import (
	"fmt"
)

// InteractionConfig controls the minimal interaction behavior
type InteractionConfig struct {
	ConfidenceThreshold float64 // Below this, ask questions (default: 0.7)
	MaxQuestions         int     // Maximum questions to ask (default: 5)
	BusinessCriticalOnly bool    // Only ask about business-critical items
}

// MinimalInteraction controls AI questioning to minimize user interruption
type MinimalInteraction struct {
	config InteractionConfig
}

// NewMinimalInteraction creates a new minimal interaction controller
func NewMinimalInteraction(config InteractionConfig) *MinimalInteraction {
	// Set defaults if not provided
	if config.ConfidenceThreshold == 0 {
		config.ConfidenceThreshold = 0.7
	}
	if config.MaxQuestions == 0 {
		config.MaxQuestions = 5
	}

	return &MinimalInteraction{
		config: config,
	}
}

// Question represents a question to ask the user
type Question struct {
	ID          string
	Text        string
	Context     string
	Criticality string // "high", "medium", "low"
	Category    string // "business_rule", "domain_logic", "workflow", "validation", etc.
	Priority    int    // Higher = more important
}

// IsConfidenceLow checks if confidence is below threshold
// This is the core decision function that determines when to interrupt the user
func (mi *MinimalInteraction) IsConfidenceLow(confidence float64, isBusinessCritical bool) bool {
	// Always check business-critical items
	if isBusinessCritical && confidence < mi.config.ConfidenceThreshold {
		return true
	}

	// For non-critical items, only check if configured to do so
	if !mi.config.BusinessCriticalOnly {
		return confidence < mi.config.ConfidenceThreshold
	}

	return false
}

// GenerateCriticalQuestionsOnly generates questions only for business-critical uncertainties
// This method implements comprehensive logic:
// 1. Filters insights by confidence and criticality
// 2. Prioritizes questions by business impact
// 3. Limits total questions to MaxQuestions
// 4. Returns sorted questions (highest priority first)
func (mi *MinimalInteraction) GenerateCriticalQuestionsOnly(insights *CodeInsights, comments []*Comment, patterns []*Pattern) []*Question {
	var questions []*Question

	// Generate questions from low-confidence business comments
	for _, comment := range comments {
		if comment.Semantics == nil {
			continue
		}

		// Check if comment has low confidence
		if mi.IsConfidenceLow(comment.Semantics.Confidence, mi.isBusinessCritical(comment)) {
			question := &Question{
				ID:          fmt.Sprintf("comment-%d", comment.LineNumber),
				Text:        mi.generateQuestionFromComment(comment),
				Context:     fmt.Sprintf("File: %s, Line: %d", comment.FilePath, comment.LineNumber),
				Criticality: mi.determineCriticality(comment),
				Category:    comment.Semantics.Purpose,
				Priority:    mi.calculateQuestionPriority(comment),
			}
			questions = append(questions, question)
		}
	}

	// Generate questions from low-confidence patterns
	for _, pattern := range patterns {
		// Check if pattern has low confidence
		if mi.IsConfidenceLow(pattern.Confidence, mi.isPatternBusinessCritical(pattern)) {
			question := &Question{
				ID:          fmt.Sprintf("pattern-%s", pattern.ID),
				Text:        mi.generateQuestionFromPattern(pattern),
				Context:     fmt.Sprintf("Pattern: %s", pattern.Name),
				Criticality: mi.determinePatternCriticality(pattern),
				Category:    pattern.Type.String(),
				Priority:    mi.calculatePatternQuestionPriority(pattern),
			}
			questions = append(questions, question)
		}
	}

	// Sort questions by priority (highest first)
	mi.sortQuestionsByPriority(questions)

	// Limit to MaxQuestions
	if len(questions) > mi.config.MaxQuestions {
		questions = questions[:mi.config.MaxQuestions]
	}

	return questions
}

// isBusinessCritical determines if a comment is business-critical
func (mi *MinimalInteraction) isBusinessCritical(comment *Comment) bool {
	if comment.Semantics == nil {
		return false
	}

	// Check for business-critical purposes
	criticalPurposes := []string{
		"business_rule",
		"validation",
		"constraint",
		"security",
		"compliance",
	}

	for _, purpose := range criticalPurposes {
		if comment.Semantics.Purpose == purpose {
			return true
		}
	}

	return false
}

// isPatternBusinessCritical determines if a pattern is business-critical
func (mi *MinimalInteraction) isPatternBusinessCritical(pattern *Pattern) bool {
	criticalTypes := []PatternType{
		BusinessPatterns,
		RulePattern,
		WorkflowPattern,
	}

	for _, ptype := range criticalTypes {
		if pattern.Type == ptype {
			return true
		}
	}

	return false
}

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

// GetConfig returns the interaction configuration
func (mi *MinimalInteraction) GetConfig() InteractionConfig {
	return mi.config
}

// SetConfig updates the interaction configuration
func (mi *MinimalInteraction) SetConfig(config InteractionConfig) {
	mi.config = config
}

// Validate checks if a question is valid
func (q *Question) Validate() error {
	if q == nil {
		return fmt.Errorf("question is nil")
	}

	if q.ID == "" {
		return fmt.Errorf("question ID is empty")
	}

	if q.Text == "" {
		return fmt.Errorf("question text is empty")
	}

	validCriticalities := map[string]bool{
		"high":   true,
		"medium": true,
		"low":    true,
	}

	if !validCriticalities[q.Criticality] {
		return fmt.Errorf("invalid criticality: %s", q.Criticality)
	}

	return nil
}
