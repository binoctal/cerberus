package ai

import (
	"fmt"
)

// InteractionConfig controls the minimal interaction behavior
type InteractionConfig struct {
	ConfidenceThreshold  float64 // Below this, ask questions (default: 0.7)
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
